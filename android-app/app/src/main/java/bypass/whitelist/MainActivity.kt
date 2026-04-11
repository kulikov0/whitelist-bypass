package bypass.whitelist

import android.content.Intent
import android.content.pm.PackageManager
import android.net.ConnectivityManager
import android.net.VpnService
import android.os.Build
import android.os.Bundle
import android.util.Log
import android.view.View
import android.webkit.JavascriptInterface
import android.widget.Button
import android.widget.EditText
import android.widget.ImageButton
import androidx.activity.enableEdgeToEdge
import androidx.activity.result.contract.ActivityResultContracts
import androidx.appcompat.app.AppCompatActivity
import androidx.core.view.ViewCompat
import androidx.core.view.WindowInsetsCompat
import bypass.whitelist.tunnel.CallPlatform
import bypass.whitelist.tunnel.RelayController
import bypass.whitelist.tunnel.TunnelMode
import bypass.whitelist.tunnel.TunnelVpnService
import bypass.whitelist.tunnel.VpnStatus
import bypass.whitelist.ui.LogViewController
import bypass.whitelist.ui.SettingsDialogFragment
import bypass.whitelist.ui.StatusBarController
import bypass.whitelist.ui.WebViewManager
import bypass.whitelist.util.LogWriter
import bypass.whitelist.util.Prefs
import bypass.whitelist.util.hideKeyboard
import bypass.whitelist.util.maskUrl
import java.net.Inet4Address
import java.net.InetAddress

class MainActivity : AppCompatActivity(), SettingsDialogFragment.Listener {

    private val logWriter by lazy { LogWriter(cacheDir) }
    private val relay by lazy {
        RelayController(
            nativeLibDir = applicationInfo.nativeLibraryDir,
            onLog = { logCtrl.append(it) },
            onStatus = ::onVpnStatus,
        )
    }

    private lateinit var urlInput: EditText
    private lateinit var logCtrl: LogViewController
    private lateinit var statusCtrl: StatusBarController
    private lateinit var webViewMgr: WebViewManager
    private lateinit var logContainer: View

    private var previousUrl = ""

    private val vpnPrepLauncher = registerForActivityResult(
        ActivityResultContracts.StartActivityForResult()
    ) {}

    private val vpnLauncher = registerForActivityResult(
        ActivityResultContracts.StartActivityForResult()
    ) { result ->
        if (result.resultCode == RESULT_OK) startVpnService()
        else logCtrl.append("VPN permission denied")
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        setContentView(R.layout.activity_main)
        ViewCompat.setOnApplyWindowInsetsListener(findViewById(R.id.main)) { v, insets ->
            val systemBars = insets.getInsets(WindowInsetsCompat.Type.systemBars())
            v.setPadding(systemBars.left, systemBars.top, systemBars.right, systemBars.bottom)
            insets
        }

        urlInput = findViewById(R.id.urlInput)
        logContainer = findViewById(R.id.logContainer)

        logCtrl = LogViewController(this, findViewById(R.id.logView), logWriter)
        logCtrl.reset()

        statusCtrl = StatusBarController(
            activity = this,
            statusBar = findViewById(R.id.statusBar),
            goButton = findViewById(R.id.goButton),
            onConnect = ::onGoPressed,
            onDisconnect = ::fullReset,
        )

        webViewMgr = WebViewManager(
            activity = this,
            webView = findViewById(R.id.webView),
            toggleButton = findViewById(R.id.toggleWebViewButton),
            toggleArrow = findViewById(R.id.toggleWebViewArrow),
            toggleLabel = findViewById(R.id.toggleWebViewLabel),
            onLog = { logCtrl.append(it) },
            onStatus = ::onVpnStatus,
        )
        webViewMgr.setup(JsBridge())

        previousUrl = Prefs.lastUrl
        urlInput.setText(previousUrl)
        statusCtrl.tunnelMode = Prefs.tunnelMode
        webViewMgr.tunnelMode = Prefs.tunnelMode
        statusCtrl.setIdle()
        logContainer.visibility = if (Prefs.showLogs) View.VISIBLE else View.GONE

        findViewById<Button>(R.id.goButton).setOnClickListener { onGoPressed() }
        findViewById<ImageButton>(R.id.shareLogsButton).setOnClickListener { logCtrl.shareLogs() }
        findViewById<ImageButton>(R.id.gearButton).setOnClickListener {
            SettingsDialogFragment().show(supportFragmentManager, SettingsDialogFragment.TAG)
        }
        findViewById<View>(R.id.clearButton).setOnClickListener { urlInput.setText("") }

        TunnelVpnService.onDisconnect = { runOnUiThread { resetState() } }

        VpnService.prepare(this)?.let { vpnPrepLauncher.launch(it) }
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU &&
            checkSelfPermission(android.Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED
        ) {
            requestPermissions(arrayOf(android.Manifest.permission.POST_NOTIFICATIONS), 0)
        }

        if (CALL_LINK.isNotEmpty()) {
            urlInput.setText(CALL_LINK)
            onGoPressed()
        } else if (Prefs.connectOnStart && previousUrl.isNotEmpty()) {
            onGoPressed()
        }
    }

    override fun onDestroy() {
        TunnelVpnService.onDisconnect = null
        relay.stop()
        TunnelVpnService.instance?.stop()
        logCtrl.close()
        super.onDestroy()
    }

    override fun onTunnelModeChanged(mode: TunnelMode) {
        statusCtrl.tunnelMode = mode
        webViewMgr.tunnelMode = mode
        fullReset()
    }

    override fun onShowLogsChanged(visible: Boolean) {
        logContainer.visibility = if (visible) View.VISIBLE else View.GONE
    }

    override fun onShareLogs() {
        logCtrl.shareLogs()
    }

    override fun onReset() {
        fullReset()
    }

    private fun onGoPressed() {
        val url = urlInput.text.toString().trim()
        if (url.isEmpty()) return
        logCtrl.reset()
        relay.stop()
        relay.start(Prefs.tunnelMode, CallPlatform.fromUrl(url))
        hideKeyboard()
        urlInput.clearFocus()
        statusCtrl.setConnected(false)
        statusCtrl.setStatus(VpnStatus.CONNECTING)
        logCtrl.append("Loading: ${maskUrl(url)}")
        if (previousUrl != url) {
            previousUrl = url
            Prefs.lastUrl = url
        }
        webViewMgr.loadUrl(url)
    }

    private fun onVpnStatus(status: VpnStatus) {
        if (!relay.isRunning) return
        TunnelVpnService.instance?.updateStatus(status)
        runOnUiThread {
            statusCtrl.setStatus(status)
            if (status == VpnStatus.TUNNEL_ACTIVE) statusCtrl.setConnected(true)
        }
    }

    private fun requestVpn() {
        val intent = VpnService.prepare(this)
        if (intent != null) vpnLauncher.launch(intent) else startVpnService()
    }

    private fun startVpnService() {
        startService(Intent(this, TunnelVpnService::class.java))
        logCtrl.append("VPN started")
        onVpnStatus(VpnStatus.TUNNEL_ACTIVE)
    }

    private fun resetState() {
        relay.stop()
        webViewMgr.loadBlank()
        webViewMgr.collapse()
        logCtrl.reset()
        statusCtrl.setConnected(false)
        statusCtrl.setIdle()
    }

    private fun fullReset() {
        resetState()
        TunnelVpnService.instance?.stop()
    }

    private fun getLocalIPAddress(): String {
        try {
            val cm = getSystemService(CONNECTIVITY_SERVICE) as ConnectivityManager
            val network = cm.activeNetwork ?: return ""
            val props = cm.getLinkProperties(network) ?: return ""
            for (addr in props.linkAddresses) {
                val ip = addr.address
                if (!ip.isLoopbackAddress && ip is Inet4Address) {
                    return ip.hostAddress ?: ""
                }
            }
        } catch (e: Exception) {
            Log.e("RELAY", "getLocalIPAddress error", e)
        }
        return ""
    }

    @Suppress("unused")
    inner class JsBridge {
        @JavascriptInterface
        fun log(msg: String) = logCtrl.append(msg)

        @JavascriptInterface
        fun getLocalIP(): String = getLocalIPAddress()

        @JavascriptInterface
        fun resolveHost(hostname: String): String = try {
            val all = InetAddress.getAllByName(hostname)
            val v4 = all.firstOrNull { it is Inet4Address }
            val addr = v4 ?: all.first()
            val ip = addr.hostAddress ?: ""
            Log.d("RELAY", "resolveHost: $hostname -> $ip (${addr.javaClass.simpleName}, ${all.size} addrs)")
            ip
        } catch (e: Exception) {
            Log.d("RELAY", "resolveHost: $hostname -> FAILED: ${e.message}")
            ""
        }

        @JavascriptInterface
        fun onTunnelReady() {
            logCtrl.append("Tunnel ready, starting VPN...")
            onVpnStatus(VpnStatus.TUNNEL_ACTIVE)
            runOnUiThread { requestVpn() }
        }

        @JavascriptInterface
        fun onCaptchaDetected(isDone: Boolean) {
            if(!isDone) {
                onVpnStatus(VpnStatus.ACTION_REQUIRED_CAPTCHA)
                runOnUiThread { webViewMgr.expand() }
            } else {
                onVpnStatus(VpnStatus.CONNECTING)
            }
        }
    }

    companion object {
        private const val CALL_LINK = "" // Open call page on app start (do not delete - I need it for debug)
    }
}
