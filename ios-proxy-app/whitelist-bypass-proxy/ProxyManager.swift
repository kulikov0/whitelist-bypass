import Foundation
import UIKit
import Combine
import Mobile


enum ProxyStatus: String {
    case idle = "IDLE"
    case ready = "READY"
    case connecting = "CONNECTING"
    case tunnelConnected = "TUNNEL_CONNECTED"
    case tunnelLost = "TUNNEL_LOST"
    case error = "ERROR"

    var displayLabel: String {
        switch self {
        case .idle: return NSLocalizedString("status_idle", comment: "")
        case .ready: return NSLocalizedString("status_ready", comment: "")
        case .connecting: return NSLocalizedString("status_connecting", comment: "")
        case .tunnelConnected: return NSLocalizedString("status_tunnel_active", comment: "")
        case .tunnelLost: return NSLocalizedString("status_tunnel_lost", comment: "")
        case .error: return NSLocalizedString("status_error", comment: "")
        }
    }
}

enum SocksAuthMode: String, CaseIterable {
    case auto = "AUTO"
    case manual = "MANUAL"
}

enum DnsMode: String, CaseIterable {
    case system = "SYSTEM"
    case custom = "CUSTOM"
}

class HeadlessCallbackBridge: NSObject, IosHeadlessCallbackProtocol {
    weak var manager: ProxyManager?

    func onLog(_ msg: String?) {
        guard let msg = msg else { return }
        print("[GO] \(msg)")
        let mgr = manager
        DispatchQueue.main.async { [weak mgr] in
            mgr?.appendLog(msg)
        }
    }

    func onStatus(_ status: String?) {
        guard let status = status else { return }
        print("[STATUS] \(status)")
        let mgr = manager
        DispatchQueue.main.async {
            mgr?.handleStatus(status)
        }
    }

    func saveCache(_ key: String?, value: String?) {
        guard let key = key, let value = value else { return }
        UserDefaults.standard.set(value, forKey: "cache_\(key)")
    }

    func loadCache(_ key: String?) -> String {
        guard let key = key else { return "" }
        return UserDefaults.standard.string(forKey: "cache_\(key)") ?? ""
    }

    func clearCache(_ key: String?) {
        guard let key = key else { return }
        UserDefaults.standard.removeObject(forKey: "cache_\(key)")
    }

    func resolveHost(_ hostname: String?) -> String {
        guard let hostname = hostname else { return "" }
        var result = ""
        let host = CFHostCreateWithName(nil, hostname as CFString).takeRetainedValue()
        CFHostStartInfoResolution(host, .addresses, nil)
        if let addresses = CFHostGetAddressing(host, nil)?.takeUnretainedValue() as? [Data] {
            for addressData in addresses {
                var storage = sockaddr_storage()
                addressData.withUnsafeBytes { buffer in
                    if let baseAddress = buffer.baseAddress {
                        memcpy(&storage, baseAddress, min(addressData.count, MemoryLayout<sockaddr_storage>.size))
                    }
                }
                if storage.ss_family == UInt8(AF_INET) {
                    var addr = sockaddr_in()
                    addressData.withUnsafeBytes { buffer in
                        if let baseAddress = buffer.baseAddress {
                            memcpy(&addr, baseAddress, MemoryLayout<sockaddr_in>.size)
                        }
                    }
                    var ipString = [CChar](repeating: 0, count: Int(INET_ADDRSTRLEN))
                    var inAddr = addr.sin_addr
                    inet_ntop(AF_INET, &inAddr, &ipString, socklen_t(INET_ADDRSTRLEN))
                    result = String(cString: ipString)
                    break
                }
            }
        }
        return result
    }
}

enum TunnelMode: String, CaseIterable {
    case dc = "dc"
    case video = "video"

    var label: String {
        switch self {
        case .dc: return "DC"
        case .video: return "Video"
        }
    }
}

enum CallPlatform: String {
    case vk = "vk"
    case telemost = "telemost"

    static func detect(url: String) -> CallPlatform {
        if url.contains("telemost.yandex") {
            return .telemost
        }
        return .vk
    }
}

class ProxyManager: ObservableObject {
    @Published var status: ProxyStatus = .idle
    @Published var errorMessage: String = ""
    @Published var logs: [String] = []
    @Published var isRunning: Bool = false
    @Published var toastMessage: String?
    @Published var statusText: String?
    var detectedPlatform: CallPlatform = .vk

    @Published var callUrl: String = AppDefaults.lastUrl { didSet { AppDefaults.lastUrl = callUrl } }
    @Published var socksPort: Int = AppDefaults.socksPort { didSet { AppDefaults.socksPort = socksPort } }
    @Published var tunnelMode: TunnelMode = AppDefaults.tunnelMode { didSet { AppDefaults.tunnelMode = tunnelMode } }
    @Published var displayName: String = AppDefaults.displayName { didSet { AppDefaults.displayName = displayName } }
    @Published var showLogs: Bool = AppDefaults.showLogs { didSet { AppDefaults.showLogs = showLogs } }
    @Published var socksAuthMode: SocksAuthMode = AppDefaults.socksAuthMode { didSet { AppDefaults.socksAuthMode = socksAuthMode } }
    @Published var manualSocksUser: String = AppDefaults.socksUser { didSet { AppDefaults.socksUser = manualSocksUser } }
    @Published var manualSocksPass: String = AppDefaults.socksPass { didSet { AppDefaults.socksPass = manualSocksPass } }

    private let autoSocksUser: String
    private let autoSocksPass: String
    private var callbackBridge: HeadlessCallbackBridge?
    private let backgroundKeepAlive = BackgroundKeepAlive()

    private var pendingLogs: [String] = []
    private var logFlushScheduled = false

    var activeSocksUser: String {
        socksAuthMode == .manual ? manualSocksUser : autoSocksUser
    }

    var activeSocksPass: String {
        socksAuthMode == .manual ? manualSocksPass : autoSocksPass
    }

    init() {
        let chars = "abcdefghijklmnopqrstuvwxyz0123456789"
        autoSocksUser = String((0..<16).map { _ in chars.randomElement()! })
        autoSocksPass = String((0..<24).map { _ in chars.randomElement()! })
    }

    var socksUrl: String {
        "socks5://\(activeSocksUser):\(activeSocksPass)@127.0.0.1:\(socksPort)"
    }

    private func isPortAvailable(_ port: Int) -> Bool {
        let socketFD = socket(AF_INET, SOCK_STREAM, IPPROTO_TCP)
        if socketFD == -1 { return false }
        defer { close(socketFD) }

        var addr = sockaddr_in()
        addr.sin_len = UInt8(MemoryLayout<sockaddr_in>.size)
        addr.sin_family = sa_family_t(AF_INET)
        addr.sin_port = in_port_t(port).bigEndian
        addr.sin_addr.s_addr = INADDR_ANY

        let result = withUnsafePointer(to: &addr) { addrPtr in
            addrPtr.withMemoryRebound(to: sockaddr.self, capacity: 1) { sockaddrPtr in
                bind(socketFD, sockaddrPtr, socklen_t(MemoryLayout<sockaddr_in>.size))
            }
        }
        return result == 0
    }

    func connect() {
        guard !callUrl.isEmpty else { return }

        if !isPortAvailable(socksPort) {
            let originalPort = socksPort
            let ranges: [ClosedRange<Int>] = [
                originalPort...min(originalPort + 100, 65535),
                1080...1380,
                8080...8380,
                9080...9380,
                49152...65535
            ]
            var foundPort = false
            for range in ranges {
                for candidatePort in range {
                    if isPortAvailable(candidatePort) {
                        socksPort = candidatePort
                        foundPort = true
                        break
                    }
                }
                if foundPort { break }
            }
            if !foundPort {
                let socketFD = socket(AF_INET, SOCK_STREAM, IPPROTO_TCP)
                if socketFD != -1 {
                    var addr = sockaddr_in()
                    addr.sin_len = UInt8(MemoryLayout<sockaddr_in>.size)
                    addr.sin_family = sa_family_t(AF_INET)
                    addr.sin_port = 0
                    addr.sin_addr.s_addr = in_addr_t(INADDR_LOOPBACK).bigEndian
                    let bound = withUnsafePointer(to: &addr) { addrPtr in
                        addrPtr.withMemoryRebound(to: sockaddr.self, capacity: 1) { sockaddrPtr in
                            bind(socketFD, sockaddrPtr, socklen_t(MemoryLayout<sockaddr_in>.size))
                        }
                    }
                    if bound == 0 {
                        var boundAddr = sockaddr_in()
                        var addrLen = socklen_t(MemoryLayout<sockaddr_in>.size)
                        withUnsafeMutablePointer(to: &boundAddr) { ptr in
                            ptr.withMemoryRebound(to: sockaddr.self, capacity: 1) { sockPtr in
                                getsockname(socketFD, sockPtr, &addrLen)
                            }
                        }
                        socksPort = Int(UInt16(bigEndian: boundAddr.sin_port))
                    }
                    close(socketFD)
                }
            }
            appendLog("Port \(originalPort) busy, using \(socksPort)")
        }

        logs.removeAll()
        pendingLogs.removeAll()
        errorMessage = ""
        status = .idle
        isRunning = true

        let bridge = HeadlessCallbackBridge()
        bridge.manager = self
        callbackBridge = bridge

        backgroundKeepAlive.start()
        detectedPlatform = CallPlatform.detect(url: callUrl)
        appendLog("Platform: \(detectedPlatform.rawValue)")

        if tunnelMode == .dc && detectedPlatform != .vk {
            tunnelMode = .video
            showToast(NSLocalizedString("dc_mode_not_supported", comment: ""))
        }

        switch detectedPlatform {
        case .telemost:
            IosStartTelemostHeadless(socksPort, activeSocksUser, activeSocksPass, bridge)
            appendLog("Started Telemost headless joiner")
            let joinParams: [String: String] = [
                "joinLink": callUrl,
                "displayName": displayName,
            ]
            if let jsonData = try? JSONSerialization.data(withJSONObject: joinParams),
               let jsonString = String(data: jsonData, encoding: .utf8) {
                IosSendJoinParams(jsonString)
                appendLog("Sent join params")
            }

        case .vk:
            IosStartVKHeadless(socksPort, activeSocksUser, activeSocksPass, callUrl, displayName, tunnelMode.rawValue, bridge)
            appendLog("Started VK headless joiner")
        }
    }

    func disconnect() {
        callbackBridge?.manager = nil
        callbackBridge = nil
        IosStopCaptchaProxy()
        IosStopHeadless()
        backgroundKeepAlive.stop()
        isRunning = false
        status = .idle
        appendLog("Disconnected")
    }

    func resetAll() {
        disconnect()
        captchaURL = nil
        statusText = nil
        logs.removeAll()
        pendingLogs.removeAll()
        errorMessage = ""
        socksPort = 1080
    }

    @Published var captchaURL: String?

    func handleStatus(_ statusString: String) {
        if statusString.hasPrefix("ERROR:") {
            let errorText = String(statusString.dropFirst(6))
            status = .error
            errorMessage = errorText
            isRunning = false
            captchaURL = nil
            appendLog("ERROR: \(errorText)")
        } else if statusString.hasPrefix("CAPTCHA:") {
            captchaURL = String(statusString.dropFirst(8))
            statusText = NSLocalizedString("status_solve_captcha", comment: "")
            appendLog("Captcha: \(captchaURL ?? "")")
        } else {
            if captchaURL != nil && statusString != "CAPTCHA" {
                captchaURL = nil
                statusText = nil
            }
            status = ProxyStatus(rawValue: statusString) ?? .idle
            appendLog("Status: \(statusString)")
        }
    }

    func appendLog(_ message: String) {
        let timestamp = DateFormatter.localizedString(from: Date(), dateStyle: .none, timeStyle: .medium)
        pendingLogs.append("[\(timestamp)] \(message)")

        if !logFlushScheduled {
            logFlushScheduled = true
            DispatchQueue.main.asyncAfter(deadline: .now() + 0.3) { [weak self] in
                self?.flushLogs()
            }
        }
    }

    private func flushLogs() {
        logFlushScheduled = false
        guard !pendingLogs.isEmpty else { return }
        logs.append(contentsOf: pendingLogs)
        pendingLogs.removeAll()
        if logs.count > 100 {
            logs.removeFirst(logs.count - 100)
        }
    }

    func copyProxyUrl() {
        UIPasteboard.general.string = socksUrl
        showToast(NSLocalizedString("proxy_url_copied", comment: ""))
    }

    func showToast(_ message: String) {
        toastMessage = message
        DispatchQueue.main.asyncAfter(deadline: .now() + 2) { [weak self] in
            if self?.toastMessage == message {
                self?.toastMessage = nil
            }
        }
    }

    func openTelegramProxy() {
        let urlString = "tg://socks?server=127.0.0.1&port=\(socksPort)&user=\(activeSocksUser)&pass=\(activeSocksPass)"
        if let url = URL(string: urlString) {
            UIApplication.shared.open(url)
        }
    }

}
