package bypass.whitelist.util

import java.security.SecureRandom

object Ports {
    const val DEFAULT_SOCKS = 1080L
    const val DC_WS = 9000L
    const val PION_SIGNALING = 9001L
}

enum class SocksAuthMode { AUTO, MANUAL }

object SocksAuth {
    private val autoUser: String
    private val autoPass: String

    init {
        val random = SecureRandom()
        val chars = "abcdefghijklmnopqrstuvwxyz0123456789"
        fun randomString(length: Int) = buildString {
            repeat(length) { append(chars[random.nextInt(chars.length)]) }
        }
        autoUser = randomString(16)
        autoPass = randomString(24)
    }

    val user: String
        get() = if (Prefs.socksAuthMode == SocksAuthMode.MANUAL) Prefs.socksUser else autoUser

    val pass: String
        get() = if (Prefs.socksAuthMode == SocksAuthMode.MANUAL) Prefs.socksPass else autoPass
}

object PrefsKeys {
    const val CONNECT_ON_START = "connect_on_start"
    const val URL = "url"
    const val TUNNEL_MODE = "tunnel_mode"
    const val SHOW_LOGS = "show_logs"
    const val SPLIT_TUNNELING_MODE = "split_tunneling_mode"
    const val SPLIT_TUNNELING_PACKAGES = "split_tunneling_packages"
    const val AUTOCLICK_ENABLED = "autoclick_enabled"
    const val AUTOCLICK_NAME = "autoclick_name"
    const val HEADLESS = "headless"
    const val SOCKS_PORT = "socks_port"
    const val SOCKS_AUTH_MODE = "socks_auth_mode"
    const val SOCKS_USER = "socks_user"
    const val SOCKS_PASS = "socks_pass"
    const val PROXY_ONLY = "proxy_only"
}

const val BLANK_URL = "about:blank"

const val DESKTOP_USER_AGENT = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

object Vpn {
    const val ADDRESS = "10.0.0.2"
    const val PREFIX_LENGTH = 32
    const val ROUTE = "0.0.0.0"
    const val MTU = 1500
    const val DNS_PRIMARY = "8.8.8.8"
    const val DNS_SECONDARY = "8.8.4.4"
    const val SESSION_NAME = "WhitelistBypass"
}
