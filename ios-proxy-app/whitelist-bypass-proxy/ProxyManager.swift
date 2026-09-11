import Foundation
import UIKit
import Combine
import Network
import Mobile


enum ProxyStatus: String {
    case idle = "IDLE"
    case ready = "READY"
    case connecting = "CONNECTING"
    case reconnecting = "RECONNECTING"
    case tunnelConnected = "TUNNEL_CONNECTED"
    case tunnelLost = "TUNNEL_LOST"
    case error = "ERROR"

    var displayLabel: String {
        switch self {
        case .idle: return NSLocalizedString("status_idle", comment: "")
        case .ready: return NSLocalizedString("status_ready", comment: "")
        case .connecting: return NSLocalizedString("status_connecting", comment: "")
        case .reconnecting: return NSLocalizedString("status_connecting", comment: "")
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
    case wbstream = "wbstream"
    case dion = "dion"

    static let wbstreamPrefix = "wbstream://"
    static let dionPrefix = "dion://"
    static let dionEventInfix = "dion.vc/event/"

    static func detect(url: String) -> CallPlatform {
        if url.hasPrefix(dionPrefix) || url.contains(dionEventInfix) {
            return .dion
        }
        if url.hasPrefix(wbstreamPrefix) {
            return .wbstream
        }
        if url.contains("telemost") {
            return .telemost
        }
        return .vk
    }

    static func extractRoomId(url: String) -> String {
        let trimmed = url.trimmingCharacters(in: .whitespaces)
        if trimmed.hasPrefix(wbstreamPrefix) {
            return String(trimmed.dropFirst(wbstreamPrefix.count)).trimmingCharacters(in: .whitespaces)
        }
        if trimmed.hasPrefix(dionPrefix) {
            return String(trimmed.dropFirst(dionPrefix.count)).trimmingCharacters(in: .whitespaces)
        }
        if let range = trimmed.range(of: dionEventInfix) {
            var slug = String(trimmed[range.upperBound...])
            if let qmark = slug.firstIndex(of: "?") { slug = String(slug[..<qmark]) }
            if let slash = slug.firstIndex(of: "/") { slug = String(slug[..<slash]) }
            return slug.trimmingCharacters(in: .whitespaces)
        }
        return trimmed
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
    @Published var vp8Fps: Int = AppDefaults.vp8Fps { didSet { AppDefaults.vp8Fps = vp8Fps } }
    @Published var vp8Batch: Int = AppDefaults.vp8Batch { didSet { AppDefaults.vp8Batch = vp8Batch } }
    @Published var dualTrack: Bool = AppDefaults.dualTrack { didSet { AppDefaults.dualTrack = dualTrack } }
    @Published var reliable: Bool = AppDefaults.reliable { didSet { AppDefaults.reliable = reliable } }
    @Published var debug: Bool = AppDefaults.debug { didSet { AppDefaults.debug = debug } }

    /// User intent: stays true across reconnects until Stop is pressed.
    @Published var isArmed: Bool = false
    /// Set when the SOCKS port had to move - the Happ profile is stale until re-imported.
    @Published var portDrifted: Bool = false
    /// Restarts stopped helping: the VPN in Happ is holding the network the joiner needs.
    @Published var needsHappToggle: Bool = false
    @Published var restartCount: Int = 0

    private let autoSocksUser: String
    private let autoSocksPass: String
    private var callbackBridge: HeadlessCallbackBridge?
    private let backgroundKeepAlive = BackgroundKeepAlive()

    private var pendingLogs: [String] = []
    private var logFlushScheduled = false

    // Supervisor: keeps the tunnel alive without the user touching anything.
    private var supervisorTimer: Timer?
    private var pendingRestart: DispatchWorkItem?
    private var pathMonitor: NWPathMonitor?
    private var networkUp = true
    private var downSince: Date?
    private var restartAttempt = 0

    /// No TUNNEL_CONNECTED for this long while armed -> tear the joiner down and start it clean.
    private let stuckTimeout: TimeInterval = 45
    /// Outage this long means restarts aren't helping - most likely Happ owns the network.
    private let happHintTimeout: TimeInterval = 100
    private let restartBackoff: [TimeInterval] = [3, 6, 12, 20, 30]

    var activeSocksUser: String {
        socksAuthMode == .manual ? manualSocksUser : autoSocksUser
    }

    var activeSocksPass: String {
        socksAuthMode == .manual ? manualSocksPass : autoSocksPass
    }

    init() {
        // Credentials are generated once and kept forever: a Happ profile imported today
        // has to keep working after the app is restarted.
        let chars = "abcdefghijklmnopqrstuvwxyz0123456789"
        if AppDefaults.autoSocksUser.isEmpty || AppDefaults.autoSocksPass.isEmpty {
            AppDefaults.autoSocksUser = String((0..<16).map { _ in chars.randomElement()! })
            AppDefaults.autoSocksPass = String((0..<24).map { _ in chars.randomElement()! })
        }
        autoSocksUser = AppDefaults.autoSocksUser
        autoSocksPass = AppDefaults.autoSocksPass

        startPathMonitor()
        NotificationCenter.default.addObserver(
            self,
            selector: #selector(appDidBecomeActive),
            name: UIApplication.didBecomeActiveNotification,
            object: nil
        )
    }

    var socksUrl: String {
        "socks5://\(activeSocksUser):\(activeSocksPass)@127.0.0.1:\(socksPort)"
    }

    /// Mirrors what Go's net.Listen does: loopback bind with SO_REUSEADDR, then an actual
    /// listen(). Probing 0.0.0.0 without SO_REUSEADDR used to report the port as busy while
    /// old connections sat in TIME_WAIT, which is what made the port creep 1080 -> 1081 -> 1082.
    private func isPortAvailable(_ port: Int) -> Bool {
        let socketFD = socket(AF_INET, SOCK_STREAM, IPPROTO_TCP)
        if socketFD == -1 { return false }
        defer { close(socketFD) }

        var reuse: Int32 = 1
        setsockopt(socketFD, SOL_SOCKET, SO_REUSEADDR, &reuse, socklen_t(MemoryLayout<Int32>.size))

        var addr = sockaddr_in()
        addr.sin_len = UInt8(MemoryLayout<sockaddr_in>.size)
        addr.sin_family = sa_family_t(AF_INET)
        addr.sin_port = in_port_t(port).bigEndian
        addr.sin_addr.s_addr = in_addr_t(INADDR_LOOPBACK).bigEndian

        let result = withUnsafePointer(to: &addr) { addrPtr in
            addrPtr.withMemoryRebound(to: sockaddr.self, capacity: 1) { sockaddrPtr in
                bind(socketFD, sockaddrPtr, socklen_t(MemoryLayout<sockaddr_in>.size))
            }
        }
        if result != 0 { return false }
        return listen(socketFD, 1) == 0
    }

    /// Waits for the previous listener to go away instead of grabbing a new port, so the
    /// port stays the one the Happ profile points at. Runs off the main thread.
    private func acquirePort(preferred: Int) -> (port: Int, drifted: Bool) {
        for attempt in 0..<16 {
            if isPortAvailable(preferred) { return (preferred, false) }
            if attempt == 0 {
                appendLogAsync("Port \(preferred) still held, waiting for the old listener")
            }
            Thread.sleep(forTimeInterval: 0.25)
        }

        let ranges: [ClosedRange<Int>] = [
            preferred...min(preferred + 100, 65535),
            1080...1380,
            8080...8380,
            9080...9380,
        ]
        for range in ranges {
            for candidate in range where isPortAvailable(candidate) {
                appendLogAsync("Port \(preferred) busy, falling back to \(candidate) - re-import the profile in Happ")
                return (candidate, true)
            }
        }
        appendLogAsync("No free port found, retrying on \(preferred)")
        return (preferred, false)
    }

    func connect() {
        guard !callUrl.isEmpty else { return }

        isArmed = true
        restartAttempt = 0
        restartCount = 0
        needsHappToggle = false
        downSince = Date()
        logs.removeAll()
        pendingLogs.removeAll()
        errorMessage = ""
        startSupervisor()
        launchHeadless()
    }

    /// Starts (or restarts) the Go joiner. Port and credentials never change here, so the
    /// Happ profile imported once keeps working across every restart.
    private func launchHeadless() {
        guard isArmed, !callUrl.isEmpty else { return }

        pendingRestart?.cancel()
        pendingRestart = nil
        errorMessage = ""
        status = .connecting
        isRunning = true

        let preferred = socksPort
        DispatchQueue.global(qos: .userInitiated).async { [weak self] in
            guard let self = self else { return }
            let result = self.acquirePort(preferred: preferred)
            DispatchQueue.main.async {
                guard self.isArmed else { return }
                if result.port != self.socksPort {
                    self.socksPort = result.port
                }
                self.portDrifted = result.drifted
                self.startJoiner()
            }
        }
    }

    private func startJoiner() {
        let bridge = HeadlessCallbackBridge()
        bridge.manager = self
        callbackBridge = bridge

        backgroundKeepAlive.start()
        detectedPlatform = CallPlatform.detect(url: callUrl)
        appendLog("Platform: \(detectedPlatform.rawValue)")

        if tunnelMode == .dc && (detectedPlatform == .telemost || detectedPlatform == .dion) {
            tunnelMode = .video
            showToast(NSLocalizedString("dc_mode_not_supported", comment: ""))
        }

        IosSetDebug(debug)

        switch detectedPlatform {
        case .telemost:
            IosStartTelemostHeadless(socksPort, activeSocksUser, activeSocksPass, bridge)
            appendLog("Started Telemost headless joiner")
            let joinParams: [String: Any] = [
                "joinLink": callUrl,
                "displayName": displayName,
                "vp8Fps": vp8Fps,
                "vp8Batch": vp8Batch,
                "dualTrack": dualTrack,
                "reliable": reliable,
            ]
            if let jsonData = try? JSONSerialization.data(withJSONObject: joinParams),
               let jsonString = String(data: jsonData, encoding: .utf8) {
                IosSendJoinParams(jsonString)
                appendLog("Sent join params")
            }

        case .vk:
            IosStartVKHeadless(socksPort, activeSocksUser, activeSocksPass, callUrl, displayName, tunnelMode.rawValue, vp8Fps, vp8Batch, dualTrack, bridge)
            appendLog("Started VK headless joiner")

        case .wbstream:
            IosStartWBStreamHeadless(socksPort, activeSocksUser, activeSocksPass, bridge)
            appendLog("Started WB Stream headless joiner")
            let joinParams: [String: Any] = [
                "roomId": CallPlatform.extractRoomId(url: callUrl),
                "displayName": displayName,
                "tunnelMode": tunnelMode.rawValue,
                "vp8Fps": vp8Fps,
                "vp8Batch": vp8Batch,
                "dualTrack": dualTrack,
                "reliable": reliable,
            ]
            if let jsonData = try? JSONSerialization.data(withJSONObject: joinParams),
               let jsonString = String(data: jsonData, encoding: .utf8) {
                IosSendJoinParams(jsonString)
                appendLog("Sent join params")
            }

        case .dion:
            IosStartDionHeadless(socksPort, activeSocksUser, activeSocksPass, bridge)
            appendLog("Started DION headless joiner")
            let joinParams: [String: Any] = [
                "roomId": CallPlatform.extractRoomId(url: callUrl),
                "displayName": displayName,
                "vp8Fps": vp8Fps,
                "vp8Batch": vp8Batch,
            ]
            if let jsonData = try? JSONSerialization.data(withJSONObject: joinParams),
               let jsonString = String(data: jsonData, encoding: .utf8) {
                IosSendJoinParams(jsonString)
                appendLog("Sent join params")
            }
        }
    }

    /// Tears the joiner down without dropping the user's intent - used between restarts.
    private func stopJoiner(keepAlive: Bool) {
        callbackBridge?.manager = nil
        callbackBridge = nil
        IosStopCaptchaProxy()
        IosStopHeadless()
        if !keepAlive { backgroundKeepAlive.stop() }
        isRunning = false
    }

    func disconnect() {
        isArmed = false
        stopSupervisor()
        pendingRestart?.cancel()
        pendingRestart = nil
        downSince = nil
        needsHappToggle = false
        stopJoiner(keepAlive: false)
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
        portDrifted = false
        socksPort = 1080
    }

    @Published var captchaURL: String?

    func handleStatus(_ statusString: String) {
        if statusString.hasPrefix("ERROR:") {
            let errorText = String(statusString.dropFirst(6))
            status = .error
            errorMessage = errorText
            captchaURL = nil
            appendLog("ERROR: \(errorText)")
            if isArmed {
                // The Go joiner gives up for good if the very first attempt fails, so the
                // restart has to come from here.
                scheduleRestart(reason: errorText)
            } else {
                isRunning = false
            }
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
            if status == .tunnelConnected {
                onTunnelHealthy()
            } else if isArmed && downSince == nil {
                downSince = Date()
            }
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

    private func appendLogAsync(_ message: String) {
        DispatchQueue.main.async { [weak self] in self?.appendLog(message) }
    }

    // MARK: - Supervisor

    private func startSupervisor() {
        stopSupervisor()
        let timer = Timer(timeInterval: 5, repeats: true) { [weak self] _ in
            self?.supervisorTick()
        }
        RunLoop.main.add(timer, forMode: .common)
        supervisorTimer = timer
    }

    private func stopSupervisor() {
        supervisorTimer?.invalidate()
        supervisorTimer = nil
    }

    private func supervisorTick() {
        guard isArmed else { return }
        guard status != .tunnelConnected else { return }

        if downSince == nil { downSince = Date() }
        let downFor = Date().timeIntervalSince(downSince!)

        if downFor >= happHintTimeout && !needsHappToggle {
            needsHappToggle = true
            appendLog("Still down after \(Int(downFor))s - Happ is most likely holding the network")
        }

        if downFor >= stuckTimeout && pendingRestart == nil {
            scheduleRestart(reason: "no tunnel for \(Int(downFor))s")
        }
    }

    private func scheduleRestart(reason: String) {
        guard isArmed, pendingRestart == nil else { return }
        if downSince == nil { downSince = Date() }

        let delay = restartBackoff[min(restartAttempt, restartBackoff.count - 1)]
        restartAttempt += 1
        appendLog("Restarting joiner in \(Int(delay))s (\(reason))")

        let work = DispatchWorkItem { [weak self] in
            guard let self = self, self.isArmed else { return }
            self.pendingRestart = nil
            self.restartCount += 1
            self.stopJoiner(keepAlive: true)
            self.launchHeadless()
        }
        pendingRestart = work
        DispatchQueue.main.asyncAfter(deadline: .now() + delay, execute: work)
    }

    /// Skips the backoff - used when something changed for the better (network came back,
    /// user opened the app), so waiting would only waste time.
    private func restartNow(reason: String) {
        guard isArmed else { return }
        pendingRestart?.cancel()
        pendingRestart = nil
        restartAttempt = 0
        scheduleRestart(reason: reason)
    }

    private func onTunnelHealthy() {
        downSince = nil
        restartAttempt = 0
        pendingRestart?.cancel()
        pendingRestart = nil
        needsHappToggle = false
    }

    private func startPathMonitor() {
        let monitor = NWPathMonitor()
        monitor.pathUpdateHandler = { [weak self] path in
            DispatchQueue.main.async {
                guard let self = self else { return }
                let up = path.status == .satisfied
                let wasUp = self.networkUp
                self.networkUp = up
                guard up, !wasUp else { return }
                self.appendLog("Network is back")
                if self.isArmed && self.status != .tunnelConnected {
                    self.restartNow(reason: "network restored")
                }
            }
        }
        monitor.start(queue: DispatchQueue.global(qos: .utility))
        pathMonitor = monitor
    }

    @objc private func appDidBecomeActive() {
        guard isArmed, status != .tunnelConnected else { return }
        guard let down = downSince, Date().timeIntervalSince(down) > 10 else { return }
        restartNow(reason: "app foregrounded")
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

    func openHappProxy() {
        let creds = "\(activeSocksUser):\(activeSocksPass)"
        let credsB64 = Data(creds.utf8).base64EncodedString()
        // Fixed name: the port and the credentials no longer change, so Happ keeps a single
        // WLB profile instead of piling up WLB-1080 / WLB-1081 / WLB-1082.
        let proxyUri = "socks://\(credsB64)@127.0.0.1:\(socksPort)#WLB"
        UIPasteboard.general.string = proxyUri
        showToast(NSLocalizedString("toast_happ_params_copied", comment: ""))
    }

    /// Hands Happ a routing profile that sends wb.ru straight out instead of into the tunnel.
    /// Without it the joiner's own signalling traffic is captured by the VPN it feeds, so a
    /// dropped tunnel can never come back while the VPN is on.
    func openHappRouting() {
        let profile: [String: Any] = [
            "Name": "WLB direct",
            "GlobalProxy": "true",
            "DirectSites": ["domain:wb.ru", "domain:wildberries.ru"],
            "DirectIp": ["127.0.0.0/8"],
            "ProxySites": [],
            "ProxyIp": [],
            "BlockSites": [],
            "BlockIp": [],
        ]
        guard let data = try? JSONSerialization.data(withJSONObject: profile) else { return }
        let link = "happ://routing/onadd/\(data.base64EncodedString())"
        UIPasteboard.general.string = link
        if let url = URL(string: link) {
            UIApplication.shared.open(url)
        }
        showToast(NSLocalizedString("toast_happ_routing_sent", comment: ""))
    }

}
