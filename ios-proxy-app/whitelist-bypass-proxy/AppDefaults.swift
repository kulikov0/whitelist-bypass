import Foundation

enum DefaultsKeys {
    static let lastUrl = "lastUrl"
    static let socksPort = "socksPort"
    static let tunnelMode = "tunnelMode"
    static let displayName = "displayName"
    static let showLogs = "showLogs"
    static let socksAuthMode = "socksAuthMode"
    static let socksUser = "socksUser"
    static let socksPass = "socksPass"
}

struct AppDefaults {
    private static let defaults = UserDefaults.standard

    static var lastUrl: String {
        get { defaults.string(forKey: DefaultsKeys.lastUrl) ?? "" }
        set { defaults.set(newValue, forKey: DefaultsKeys.lastUrl) }
    }

    static var socksPort: Int {
        get { defaults.object(forKey: DefaultsKeys.socksPort) as? Int ?? 1080 }
        set { defaults.set(newValue, forKey: DefaultsKeys.socksPort) }
    }

    static var tunnelMode: TunnelMode {
        get { TunnelMode(rawValue: defaults.string(forKey: DefaultsKeys.tunnelMode) ?? "") ?? .video }
        set { defaults.set(newValue.rawValue, forKey: DefaultsKeys.tunnelMode) }
    }

    static var displayName: String {
        get { defaults.string(forKey: DefaultsKeys.displayName) ?? "Hello" }
        set { defaults.set(newValue, forKey: DefaultsKeys.displayName) }
    }

    static var showLogs: Bool {
        get { defaults.object(forKey: DefaultsKeys.showLogs) as? Bool ?? true }
        set { defaults.set(newValue, forKey: DefaultsKeys.showLogs) }
    }

    static var socksAuthMode: SocksAuthMode {
        get { SocksAuthMode(rawValue: defaults.string(forKey: DefaultsKeys.socksAuthMode) ?? "") ?? .auto }
        set { defaults.set(newValue.rawValue, forKey: DefaultsKeys.socksAuthMode) }
    }

    static var socksUser: String {
        get { defaults.string(forKey: DefaultsKeys.socksUser) ?? "" }
        set { defaults.set(newValue, forKey: DefaultsKeys.socksUser) }
    }

    static var socksPass: String {
        get { defaults.string(forKey: DefaultsKeys.socksPass) ?? "" }
        set { defaults.set(newValue, forKey: DefaultsKeys.socksPass) }
    }
}
