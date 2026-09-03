import Foundation
import Combine

enum SocksAuthMode: String, CaseIterable, Identifiable {
    case auto = "AUTO"
    case manual = "MANUAL"
    var id: String { rawValue }
    var label: String { self == .auto ? NSLocalizedString("settings_auth_auto", comment: "") : NSLocalizedString("settings_auth_manual", comment: "") }
    var sub: String { self == .auto ? NSLocalizedString("proxy_auth_auto_sub", comment: "") : NSLocalizedString("proxy_auth_manual_sub", comment: "") }
}

enum DnsMode: String, CaseIterable, Identifiable {
    case system = "SYSTEM"
    case custom = "CUSTOM"
    var id: String { rawValue }
    var label: String { self == .system ? NSLocalizedString("dns_mode_system", comment: "") : NSLocalizedString("dns_mode_custom", comment: "") }
}

enum DKey {
    static let savedCalls = "savedCalls"
    static let activeCallId = "activeCallId"
    static let tunnelMode = "tunnelMode"
    static let autofillEnabled = "autofillEnabled"
    static let autofillName = "autofillName"
    static let themeMode = "themeMode"
    static let connectOnStart = "connectOnStart"
    static let socksPort = "socksPort"
    static let socksAuthMode = "socksAuthMode"
    static let socksUser = "socksUser"
    static let socksPass = "socksPass"
    static let vp8Fps = "vp8Fps"
    static let vp8Batch = "vp8Batch"
    static let dualTrack = "dualTrack"
    static let reliable = "reliable"
    static let debug = "debug"
    static let dnsMode = "dnsMode"
    static let dnsPrimary = "dnsPrimary"
    static let dnsSecondary = "dnsSecondary"
}

enum Defaults {
    static let autofillName = "Hello"
    static let vp8Fps = 24
    static let vp8Batch = 30
}

final class AppState: ObservableObject {
    @Published var savedCalls: [CallConfig] { didSet { store.set(CallConfig.encodeList(savedCalls), forKey: DKey.savedCalls) } }
    @Published var activeCallId: String? {
        didSet {
            if let activeCallId { store.set(activeCallId, forKey: DKey.activeCallId) }
            else { store.removeObject(forKey: DKey.activeCallId) }
        }
    }

    @Published var tunnelMode: TunnelMode { didSet { store.set(tunnelMode.rawValue, forKey: DKey.tunnelMode) } }
    @Published var autofillEnabled: Bool { didSet { store.set(autofillEnabled, forKey: DKey.autofillEnabled) } }
    @Published var autofillName: String { didSet { store.set(autofillName, forKey: DKey.autofillName) } }
    @Published var themeMode: ThemeMode { didSet { store.set(themeMode.rawValue, forKey: DKey.themeMode) } }
    @Published var connectOnStart: Bool { didSet { store.set(connectOnStart, forKey: DKey.connectOnStart) } }
    @Published var socksPort: Int { didSet { store.set(socksPort, forKey: DKey.socksPort) } }
    @Published var socksAuthMode: SocksAuthMode { didSet { store.set(socksAuthMode.rawValue, forKey: DKey.socksAuthMode) } }
    @Published var manualSocksUser: String { didSet { store.set(manualSocksUser, forKey: DKey.socksUser) } }
    @Published var manualSocksPass: String { didSet { store.set(manualSocksPass, forKey: DKey.socksPass) } }
    @Published var vp8Fps: Int { didSet { store.set(vp8Fps, forKey: DKey.vp8Fps) } }
    @Published var vp8Batch: Int { didSet { store.set(vp8Batch, forKey: DKey.vp8Batch) } }
    @Published var dualTrack: Bool { didSet { store.set(dualTrack, forKey: DKey.dualTrack) } }
    @Published var reliable: Bool { didSet { store.set(reliable, forKey: DKey.reliable) } }
    @Published var debug: Bool { didSet { store.set(debug, forKey: DKey.debug) } }
    @Published var dnsMode: DnsMode { didSet { store.set(dnsMode.rawValue, forKey: DKey.dnsMode) } }
    @Published var dnsPrimary: String { didSet { store.set(dnsPrimary, forKey: DKey.dnsPrimary) } }
    @Published var dnsSecondary: String { didSet { store.set(dnsSecondary, forKey: DKey.dnsSecondary) } }

    var activeSocksUser: String { socksAuthMode == .manual ? manualSocksUser : autoUser }
    var activeSocksPass: String { socksAuthMode == .manual ? manualSocksPass : autoPass }
    var activeCall: CallConfig? { savedCalls.first { $0.id == activeCallId } }

    private let store = UserDefaults(suiteName: AppIdentifiers.appGroup) ?? .standard
    private let autoUser: String
    private let autoPass: String

    init() {
        let s = UserDefaults(suiteName: AppIdentifiers.appGroup) ?? .standard
        savedCalls = CallConfig.decodeList(s.string(forKey: DKey.savedCalls) ?? "[]")
        activeCallId = s.string(forKey: DKey.activeCallId)
        tunnelMode = TunnelMode(rawValue: s.string(forKey: DKey.tunnelMode) ?? "") ?? .video
        autofillEnabled = s.object(forKey: DKey.autofillEnabled) as? Bool ?? true
        autofillName = s.string(forKey: DKey.autofillName) ?? Defaults.autofillName
        themeMode = ThemeMode(rawValue: s.string(forKey: DKey.themeMode) ?? "") ?? .system
        connectOnStart = s.object(forKey: DKey.connectOnStart) as? Bool ?? false
        socksPort = s.object(forKey: DKey.socksPort) as? Int ?? VpnConfig.defaultSocksPort
        socksAuthMode = SocksAuthMode(rawValue: s.string(forKey: DKey.socksAuthMode) ?? "") ?? .auto
        manualSocksUser = s.string(forKey: DKey.socksUser) ?? ""
        manualSocksPass = s.string(forKey: DKey.socksPass) ?? ""
        vp8Fps = s.object(forKey: DKey.vp8Fps) as? Int ?? Defaults.vp8Fps
        vp8Batch = s.object(forKey: DKey.vp8Batch) as? Int ?? Defaults.vp8Batch
        dualTrack = s.object(forKey: DKey.dualTrack) as? Bool ?? false
        reliable = s.object(forKey: DKey.reliable) as? Bool ?? false
        debug = s.object(forKey: DKey.debug) as? Bool ?? false
        dnsMode = DnsMode(rawValue: s.string(forKey: DKey.dnsMode) ?? "") ?? .system
        dnsPrimary = s.string(forKey: DKey.dnsPrimary) ?? VpnConfig.dnsPrimary
        dnsSecondary = s.string(forKey: DKey.dnsSecondary) ?? VpnConfig.dnsSecondary

        let chars = Array("abcdefghijklmnopqrstuvwxyz0123456789")
        autoUser = String((0..<16).map { _ in chars.randomElement()! })
        autoPass = String((0..<24).map { _ in chars.randomElement()! })
    }

    func addCall(name: String, url: String) {
        let call = CallConfig.make(name: name, url: url)
        savedCalls.append(call)
        activeCallId = call.id
    }

    func removeCall(id: String) {
        savedCalls.removeAll { $0.id == id }
        if activeCallId == id { activeCallId = savedCalls.first?.id }
    }

    func renameCall(id: String, name: String) {
        guard let index = savedCalls.firstIndex(where: { $0.id == id }) else { return }
        let trimmed = name.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return }
        savedCalls[index].name = trimmed
    }

    func updateCall(_ call: CallConfig) {
        guard let index = savedCalls.firstIndex(where: { $0.id == call.id }) else { return }
        savedCalls[index] = call
    }

    func selectCall(id: String) { activeCallId = id }

    func forgetAllCalls() {
        savedCalls = []
        activeCallId = nil
    }

    func resetAllSettings() {
        tunnelMode = .video
        autofillEnabled = true
        autofillName = Defaults.autofillName
        themeMode = .system
        connectOnStart = false
        socksPort = VpnConfig.defaultSocksPort
        socksAuthMode = .auto
        manualSocksUser = ""
        manualSocksPass = ""
        vp8Fps = Defaults.vp8Fps
        vp8Batch = Defaults.vp8Batch
        dualTrack = false
        reliable = false
        debug = false
        dnsMode = .system
        dnsPrimary = VpnConfig.dnsPrimary
        dnsSecondary = VpnConfig.dnsSecondary
    }

    func tunnelConfig() -> TunnelConfig? {
        guard let call = activeCall else { return nil }
        let platform = call.platform
        var mode = call.tunnelMode ?? tunnelMode
        if mode == .dc && (platform == .telemost || platform == .dion) {
            mode = .video
        }
        let dns = dnsMode == .custom ? [dnsPrimary, dnsSecondary].filter { !$0.isEmpty } : []
        return TunnelConfig(
            url: call.url,
            displayName: autofillEnabled ? autofillName : "",
            platform: platform,
            tunnelMode: mode,
            socksPort: socksPort,
            socksUser: activeSocksUser,
            socksPass: activeSocksPass,
            vp8Fps: call.vp8Fps ?? vp8Fps,
            vp8Batch: call.vp8Batch ?? vp8Batch,
            dualTrack: call.dualTrack ?? dualTrack,
            reliable: call.reliable ?? reliable,
            debug: debug,
            dnsServers: dns
        )
    }
}

enum RandomNames {
    static let pool: [String] = {
        guard let url = Bundle.main.url(forResource: "names", withExtension: "txt"),
              let text = try? String(contentsOf: url, encoding: .utf8) else { return [Defaults.autofillName] }
        let names = text.split(whereSeparator: \.isNewline).map { $0.trimmingCharacters(in: .whitespaces) }.filter { !$0.isEmpty }
        return names.isEmpty ? [Defaults.autofillName] : names
    }()
}
