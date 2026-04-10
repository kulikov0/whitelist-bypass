package bypass.whitelist

import android.content.Intent
import android.net.VpnService
import android.os.Bundle
import android.util.Log
import android.view.ViewGroup
import android.webkit.WebView
import androidx.activity.result.contract.ActivityResultContracts
import androidx.appcompat.app.AppCompatActivity
import bypass.whitelist.tunnel.HeadlessRelayController
import bypass.whitelist.tunnel.TunnelMode
import bypass.whitelist.tunnel.TunnelVpnService
import bypass.whitelist.tunnel.VpnStatus
import bypass.whitelist.util.Prefs
import bypass.whitelist.ui.VkCaptchaWebView

class HeadlessJoinActivity : AppCompatActivity() {

    private lateinit var captchaView: VkCaptchaWebView
    private lateinit var relay: HeadlessRelayController

    private val vpnLauncher = registerForActivityResult(ActivityResultContracts.StartActivityForResult()) {
        startVpnService()
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)

        val webView = WebView(this)
        webView.layoutParams = ViewGroup.LayoutParams(
            ViewGroup.LayoutParams.MATCH_PARENT,
            ViewGroup.LayoutParams.MATCH_PARENT
        )
        setContentView(webView)

        TunnelVpnService.onDisconnect = { runOnUiThread { relay.stop() } }

        relay = HeadlessRelayController(
            applicationInfo.nativeLibraryDir,
            onLog = { msg ->
                if (msg.contains("ERROR:")) {
                    runOnUiThread {
                        webView.evaluateJavascript(
                            "document.getElementById('status').textContent='${msg.replace("'", "\\'")}'", null
                        )
                    }
                }
            },
            onStatus = { status ->
                Log.d("HEADLESS", "status: $status")
                TunnelVpnService.instance?.updateStatus(status)
                if (status == VpnStatus.TUNNEL_ACTIVE) {
                    runOnUiThread { requestVpn() }
                }
            },
        )
        relay.start()

        captchaView = VkCaptchaWebView(this, webView) { joinJson ->
            Log.d("HEADLESS", "Auth complete, sending join params to relay")
            val params = org.json.JSONObject(joinJson)
            params.put("tunnelMode", TunnelMode.DC.relayArg)
            relay.sendJoinParams(params.toString())
        }
        captchaView.setup()
        captchaView.start(
            CALL_LINK,
            "Test"
        )
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
        Log.d("HEADLESS", "VPN started")
    }

    companion object {
        private const val CALL_LINK = "" // Open call page on app start (do not delete - I need it for debug)
    }
}
