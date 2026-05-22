package bypass.whitelist.util

import android.content.Context
import android.content.SharedPreferences
import androidx.core.content.edit
import bypass.whitelist.tunnel.SplitTunnelingMode
import bypass.whitelist.tunnel.TunnelMode

object Prefs {

    private lateinit var prefs: SharedPreferences

    fun init(context: Context) {
        prefs = context.getSharedPreferences("app_prefs", Context.MODE_PRIVATE)
    }

    private fun profileKey(baseKey: String): String {
        val pId = currentProfileId
        return if (pId == 0) baseKey else "${baseKey}_$pId"
    }

    var connectOnStart: Boolean
        get() = prefs.getBoolean(PrefsKeys.CONNECT_ON_START, false)
        set(value) = prefs.edit { putBoolean(PrefsKeys.CONNECT_ON_START, value) }

    var currentProfileId: Int
        get() = prefs.getInt(PrefsKeys.CURRENT_PROFILE, 0)
        set(value) = prefs.edit { putInt(PrefsKeys.CURRENT_PROFILE, value) }

    var lastUrl: String
        get() = prefs.getString(profileKey(PrefsKeys.URL), "")!!
        set(value) = prefs.edit { putString(profileKey(PrefsKeys.URL), value) }

    var tunnelMode: TunnelMode
        get() {
            val name = prefs.getString(profileKey(PrefsKeys.TUNNEL_MODE), TunnelMode.VIDEO.name)!!
            return try {
                TunnelMode.valueOf(name)
            } catch (_: IllegalArgumentException) {
                TunnelMode.VIDEO
            }
        }
        set(value) = prefs.edit { putString(profileKey(PrefsKeys.TUNNEL_MODE), value.name) }

    var showLogs: Boolean
        get() = prefs.getBoolean(PrefsKeys.SHOW_LOGS, false)
        set(value) = prefs.edit { putBoolean(PrefsKeys.SHOW_LOGS, value) }

    var splitTunnelingMode: SplitTunnelingMode
        get() {
            val title = prefs.getString(profileKey(PrefsKeys.SPLIT_TUNNELING_MODE), SplitTunnelingMode.NONE.name)!!
            return try {
                SplitTunnelingMode.valueOf(title)
            } catch (_: IllegalArgumentException) {
                SplitTunnelingMode.NONE
            }
        }
        set(value) = prefs.edit { putString(profileKey(PrefsKeys.SPLIT_TUNNELING_MODE), value.name) }

    var splitTunnelingPackages: Set<String>
        get() = prefs.getStringSet(profileKey(PrefsKeys.SPLIT_TUNNELING_PACKAGES), emptySet()) ?: emptySet()
        set(value) = prefs.edit { putStringSet(profileKey(PrefsKeys.SPLIT_TUNNELING_PACKAGES), value) }

    var autoclickEnabled: Boolean
        get() = prefs.getBoolean(PrefsKeys.AUTOCLICK_ENABLED, true)
        set(value) = prefs.edit { putBoolean(PrefsKeys.AUTOCLICK_ENABLED, value) }

    var autoclickName: String
        get() = prefs.getString(PrefsKeys.AUTOCLICK_NAME, "Hello")!!
        set(value) = prefs.edit { putString(PrefsKeys.AUTOCLICK_NAME, value) }

    var headless: Boolean
        get() = prefs.getBoolean(profileKey(PrefsKeys.HEADLESS), true)
        set(value) = prefs.edit { putBoolean(profileKey(PrefsKeys.HEADLESS), value) }

    var socksPort: Long
        get() = prefs.getLong(profileKey(PrefsKeys.SOCKS_PORT), Ports.DEFAULT_SOCKS)
        set(value) = prefs.edit { putLong(profileKey(PrefsKeys.SOCKS_PORT), value) }

    var socksAuthMode: SocksAuthMode
        get() {
            val name = prefs.getString(profileKey(PrefsKeys.SOCKS_AUTH_MODE), SocksAuthMode.AUTO.name)!!
            return try {
                SocksAuthMode.valueOf(name)
            } catch (_: IllegalArgumentException) {
                SocksAuthMode.AUTO
            }
        }
        set(value) = prefs.edit { putString(profileKey(PrefsKeys.SOCKS_AUTH_MODE), value.name) }

    var socksUser: String
        get() = prefs.getString(profileKey(PrefsKeys.SOCKS_USER), "")!!
        set(value) = prefs.edit { putString(profileKey(PrefsKeys.SOCKS_USER), value) }

    var socksPass: String
        get() = prefs.getString(profileKey(PrefsKeys.SOCKS_PASS), "")!!
        set(value) = prefs.edit { putString(profileKey(PrefsKeys.SOCKS_PASS), value) }

    var proxyOnly: Boolean
        get() = prefs.getBoolean(profileKey(PrefsKeys.PROXY_ONLY), false)
        set(value) = prefs.edit { putBoolean(profileKey(PrefsKeys.PROXY_ONLY), value) }

    var dnsMode: DnsMode
        get() {
            val name = prefs.getString(profileKey(PrefsKeys.DNS_MODE), DnsMode.SYSTEM.name)!!
            return try {
                DnsMode.valueOf(name)
            } catch (_: IllegalArgumentException) {
                DnsMode.SYSTEM
            }
        }
        set(value) = prefs.edit { putString(profileKey(PrefsKeys.DNS_MODE), value.name) }

    var dnsPrimary: String
        get() = prefs.getString(profileKey(PrefsKeys.DNS_PRIMARY), Vpn.DNS_PRIMARY)!!
        set(value) = prefs.edit { putString(profileKey(PrefsKeys.DNS_PRIMARY), value) }

    var dnsSecondary: String
        get() = prefs.getString(profileKey(PrefsKeys.DNS_SECONDARY), Vpn.DNS_SECONDARY)!!
        set(value) = prefs.edit { putString(profileKey(PrefsKeys.DNS_SECONDARY), value) }

    var vp8Fps: Int
        get() = prefs.getInt(profileKey(PrefsKeys.VP8_FPS), VP8Defaults.FPS)
        set(value) = prefs.edit { putInt(profileKey(PrefsKeys.VP8_FPS), value) }

    var vp8Batch: Int
        get() = prefs.getInt(profileKey(PrefsKeys.VP8_BATCH), VP8Defaults.BATCH)
        set(value) = prefs.edit { putInt(profileKey(PrefsKeys.VP8_BATCH), value) }
}
