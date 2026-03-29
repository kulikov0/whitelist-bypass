package bypass.whitelist.ui

import android.widget.Button
import android.widget.TextView
import androidx.appcompat.app.AppCompatActivity
import bypass.whitelist.R
import bypass.whitelist.tunnel.TunnelMode
import bypass.whitelist.tunnel.VpnStatus
import bypass.whitelist.util.Callback

class StatusBarController(
    private val activity: AppCompatActivity,
    private val statusBar: TextView,
    private val goButton: Button,
    private val onConnect: Callback,
    private val onDisconnect: Callback,
) {
    var connected = false
        private set

    var tunnelMode: TunnelMode = TunnelMode.DC

    fun setConnected(value: Boolean) {
        connected = value
        goButton.setText(if (value) R.string.btn_disconnect else R.string.btn_go)
        goButton.setOnClickListener {
            if (connected) onDisconnect() else onConnect()
        }
    }

    fun setStatus(status: VpnStatus) {
        statusBar.text = activity.getString(R.string.status_format, tunnelMode.label, activity.getString(status.labelRes))
        val colorRes = when (status) {
            VpnStatus.TUNNEL_ACTIVE -> R.color.status_active
            VpnStatus.CONNECTING,
            VpnStatus.CALL_CONNECTED,
            VpnStatus.DATACHANNEL_OPEN -> R.color.status_connecting
            VpnStatus.TUNNEL_LOST,
            VpnStatus.DATACHANNEL_LOST -> R.color.status_warning
            VpnStatus.CALL_DISCONNECTED,
            VpnStatus.CALL_FAILED -> R.color.status_error
            VpnStatus.STARTING -> R.color.status_idle
        }
        statusBar.setBackgroundColor(activity.getColor(colorRes))
    }

    fun setIdle() {
        statusBar.text = activity.getString(R.string.status_format, tunnelMode.label, activity.getString(R.string.status_idle))
        statusBar.setBackgroundColor(activity.getColor(R.color.status_idle))
    }
}
