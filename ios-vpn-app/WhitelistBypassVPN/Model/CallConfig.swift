import Foundation

extension CallPlatform {
    var label: String {
        switch self {
        case .vk: return "VK"
        case .telemost: return "Telemost"
        case .wbstream: return "WB Stream"
        case .dion: return "DION"
        }
    }

    var suggestedName: String {
        switch self {
        case .vk: return NSLocalizedString("suggest_name_vk", comment: "")
        case .telemost: return NSLocalizedString("suggest_name_telemost", comment: "")
        case .wbstream: return NSLocalizedString("suggest_name_wbstream", comment: "")
        case .dion: return NSLocalizedString("suggest_name_dion", comment: "")
        }
    }
}

struct CallConfig: Identifiable, Codable, Equatable {
    var id: String
    var name: String
    var url: String
    var tunnelMode: TunnelMode?
    var vp8Fps: Int?
    var vp8Batch: Int?
    var dualTrack: Bool?
    var reliable: Bool?

    var platform: CallPlatform { CallPlatform.detect(url: url) }
    var glyph: String { platform.glyph }
    var platformLabel: String { platform.label }

    static func make(name: String, url: String) -> CallConfig {
        let trimmedUrl = url.trimmingCharacters(in: .whitespacesAndNewlines)
        let trimmedName = name.trimmingCharacters(in: .whitespacesAndNewlines)
        let finalName = trimmedName.isEmpty ? CallPlatform.detect(url: trimmedUrl).suggestedName : trimmedName
        return CallConfig(id: UUID().uuidString, name: finalName, url: trimmedUrl,
                          tunnelMode: nil, vp8Fps: nil, vp8Batch: nil, dualTrack: nil, reliable: nil)
    }

    static func decodeList(_ json: String) -> [CallConfig] {
        guard let data = json.data(using: .utf8),
              let list = try? JSONDecoder().decode([CallConfig].self, from: data) else { return [] }
        return list
    }

    static func encodeList(_ list: [CallConfig]) -> String {
        guard let data = try? JSONEncoder().encode(list),
              let json = String(data: data, encoding: .utf8) else { return "[]" }
        return json
    }
}
