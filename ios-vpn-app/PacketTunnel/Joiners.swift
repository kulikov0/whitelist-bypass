import Foundation
import Mobile

protocol HeadlessJoiner {
    func start(_ config: TunnelConfig, callback: IosHeadlessCallbackProtocol)
}

enum JoinerFactory {
    static func make(for platform: CallPlatform) -> HeadlessJoiner {
        switch platform {
        case .telemost: return TelemostJoiner()
        case .wbstream: return WBStreamJoiner()
        case .dion: return DionJoiner()
        case .vk: return VKJoiner()
        }
    }
}

private func sendJoinParams<T: Encodable>(_ params: T) {
    guard let data = try? JSONEncoder().encode(params),
          let json = String(data: data, encoding: .utf8) else { return }
    IosSendJoinParams(json)
}

struct TelemostJoiner: HeadlessJoiner {
    private struct Params: Encodable {
        let joinLink: String
        let displayName: String
        let vp8Fps: Int
        let vp8Batch: Int
        let dualTrack: Bool
        let reliable: Bool
    }

    func start(_ config: TunnelConfig, callback: IosHeadlessCallbackProtocol) {
        IosStartTelemostHeadless(config.socksPort, config.socksUser, config.socksPass, callback)
        sendJoinParams(Params(
            joinLink: config.url,
            displayName: config.displayName,
            vp8Fps: config.vp8Fps,
            vp8Batch: config.vp8Batch,
            dualTrack: config.dualTrack,
            reliable: config.reliable
        ))
    }
}

struct WBStreamJoiner: HeadlessJoiner {
    private struct Params: Encodable {
        let roomId: String
        let displayName: String
        let tunnelMode: String
        let vp8Fps: Int
        let vp8Batch: Int
        let dualTrack: Bool
        let reliable: Bool
    }

    func start(_ config: TunnelConfig, callback: IosHeadlessCallbackProtocol) {
        IosStartWBStreamHeadless(config.socksPort, config.socksUser, config.socksPass, callback)
        sendJoinParams(Params(
            roomId: config.roomId,
            displayName: config.displayName,
            tunnelMode: config.tunnelMode.rawValue,
            vp8Fps: config.vp8Fps,
            vp8Batch: config.vp8Batch,
            dualTrack: config.dualTrack,
            reliable: config.reliable
        ))
    }
}

struct DionJoiner: HeadlessJoiner {
    private struct Params: Encodable {
        let roomId: String
        let displayName: String
        let vp8Fps: Int
        let vp8Batch: Int
    }

    func start(_ config: TunnelConfig, callback: IosHeadlessCallbackProtocol) {
        IosStartDionHeadless(config.socksPort, config.socksUser, config.socksPass, callback)
        sendJoinParams(Params(
            roomId: config.roomId,
            displayName: config.displayName,
            vp8Fps: config.vp8Fps,
            vp8Batch: config.vp8Batch
        ))
    }
}

struct VKJoiner: HeadlessJoiner {
    func start(_ config: TunnelConfig, callback: IosHeadlessCallbackProtocol) {
        IosStartVKHeadless(
            config.socksPort,
            config.socksUser,
            config.socksPass,
            config.url,
            config.displayName,
            config.tunnelMode.rawValue,
            config.vp8Fps,
            config.vp8Batch,
            config.dualTrack,
            callback
        )
    }
}
