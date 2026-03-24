package bypass.whitelist

object Ports {
    const val SOCKS = 1080L
    const val DC_WS = 9000L
    const val PION_SIGNALING = 9001L
}

object PrefsKeys {
    const val CONNECT_ON_START = "connect_on_start"
    const val URL = "url"
    const val TUNNEL_MODE = "tunnel_mode"
}

object Vpn {
    const val ADDRESS = "10.0.0.2"
    const val PREFIX_LENGTH = 32
    const val ROUTE = "0.0.0.0"
    const val MTU = 1500
    const val DNS_PRIMARY = "8.8.8.8"
    const val DNS_SECONDARY = "8.8.4.4"
    const val SESSION_NAME = "WhitelistBypass"
}