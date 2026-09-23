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
    static let vp8Fps = "vp8Fps"
    static let vp8Batch = "vp8Batch"
    static let dualTrack = "dualTrack"
    static let reliable = "reliable"
    static let debug = "debug"
    static let keepaliveMinMs = "keepaliveMinMs"
    static let keepaliveMaxMs = "keepaliveMaxMs"
    static let skipVideoTrack = "skipVideoTrack"
    static let disableMdns = "disableMdns"
}

enum VP8Defaults {
    static let fps: Int = 24
    static let batch: Int = 30
    // Idle keepalive spread. The original 60..200ms emits ~9 frames per second
    // into silence, which never lets the radio sleep. Stretched to 3..8s;
    // verified against a live WB Stream room, the tunnel holds.
    static let keepaliveMinMs: Int = 3000
    static let keepaliveMaxMs: Int = 8000
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
        // Off by default: with logs on, every line from Go wakes the main thread
        // and redraws SwiftUI. Anyone who needs them can switch them back on.
        get { defaults.object(forKey: DefaultsKeys.showLogs) as? Bool ?? false }
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

    static var vp8Fps: Int {
        get { defaults.object(forKey: DefaultsKeys.vp8Fps) as? Int ?? VP8Defaults.fps }
        set { defaults.set(newValue, forKey: DefaultsKeys.vp8Fps) }
    }

    static var vp8Batch: Int {
        get { defaults.object(forKey: DefaultsKeys.vp8Batch) as? Int ?? VP8Defaults.batch }
        set { defaults.set(newValue, forKey: DefaultsKeys.vp8Batch) }
    }

    static var dualTrack: Bool {
        get { defaults.object(forKey: DefaultsKeys.dualTrack) as? Bool ?? false }
        set { defaults.set(newValue, forKey: DefaultsKeys.dualTrack) }
    }

    static var reliable: Bool {
        get { defaults.object(forKey: DefaultsKeys.reliable) as? Bool ?? false }
        set { defaults.set(newValue, forKey: DefaultsKeys.reliable) }
    }

    static var keepaliveMinMs: Int {
        get { defaults.object(forKey: DefaultsKeys.keepaliveMinMs) as? Int ?? VP8Defaults.keepaliveMinMs }
        set { defaults.set(newValue, forKey: DefaultsKeys.keepaliveMinMs) }
    }

    static var keepaliveMaxMs: Int {
        get { defaults.object(forKey: DefaultsKeys.keepaliveMaxMs) as? Int ?? VP8Defaults.keepaliveMaxMs }
        set { defaults.set(newValue, forKey: DefaultsKeys.keepaliveMaxMs) }
    }

    static var skipVideoTrack: Bool {
        // Off by default: if WB Stream refuses a participant without a video
        // track the tunnel simply will not come up. Enable deliberately.
        get { defaults.object(forKey: DefaultsKeys.skipVideoTrack) as? Bool ?? false }
        set { defaults.set(newValue, forKey: DefaultsKeys.skipVideoTrack) }
    }

    static var disableMdns: Bool {
        get { defaults.object(forKey: DefaultsKeys.disableMdns) as? Bool ?? true }
        set { defaults.set(newValue, forKey: DefaultsKeys.disableMdns) }
    }

    static var debug: Bool {
        get { defaults.object(forKey: DefaultsKeys.debug) as? Bool ?? false }
        set { defaults.set(newValue, forKey: DefaultsKeys.debug) }
    }
}
