import Foundation
import os

enum Log {
    static let subsystem = "whitelist.bypass.vpn"
    static let tunnel = Logger(subsystem: subsystem, category: "tunnel")
    static let relay = Logger(subsystem: subsystem, category: "relay")
    static let status = Logger(subsystem: subsystem, category: "status")
    static let app = Logger(subsystem: subsystem, category: "app")
}

enum AppIdentifiers {
    static let appGroup = "group.whitelist.bypass.vpn"
    static let tunnelBundleId = "whitelist.bypass.vpn.PacketTunnel"
    static let vpnLocalizedDescription = "Whitelist Bypass"

    static var relayLogURL: URL? {
        FileManager.default.containerURL(forSecurityApplicationGroupIdentifier: appGroup)?.appendingPathComponent("relay.log")
    }
}

enum VpnConfig {
    static let address = "10.0.0.2"
    static let subnetMask = "255.255.255.255"
    static let address6 = "fd00::2"
    static let prefix6: NSNumber = 128
    static let mtu = 1500
    static let dnsPrimary = "8.8.8.8"
    static let dnsSecondary = "8.8.4.4"
    static let tunnelRemote = "127.0.0.1"
    static let defaultSocksPort = 1080
}

enum ConfigKey {
    static let url = "url"
    static let displayName = "displayName"
    static let platform = "platform"
    static let tunnelMode = "tunnelMode"
    static let socksPort = "socksPort"
    static let socksUser = "socksUser"
    static let socksPass = "socksPass"
    static let vp8Fps = "vp8Fps"
    static let vp8Batch = "vp8Batch"
    static let dualTrack = "dualTrack"
    static let reliable = "reliable"
    static let debug = "debug"
    static let dnsPrimary = "dnsPrimary"
    static let dnsSecondary = "dnsSecondary"
}

struct LogSnapshot: Codable {
    let status: String
    let revision: Int
    let lines: [String]
}

enum GoStatus {
    static let idle = "IDLE"
    static let ready = "READY"
    static let connecting = "CONNECTING"
    static let reconnecting = "RECONNECTING"
    static let tunnelConnected = "TUNNEL_CONNECTED"
    static let tunnelLost = "TUNNEL_LOST"
    static let error = "ERROR"
}

enum CallPlatform: String {
    case vk
    case telemost
    case wbstream
    case dion

    static let wbstreamPrefix = "wbstream://"
    static let dionPrefix = "dion://"
    static let dionEventInfix = "dion.vc/event/"

    static func detect(url: String) -> CallPlatform {
        if url.hasPrefix(dionPrefix) || url.contains(dionEventInfix) { return .dion }
        if url.hasPrefix(wbstreamPrefix) { return .wbstream }
        if url.contains("telemost") { return .telemost }
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

    var glyph: String {
        switch self {
        case .vk: return "VK"
        case .telemost: return "TM"
        case .wbstream: return "WB"
        case .dion: return "DN"
        }
    }
}

enum TunnelMode: String, CaseIterable, Identifiable, Codable {
    case dc
    case video
    var id: String { rawValue }
    var label: String { self == .dc ? NSLocalizedString("settings_tunnel_dc", comment: "") : NSLocalizedString("settings_tunnel_video", comment: "") }
}

struct TunnelConfig {
    var url: String
    var displayName: String
    var platform: CallPlatform
    var tunnelMode: TunnelMode
    var socksPort: Int
    var socksUser: String
    var socksPass: String
    var vp8Fps: Int
    var vp8Batch: Int
    var dualTrack: Bool
    var reliable: Bool
    var debug: Bool
    var dnsServers: [String]

    var roomId: String { CallPlatform.extractRoomId(url: url) }
}

extension TunnelConfig {
    init(dictionary d: [String: Any]) {
        let primary = d[ConfigKey.dnsPrimary] as? String ?? ""
        let secondary = d[ConfigKey.dnsSecondary] as? String ?? ""
        let dns = [primary, secondary].filter { !$0.isEmpty }

        self.init(
            url: d[ConfigKey.url] as? String ?? "",
            displayName: d[ConfigKey.displayName] as? String ?? "",
            platform: CallPlatform(rawValue: d[ConfigKey.platform] as? String ?? "") ?? .vk,
            tunnelMode: TunnelMode(rawValue: d[ConfigKey.tunnelMode] as? String ?? "") ?? .video,
            socksPort: (d[ConfigKey.socksPort] as? NSNumber)?.intValue ?? VpnConfig.defaultSocksPort,
            socksUser: d[ConfigKey.socksUser] as? String ?? "",
            socksPass: d[ConfigKey.socksPass] as? String ?? "",
            vp8Fps: (d[ConfigKey.vp8Fps] as? NSNumber)?.intValue ?? 24,
            vp8Batch: (d[ConfigKey.vp8Batch] as? NSNumber)?.intValue ?? 30,
            dualTrack: (d[ConfigKey.dualTrack] as? NSNumber)?.boolValue ?? false,
            reliable: (d[ConfigKey.reliable] as? NSNumber)?.boolValue ?? false,
            debug: (d[ConfigKey.debug] as? NSNumber)?.boolValue ?? false,
            dnsServers: dns
        )
    }

    var dictionary: [String: Any] {
        [
            ConfigKey.url: url,
            ConfigKey.displayName: displayName,
            ConfigKey.platform: platform.rawValue,
            ConfigKey.tunnelMode: tunnelMode.rawValue,
            ConfigKey.socksPort: socksPort,
            ConfigKey.socksUser: socksUser,
            ConfigKey.socksPass: socksPass,
            ConfigKey.vp8Fps: vp8Fps,
            ConfigKey.vp8Batch: vp8Batch,
            ConfigKey.dualTrack: dualTrack,
            ConfigKey.reliable: reliable,
            ConfigKey.debug: debug,
            ConfigKey.dnsPrimary: dnsServers.first ?? "",
            ConfigKey.dnsSecondary: dnsServers.count > 1 ? dnsServers[1] : "",
        ]
    }
}

enum TokenCache {
    private static let defaults = UserDefaults(suiteName: AppIdentifiers.appGroup)

    static func save(_ key: String, _ value: String) {
        defaults?.set(value, forKey: "cache_\(key)")
    }

    static func load(_ key: String) -> String {
        defaults?.string(forKey: "cache_\(key)") ?? ""
    }

    static func clear(_ key: String) {
        defaults?.removeObject(forKey: "cache_\(key)")
    }
}
