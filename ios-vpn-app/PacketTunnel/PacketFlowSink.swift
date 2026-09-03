import NetworkExtension
import Mobile

final class PacketFlowSink: NSObject, IosPacketFlowSinkProtocol {
    private weak var flow: NEPacketTunnelFlow?

    init(flow: NEPacketTunnelFlow) {
        self.flow = flow
    }

    func writePacket(_ packet: Data?, afFamily: Int) {
        guard let packet, let flow else { return }
        flow.writePackets([packet], withProtocols: [NSNumber(value: afFamily)])
    }
}
