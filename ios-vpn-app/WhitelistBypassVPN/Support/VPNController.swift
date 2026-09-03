import Foundation
import NetworkExtension
import Combine

enum ConnectionPhase {
    case disconnected
    case connecting
    case connected
    case failed
}

@MainActor
final class VPNController: ObservableObject {
    @Published var phase: ConnectionPhase = .disconnected
    @Published var statusDetail: String = ""
    @Published var uptimeText: String = "00:00:00"
    @Published var logs: [String] = []
    @Published var lastError: String?
    @Published var captchaURL: String?

    var appState: AppState?

    private var manager: NETunnelProviderManager?
    private var statusObserver: NSObjectProtocol?
    private var pollTimer: Timer?
    private var connectedSince: Date?
    private var relayUp = false
    private var lastRevision = -1
    private var lastStatus = ""

    init() {
        statusObserver = NotificationCenter.default.addObserver(
            forName: .NEVPNStatusDidChange, object: nil, queue: .main
        ) { [weak self] _ in
            Task { @MainActor in self?.refreshStatus() }
        }
        Task { await loadManager() }
        startPolling()
    }

    deinit {
        if let observer = statusObserver {
            NotificationCenter.default.removeObserver(observer)
        }
        pollTimer?.invalidate()
    }

    func connect() async {
        guard let appState else { return }
        guard let config = appState.tunnelConfig() else {
            Log.app.error("connect aborted: no active call")
            return
        }

        Log.app.log("connect requested: platform=\(config.platform.rawValue) room=\(config.roomId)")
        lastError = nil
        captchaURL = nil
        relayUp = false
        connectedSince = nil
        logs = []
        lastRevision = -1
        lastStatus = ""

        let mgr = manager ?? NETunnelProviderManager()
        let proto = NETunnelProviderProtocol()
        proto.providerBundleIdentifier = AppIdentifiers.tunnelBundleId
        proto.serverAddress = AppIdentifiers.vpnLocalizedDescription
        proto.providerConfiguration = config.dictionary
        mgr.protocolConfiguration = proto
        mgr.localizedDescription = AppIdentifiers.vpnLocalizedDescription
        mgr.isEnabled = true

        do {
            try await mgr.saveToPreferences()
            try await mgr.loadFromPreferences()
            manager = mgr
            try mgr.connection.startVPNTunnel()
        } catch {
            phase = .failed
            lastError = error.localizedDescription
            Log.app.error("start error: \(error.localizedDescription)")
        }
    }

    func disconnect() {
        Log.app.log("disconnect requested")
        manager?.connection.stopVPNTunnel()
    }

    func startPolling() {
        pollTimer?.invalidate()
        pollTimer = Timer.scheduledTimer(withTimeInterval: 0.5, repeats: true) { [weak self] _ in
            Task { @MainActor in self?.tick() }
        }
    }

    func stopPolling() {
        pollTimer?.invalidate()
        pollTimer = nil
    }

    private func loadManager() async {
        let managers = (try? await NETunnelProviderManager.loadAllFromPreferences()) ?? []
        manager = managers.first ?? NETunnelProviderManager()
        refreshStatus()
        if phase == .disconnected || phase == .failed {
            captchaURL = nil
        }
    }

    private func refreshStatus() {
        guard let connection = manager?.connection else {
            phase = .disconnected
            return
        }
        switch connection.status {
        case .connected:
            phase = relayUp ? .connected : .connecting
        case .connecting, .reasserting:
            phase = .connecting
        case .disconnecting:
            phase = .connecting
        case .disconnected, .invalid:
            relayUp = false
            connectedSince = nil
            uptimeText = "00:00:00"
            phase = lastStatus.hasPrefix(GoStatus.error) ? .failed : .disconnected
        @unknown default:
            phase = .disconnected
        }
    }

    private func tick() {
        if phase == .connected, let since = connectedSince {
            uptimeText = formatUptime(Date().timeIntervalSince(since))
        }
        pollExtension()
    }

    private func pollExtension() {
        guard let session = manager?.connection as? NETunnelProviderSession else { return }
        switch session.status {
        case .connected, .connecting, .reasserting, .disconnecting: break
        default: return
        }
        try? session.sendProviderMessage(Data()) { [weak self] data in
            guard let data, let snapshot = try? JSONDecoder().decode(LogSnapshot.self, from: data) else { return }
            Task { @MainActor in self?.applySnapshot(snapshot) }
        }
    }

    private func applySnapshot(_ snapshot: LogSnapshot) {
        if snapshot.revision != lastRevision {
            let previousCount = logs.count
            let start = snapshot.lines.count >= previousCount ? previousCount : 0
            if snapshot.lines.count > start {
                for line in snapshot.lines[start..<snapshot.lines.count] { print("[GO] \(line)") }
            }
            logs = snapshot.lines
            lastRevision = snapshot.revision
        }
        if snapshot.status != lastStatus {
            lastStatus = snapshot.status
            Log.app.log("go status -> \(snapshot.status)")
        }
        applyGoStatus(snapshot.status)
    }

    private func applyGoStatus(_ status: String) {
        if status.hasPrefix(GoStatus.error) {
            let msg = String(status.dropFirst(GoStatus.error.count).drop(while: { $0 == ":" }))
            statusDetail = msg.isEmpty ? NSLocalizedString("status_error", comment: "") : msg
            lastError = statusDetail
            captchaURL = nil
            relayUp = false
            return
        }
        if status.hasPrefix("CAPTCHA:") {
            if phase == .connecting || phase == .connected {
                captchaURL = String(status.dropFirst("CAPTCHA:".count))
                statusDetail = NSLocalizedString("status_solve_captcha", comment: "")
            } else {
                captchaURL = nil
            }
            return
        }
        if captchaURL != nil, status != "CAPTCHA" {
            captchaURL = nil
        }
        switch status {
        case GoStatus.tunnelConnected:
            if !relayUp {
                relayUp = true
                connectedSince = Date()
            }
            refreshStatus()
            statusDetail = NSLocalizedString("status_tunnel_active", comment: "")
        case GoStatus.connecting, GoStatus.reconnecting, GoStatus.tunnelLost:
            relayUp = false
            refreshStatus()
            switch status {
            case GoStatus.connecting: statusDetail = NSLocalizedString("status_connecting_ellipsis", comment: "")
            case GoStatus.reconnecting: statusDetail = NSLocalizedString("status_reconnecting", comment: "")
            default: statusDetail = NSLocalizedString("status_tunnel_lost", comment: "")
            }
        case GoStatus.ready:
            statusDetail = NSLocalizedString("status_ready", comment: "")
        default:
            statusDetail = phase == .connected ? NSLocalizedString("status_tunnel_active", comment: "") : ""
        }
    }

    private func formatUptime(_ interval: TimeInterval) -> String {
        let total = Int(interval)
        return String(format: "%02d:%02d:%02d", total / 3600, (total % 3600) / 60, total % 60)
    }
}
