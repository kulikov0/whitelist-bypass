import NetworkExtension
import Mobile

final class PacketTunnelProvider: NEPacketTunnelProvider {
    private let callback = HeadlessCallback()
    private var config: TunnelConfig?
    private var dataPathStarted = false
    private var tornDown = false
    private var sink: PacketFlowSink?

    override func startTunnel(options: [String: NSObject]?, completionHandler: @escaping (Error?) -> Void) {
        let proto = protocolConfiguration as? NETunnelProviderProtocol
        var config = TunnelConfig(dictionary: proto?.providerConfiguration ?? [:])
        let chosenPort = PortPicker.free(preferred: config.socksPort)
        if chosenPort != config.socksPort {
            Log.tunnel.log("socks port \(config.socksPort) busy, falling back to \(chosenPort)")
            config.socksPort = chosenPort
        }
        self.config = config
        callback.provider = self

        LogWriter.shared.reset()
        StatusHolder.shared.set(GoStatus.connecting)
        LogWriter.shared.append("startTunnel platform=\(config.platform.rawValue) socks=\(config.socksPort) mode=\(config.tunnelMode.rawValue)")
        #if targetEnvironment(simulator)
        LogWriter.shared.append("environment: iOS Simulator (utun may not route real device traffic)")
        #else
        LogWriter.shared.append("environment: physical device")
        #endif
        IosSetDebug(config.debug)
        JoinerFactory.make(for: config.platform).start(config, callback: callback)

        setTunnelNetworkSettings(makeNetworkSettings(config, captureAll: false)) { error in
            if let error {
                LogWriter.shared.append("network settings error: \(error.localizedDescription)")
                completionHandler(error)
                return
            }
            LogWriter.shared.append("utun up without capture, join over normal network")
            completionHandler(nil)
        }
    }

    override func stopTunnel(with reason: NEProviderStopReason, completionHandler: @escaping () -> Void) {
        tornDown = true
        Log.tunnel.log("stopTunnel reason=\(reason.rawValue)")
        IosStopTun2Socks()
        IosStopCaptchaProxy()
        IosStopHeadless()
        StatusHolder.shared.set(GoStatus.idle)
        LogWriter.shared.append("tunnel stopped")
        completionHandler()
    }

    override func handleAppMessage(_ messageData: Data, completionHandler: ((Data?) -> Void)?) {
        let snapshot = LogSnapshot(status: StatusHolder.shared.get(),
                                   revision: LogWriter.shared.revision(),
                                   lines: LogWriter.shared.displayText())
        completionHandler?(try? JSONEncoder().encode(snapshot))
    }

    func relayConnected() {
        guard !tornDown, !dataPathStarted, let config else { return }
        dataPathStarted = true
        Log.tunnel.log("relay connected, applying full routes before forwarding")
        LogWriter.shared.append("relay up, applying full routes")
        setTunnelNetworkSettings(makeNetworkSettings(config, captureAll: true)) { [weak self] error in
            guard let self else { return }
            if let error {
                self.failSession("route apply error: \(error.localizedDescription)")
                return
            }
            LogWriter.shared.append("full routes applied, starting packetFlow tun2socks socks \(config.socksPort)")
            let sink = PacketFlowSink(flow: self.packetFlow)
            self.sink = sink
            var tunErr: NSError?
            let ok = IosStartTun2SocksPacketFlow(sink, VpnConfig.mtu, config.socksPort, config.socksUser, config.socksPass, &tunErr)
            if !ok {
                self.failSession(tunErr?.localizedDescription ?? "tun2socks failed")
                return
            }
            LogWriter.shared.append("packetFlow tun2socks up, pumping packets")
            self.pumpInbound()
        }
    }

    func failSession(_ reason: String) {
        guard !tornDown else { return }
        tornDown = true
        Log.tunnel.error("failSession, tearing down: \(reason, privacy: .public)")
        LogWriter.shared.append("session failed: \(reason)")
        IosStopTun2Socks()
        IosStopCaptchaProxy()
        IosStopHeadless()
        cancelTunnelWithError(NSError(domain: "PacketTunnel", code: 2, userInfo: [NSLocalizedDescriptionKey: reason]))
    }

    private func pumpInbound() {
        packetFlow.readPackets { [weak self] packets, _ in
            guard let self, !self.tornDown else { return }
            for packet in packets {
                IosInputPacket(packet)
            }
            self.pumpInbound()
        }
    }

    private func makeNetworkSettings(_ config: TunnelConfig, captureAll: Bool) -> NEPacketTunnelNetworkSettings {
        let settings = NEPacketTunnelNetworkSettings(tunnelRemoteAddress: VpnConfig.tunnelRemote)

        let ipv4 = NEIPv4Settings(addresses: [VpnConfig.address], subnetMasks: [VpnConfig.subnetMask])
        ipv4.includedRoutes = captureAll ? [NEIPv4Route.default()] : []
        settings.ipv4Settings = ipv4

        let ipv6 = NEIPv6Settings(addresses: [VpnConfig.address6], networkPrefixLengths: [VpnConfig.prefix6])
        ipv6.includedRoutes = captureAll ? [NEIPv6Route.default()] : []
        settings.ipv6Settings = ipv6

        if captureAll {
            let servers = config.dnsServers.isEmpty ? [VpnConfig.dnsPrimary, VpnConfig.dnsSecondary] : config.dnsServers
            let dnsSettings = NEDNSSettings(servers: servers)
            dnsSettings.matchDomains = [""]
            settings.dnsSettings = dnsSettings
        }
        settings.mtu = NSNumber(value: VpnConfig.mtu)
        return settings
    }
}
