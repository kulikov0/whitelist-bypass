package bypass.whitelist

import android.annotation.SuppressLint
import android.content.Intent
import android.content.pm.PackageManager
import android.net.VpnService
import android.os.Build
import android.os.Bundle
import android.util.Log
import android.view.View
import android.webkit.*
import android.widget.Button
import android.widget.EditText
import android.widget.ImageButton
import android.widget.PopupMenu
import android.widget.TextView
import androidx.activity.enableEdgeToEdge
import androidx.activity.result.contract.ActivityResultContracts
import androidx.appcompat.app.AppCompatActivity
import androidx.core.content.FileProvider
import androidx.core.view.ViewCompat
import androidx.core.view.WindowInsetsCompat
import java.net.Inet4Address
import java.net.InetAddress

class MainActivity : AppCompatActivity() {

    private var tunnelMode = TunnelMode.DC
    private var connected = false
    private var showLogs = false

    private val logWriter by lazy { LogWriter(cacheDir) }
    private val relay by lazy {
        RelayController(
            nativeLibDir = applicationInfo.nativeLibraryDir,
            onLog = ::appendLog,
            onStatus = ::onVpnStatus,
        )
    }

    private lateinit var webView: WebView
    private lateinit var logView: TextView
    private lateinit var urlInput: EditText
    private lateinit var goButton: Button
    private lateinit var statusBar: TextView
    private lateinit var logContainer: View

    private var previousUrl = ""

    private val vpnPrepLauncher = registerForActivityResult(
        ActivityResultContracts.StartActivityForResult()
    ) {}

    private val vpnLauncher = registerForActivityResult(
        ActivityResultContracts.StartActivityForResult()
    ) { result ->
        if (result.resultCode == RESULT_OK) startVpnService()
        else appendLog("VPN permission denied")
    }

    private val hookVk by lazy { assets.open("dc-joiner-vk.js").bufferedReader().readText() }
    private val hookTelemost by lazy { assets.open("dc-joiner-telemost.js").bufferedReader().readText() }
    private val hookPionVk by lazy { assets.open("video-vk.js").bufferedReader().readText() }
    private val hookPionTelemost by lazy { assets.open("video-telemost.js").bufferedReader().readText() }

    @SuppressLint("SetJavaScriptEnabled")
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        setContentView(R.layout.activity_main)
        ViewCompat.setOnApplyWindowInsetsListener(findViewById(R.id.main)) { v, insets ->
            val systemBars = insets.getInsets(WindowInsetsCompat.Type.systemBars())
            v.setPadding(systemBars.left, systemBars.top, systemBars.right, systemBars.bottom)
            insets
        }

        logView = findViewById(R.id.logView)
        urlInput = findViewById(R.id.urlInput)
        webView = findViewById(R.id.webView)
        goButton = findViewById(R.id.goButton)
        statusBar = findViewById(R.id.statusBar)
        logContainer = findViewById(R.id.logContainer)

        logWriter.reset()

        previousUrl = Prefs.lastUrl
        urlInput.setText(previousUrl)
        tunnelMode = Prefs.tunnelMode
        statusBar.text = getString(R.string.status_format, tunnelMode.label, getString(R.string.status_idle))
        showLogs = Prefs.showLogs
        logContainer.visibility = if (showLogs) View.VISIBLE else View.GONE

        setupWebView()

        goButton.setOnClickListener { onGoPressed() }

        findViewById<ImageButton>(R.id.shareLogsButton).setOnClickListener {
            val uri = FileProvider.getUriForFile(this, "$packageName.fileprovider", logWriter.file)
            val share = Intent(Intent.ACTION_SEND).apply {
                type = "text/plain"
                putExtra(Intent.EXTRA_STREAM, uri)
                addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
            }
            startActivity(Intent.createChooser(share, getString(R.string.share_logs)))
        }

        findViewById<ImageButton>(R.id.gearButton).setOnClickListener { showGearMenu(it) }
        findViewById<View>(R.id.clearButton).setOnClickListener { urlInput.setText("") }

        TunnelVpnService.onDisconnect = { runOnUiThread { resetState() } }

        VpnService.prepare(this)?.let { vpnPrepLauncher.launch(it) }
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU &&
            checkSelfPermission(android.Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED) {
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
        stopRelay()
        TunnelVpnService.instance?.stop()
        logWriter.close()
        super.onDestroy()
    }

    private fun onGoPressed() {
        val url = urlInput.text.toString().trim()
        if (url.isEmpty()) return
        logWriter.reset()
        logView.text = ""
        stopRelay()
        startRelay()
        hideKeyboard()
        urlInput.clearFocus()
        setConnected(false)
        setStatus(VpnStatus.CONNECTING)
        appendLog("Loading: ${maskUrl(url)}")
        if (previousUrl != url) {
            previousUrl = url
            Prefs.lastUrl = url
        }
        webView.loadUrl(url)
    }

    private enum class MenuItem(val id: Int, val stringRes: Int) {
        MODE(99, R.string.menu_mode),
        RECONNECT_ON_START(100, R.string.menu_reconnect_on_start),
        SHOW_LOGS(101, R.string.menu_show_logs),
        SHARE_LOGS(102, R.string.menu_share_logs),
        RESET(200, R.string.menu_reset),
    }

    private fun showGearMenu(anchor: View) {
        val popup = PopupMenu(this, anchor)
        val menu = popup.menu

        menu.add(0, MenuItem.MODE.id, 0, getString(MenuItem.MODE.stringRes, tunnelMode.label))

        menu.add(0, MenuItem.RECONNECT_ON_START.id, 0, MenuItem.RECONNECT_ON_START.stringRes).apply {
            isCheckable = true
            isChecked = Prefs.connectOnStart
        }
        menu.add(0, MenuItem.SHOW_LOGS.id, 0, MenuItem.SHOW_LOGS.stringRes).apply {
            isCheckable = true
            isChecked = showLogs
        }
        menu.add(0, MenuItem.SHARE_LOGS.id, 0, MenuItem.SHARE_LOGS.stringRes)
        menu.add(0, MenuItem.RESET.id, 0, MenuItem.RESET.stringRes)

        popup.setOnMenuItemClickListener { item ->
            when (item.itemId) {
                MenuItem.RECONNECT_ON_START.id -> {
                    Prefs.connectOnStart = !item.isChecked
                    true
                }
                MenuItem.SHOW_LOGS.id -> {
                    showLogs = !item.isChecked
                    Prefs.showLogs = showLogs
                    logContainer.visibility = if (showLogs) View.VISIBLE else View.GONE
                    true
                }
                MenuItem.SHARE_LOGS.id -> {
                    findViewById<ImageButton>(R.id.shareLogsButton).performClick()
                    true
                }
                MenuItem.RESET.id -> {
                    resetState()
                    TunnelVpnService.instance?.stop()
                    true
                }
                MenuItem.MODE.id -> {
                    showModeDialog()
                    true
                }
                else -> false
            }
        }
        popup.show()
    }

    private fun showModeDialog() {
        val modes = TunnelMode.entries
        val labels = modes.map { it.label }.toTypedArray()
        val current = modes.indexOf(tunnelMode)
        android.app.AlertDialog.Builder(this)
            .setSingleChoiceItems(labels, current) { dialog, which ->
                dialog.dismiss()
                val mode = modes[which]
                if (mode != tunnelMode) {
                    tunnelMode = mode
                    Prefs.tunnelMode = mode
                    resetState()
                    TunnelVpnService.instance?.stop()
                }
            }
            .show()
    }

    private fun setConnected(value: Boolean) {
        connected = value
        goButton.setText(if (value) R.string.btn_disconnect else R.string.btn_go)
        goButton.setOnClickListener {
            if (connected) {
                resetState()
                TunnelVpnService.instance?.stop()
            } else {
                onGoPressed()
            }
        }
    }

    private fun setStatus(status: VpnStatus) {
        statusBar.text = getString(R.string.status_format, tunnelMode.label, getString(status.labelRes))
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
        statusBar.setBackgroundColor(getColor(colorRes))
    }

    private fun onVpnStatus(status: VpnStatus) {
        if (!relay.isRunning) return
        TunnelVpnService.instance?.updateStatus(status)
        runOnUiThread {
            setStatus(status)
            if (status == VpnStatus.TUNNEL_ACTIVE) setConnected(true)
        }
    }

    private fun startRelay() {
        val isTelemost = urlInput.text.toString().contains("telemost")
        relay.start(tunnelMode, isTelemost)
    }

    private fun stopRelay() {
        relay.stop()
    }

    private fun requestVpn() {
        val intent = VpnService.prepare(this)
        if (intent != null) vpnLauncher.launch(intent) else startVpnService()
    }

    private fun startVpnService() {
        startService(Intent(this, TunnelVpnService::class.java))
        appendLog("VPN started")
        onVpnStatus(VpnStatus.TUNNEL_ACTIVE)
    }

    private fun hookForUrl(url: String): String {
        if (tunnelMode.isPion) {
            return if (url.contains("telemost.yandex")) hookPionTelemost else hookPionVk
        }
        return if (url.contains("telemost.yandex")) hookTelemost else hookVk
    }

    @SuppressLint("SetJavaScriptEnabled")
    private fun setupWebView() {
        webView.settings.apply {
            javaScriptEnabled = true
            domStorageEnabled = true
            mediaPlaybackRequiresUserGesture = false
            allowContentAccess = true
            allowFileAccess = true
            databaseEnabled = true
            setSupportMultipleWindows(false)
            useWideViewPort = true
            loadWithOverviewMode = true
            builtInZoomControls = true
            displayZoomControls = false
            userAgentString = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
        }

        webView.addJavascriptInterface(JsBridge(), "AndroidBridge")

        webView.webChromeClient = object : WebChromeClient() {
            override fun onPermissionRequest(request: PermissionRequest) {
                runOnUiThread { request.grant(request.resources) }
            }

            override fun onConsoleMessage(msg: ConsoleMessage): Boolean {
                val text = msg.message()
                Log.d("HOOK", text)
                if (text.contains("[HOOK]")) {
                    appendLog(text)
                    when {
                        //TODO - use js bridge
                        text.contains("CALL CONNECTED") -> onVpnStatus(VpnStatus.CALL_CONNECTED)
                        text.contains("DataChannel open") -> onVpnStatus(VpnStatus.DATACHANNEL_OPEN)
                        text.contains("DataChannel closed") -> onVpnStatus(VpnStatus.DATACHANNEL_LOST)
                        text.contains("WebSocket connected") -> onVpnStatus(VpnStatus.TUNNEL_ACTIVE)
                        text.contains("WebSocket disconnected") -> onVpnStatus(VpnStatus.TUNNEL_LOST)
                        text.contains("Connection state: connecting") -> onVpnStatus(VpnStatus.CONNECTING)
                        text.contains("Connection state: disconnected") -> onVpnStatus(VpnStatus.CALL_DISCONNECTED)
                        text.contains("Connection state: failed") -> onVpnStatus(VpnStatus.CALL_FAILED)
                    }
                }
                return true
            }
        }

        webView.webViewClient = object : WebViewClient() {
            override fun shouldInterceptRequest(view: WebView, request: WebResourceRequest): WebResourceResponse? {
                val url = request.url.toString()
                if (!url.contains("telemost.yandex.ru/j/") || request.method != "GET") return null
                return try {
                    val conn = java.net.URL(url).openConnection() as java.net.HttpURLConnection
                    conn.requestMethod = "GET"
                    request.requestHeaders?.forEach { (k, v) -> conn.setRequestProperty(k, v) }
                    val headers = mutableMapOf<String, String>()
                    conn.headerFields?.forEach { (k, v) ->
                        if (k != null
                            && !k.equals("content-security-policy", ignoreCase = true)
                            && !k.equals("content-security-policy-report-only", ignoreCase = true)
                        ) {
                            headers[k] = v.joinToString(", ")
                        }
                    }
                    WebResourceResponse(
                        conn.contentType?.split(";")?.firstOrNull() ?: "text/html",
                        "utf-8", conn.responseCode, conn.responseMessage ?: "OK",
                        headers, conn.inputStream
                    )
                } catch (_: Exception) { null }
            }

            override fun onPageStarted(view: WebView, url: String, favicon: android.graphics.Bitmap?) {
                view.evaluateJavascript("""(function(){
var oac=window.AudioContext||window.webkitAudioContext;
if(oac){var nac=function(){var c=new oac();c.suspend();
  c.resume=function(){return Promise.resolve()};
  return c};
  nac.prototype=oac.prototype;window.AudioContext=nac;
  if(window.webkitAudioContext)window.webkitAudioContext=nac}
})()""", null)
            }

            override fun onPageFinished(view: WebView, url: String) {
                view.evaluateJavascript("!!window.__hookInstalled") { result ->
                    if (result == "true") {
                        Log.d("HOOK", "Hook already injected, skipping")
                        return@evaluateJavascript
                    }
                    appendLog("Page loaded, injecting hook for ${maskUrl(url)}")
                    view.evaluateJavascript(hookForUrl(url), null)
                }
            }
        }
    }

    private fun appendLog(msg: String) {
        val (line, evicted) = logWriter.append(msg)
        runOnUiThread {
            if (evicted) {
                logView.text = logWriter.displayText()
            } else {
                logView.append("$line\n")
            }
            val scrollAmount = logView.layout?.let {
                it.getLineTop(logView.lineCount) - logView.height
            } ?: 0
            if (scrollAmount > 0) logView.scrollTo(0, scrollAmount)
        }
    }

    private fun getLocalIPAddress(): String {
        try {
            val cm = getSystemService(CONNECTIVITY_SERVICE) as android.net.ConnectivityManager
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

    private fun resetState() {
        stopRelay()
        webView.loadUrl("about:blank")
        logWriter.reset()
        logView.text = ""
        logView.scrollTo(0, 0)
        setConnected(false)
        statusBar.text = getString(R.string.status_format, tunnelMode.label, getString(R.string.status_idle))
        statusBar.setBackgroundColor(getColor(R.color.status_idle))
    }

    @Suppress("unused")
    inner class JsBridge {
        @JavascriptInterface
        fun log(msg: String) = appendLog(msg)

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
            appendLog("Tunnel ready, starting VPN...")
            onVpnStatus(VpnStatus.TUNNEL_ACTIVE)
            runOnUiThread { requestVpn() }
        }
    }

    companion object {
        private const val CALL_LINK = "" // Open call page on app start (do not delete - I need it for debug)
    }
}
