package bypass.whitelist

enum class VpnStatus(val label: String) {
    STARTING("Starting..."),
    CONNECTING("Connecting..."),
    CALL_CONNECTED("Call connected"),
    DATACHANNEL_OPEN("DataChannel open"),
    DATACHANNEL_LOST("DataChannel lost"),
    TUNNEL_ACTIVE("Tunnel active"),
    TUNNEL_LOST("Tunnel lost, reconnecting..."),
    CALL_DISCONNECTED("Call disconnected"),
    CALL_FAILED("Call failed")
}
