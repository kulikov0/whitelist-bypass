package bypass.whitelist

import android.content.Intent
import android.net.VpnService
import android.os.Bundle
import android.util.Log
import android.widget.TextView
import androidx.activity.result.contract.ActivityResultContracts
import androidx.appcompat.app.AppCompatActivity
import bypass.whitelist.tunnel.HeadlessRelayController
import bypass.whitelist.tunnel.TunnelVpnService
import bypass.whitelist.tunnel.VpnStatus

class TelemostHeadlessJoinActivity : AppCompatActivity() {

    private lateinit var relay: HeadlessRelayController
    private lateinit var statusView: TextView

    private val vpnLauncher = registerForActivityResult(ActivityResultContracts.StartActivityForResult()) {
        startVpnService()
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)

        statusView = TextView(this).apply {
            setPadding(32, 32, 32, 32)
            textSize = 14f
            text = "Starting telemost headless joiner..."
        }
        setContentView(statusView)

        val joinLink = intent.getStringExtra("join_link") ?: JOIN_LINK
        val displayName = intent.getStringExtra("display_name") ?: "Joiner"
        val tunnelMode = intent.getStringExtra("tunnel_mode") ?: "video"

        if (joinLink.isEmpty()) {
            statusView.text = "No join link provided"
            return
        }

        TunnelVpnService.onDisconnect = { runOnUiThread { relay.stop() } }

        relay = HeadlessRelayController(
            applicationInfo.nativeLibraryDir,
            relayMode = "telemost-headless-joiner",
            onLog = {},
            onStatus = { status ->
                Log.d("TM-HEADLESS", "status: $status")
                runOnUiThread { statusView.text = status.name }
                TunnelVpnService.instance?.updateStatus(status)
                when (status) {
                    VpnStatus.STARTING -> {
                        val params = org.json.JSONObject().apply {
                            put("joinLink", joinLink)
                            put("displayName", displayName)
                            put("tunnelMode", tunnelMode)
                        }
                        relay.sendJoinParams(params.toString())
                    }
                    VpnStatus.TUNNEL_ACTIVE -> runOnUiThread { requestVpn() }
                    else -> {}
                }
            },
        )
        relay.start()
    }

    override fun onDestroy() {
        TunnelVpnService.onDisconnect = null
        relay.stop()
        TunnelVpnService.instance?.stop()
        super.onDestroy()
    }

    private fun requestVpn() {
        val intent = VpnService.prepare(this)
        if (intent != null) vpnLauncher.launch(intent) else startVpnService()
    }

    private fun startVpnService() {
        startService(Intent(this, TunnelVpnService::class.java))
        Log.d("TM-HEADLESS", "VPN started")
    }

    companion object {
        private const val JOIN_LINK = ""
    }
}
