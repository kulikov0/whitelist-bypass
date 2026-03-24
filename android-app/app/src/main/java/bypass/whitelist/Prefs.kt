package bypass.whitelist

import android.content.Context
import android.content.SharedPreferences
import androidx.core.content.edit

object Prefs {

    private lateinit var prefs: SharedPreferences

    fun init(context: Context) {
        prefs = context.getSharedPreferences("app_prefs", Context.MODE_PRIVATE)
    }

    var connectOnStart: Boolean
        get() = prefs.getBoolean(PrefsKeys.CONNECT_ON_START, false)
        set(value) = prefs.edit { putBoolean(PrefsKeys.CONNECT_ON_START, value) }

    var lastUrl: String
        get() = prefs.getString(PrefsKeys.URL, "")!!
        set(value) = prefs.edit { putString(PrefsKeys.URL, value) }

    var tunnelMode: TunnelMode
        get() {
            val name = prefs.getString(PrefsKeys.TUNNEL_MODE, TunnelMode.DC.name)!!
            return try {
                TunnelMode.valueOf(name)
            } catch (_: IllegalArgumentException) {
                TunnelMode.DC
            }
        }
        set(value) = prefs.edit { putString(PrefsKeys.TUNNEL_MODE, value.name) }

    var showLogs: Boolean
        get() = prefs.getBoolean(PrefsKeys.SHOW_LOGS, false)
        set(value) = prefs.edit { putBoolean(PrefsKeys.SHOW_LOGS, value) }
}