package bypass.whitelist

import android.annotation.SuppressLint
import android.content.ClipData
import android.content.ClipboardManager
import android.content.Intent
import android.content.SharedPreferences
import android.content.pm.PackageManager
import android.net.VpnService
import android.os.Build
import android.os.Bundle
import android.util.Log
import android.view.View
import android.webkit.*
import android.widget.AdapterView
import android.widget.ArrayAdapter
import android.widget.Button
import android.widget.CompoundButton
import android.widget.EditText
import android.widget.ImageButton
import android.widget.Spinner
import android.widget.TextView
import android.widget.Toast
import androidx.activity.enableEdgeToEdge
import androidx.activity.result.contract.ActivityResultContracts
import androidx.appcompat.app.AppCompatActivity
import androidx.core.view.ViewCompat
import androidx.core.view.WindowInsetsCompat
import mobile.LogCallback
import mobile.Mobile
import java.io.File
import androidx.core.content.edit

private val reUrl = Regex("(https?://[^/]+/)(.+)")
private val reIp4 = Regex("\\d+\\.\\d+\\.\\d+\\.\\d+")
private val reIp6 = Regex("[0-9a-fA-F]{1,4}(?::[0-9a-fA-F]{1,4}){2,7}|(?:[0-9a-fA-F]{1,4}:){1,6}:[0-9a-fA-F]{1,4}|::(?:[0-9a-fA-F]{1,4}:){0,5}[0-9a-fA-F]{1,4}")

private fun maskUrl(url: String): String {
    return url.replace(reUrl) { m ->
        m.groupValues[1] + m.groupValues[2].take(4) + "***"
    }.replace(reIp4) { m ->
        val p = m.value.split(".")
        "${p[0]}.${p[1]}.x.x"
    }.replace(reIp6, "x::x")
}

class MainActivity : AppCompatActivity() {

    private var tunnelMode = TunnelMode.DC
    private var pionProcess: Process? = null

    private lateinit var webView: WebView
    private lateinit var logView: TextView
    private lateinit var urlInput: EditText
    private lateinit var connectOnStartSwitch: CompoundButton

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

    private val hookVk by lazy { assets.open("joiner-vk.js").bufferedReader().readText() }
    private val hookTelemost by lazy { assets.open("joiner-telemost.js").bufferedReader().readText() }
    private val hookPionVk by lazy { assets.open("pion-vk.js").bufferedReader().readText() }
    private val hookPionTelemost by lazy { assets.open("pion-telemost.js").bufferedReader().readText() }

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

        previousUrl = getPreferences(MODE_PRIVATE).getString(SharedPrefsKeys.URL, "")!!
        urlInput.setText(previousUrl)

        tunnelMode = getTunnelModeFromPrefs()

        setupWebView()
        setupModeSpinner()

        val goButton = findViewById<Button>(R.id.goButton)
        goButton.setOnClickListener {
            val url = urlInput.text.toString().trim()
            if (url.isNotEmpty()) {
                stopRelay()
                startRelay()
                appendLog("Loading: ${maskUrl(url)}")
                if (previousUrl != url) {
                    previousUrl = url
                    getPreferences(MODE_PRIVATE).edit {
                            putString(SharedPrefsKeys.URL, url)
                        }
                }
                webView.loadUrl(url)
            }
        }

        findViewById<ImageButton>(R.id.copyLogsButton).setOnClickListener {
            val clip = ClipData.newPlainText("logs", logView.text)
            (getSystemService(CLIPBOARD_SERVICE) as ClipboardManager).setPrimaryClip(clip)
            Toast.makeText(this, "Logs copied", Toast.LENGTH_SHORT).show()
        }

        VpnService.prepare(this)?.let { vpnPrepLauncher.launch(it) }
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU &&
            checkSelfPermission(android.Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED) {
            requestPermissions(arrayOf(android.Manifest.permission.POST_NOTIFICATIONS), 0)
        }

        if (CALL_LINK.isNotEmpty()) {
            urlInput.setText(CALL_LINK)
            goButton.performClick()
        }

        connectOnStartSwitch = findViewById(R.id.connectOnAppStartSwitch)
        connectOnStartSwitch.isChecked = getPreferences(MODE_PRIVATE).getBoolean(
            SharedPrefsKeys.CONNECT_ON_START,
            true
        )
        connectOnStartSwitch.setOnCheckedChangeListener { _, isChecked ->
            getPreferences(MODE_PRIVATE).edit {
                    putBoolean(SharedPrefsKeys.CONNECT_ON_START, isChecked)
                }
        }
        if (connectOnStartSwitch.isChecked && previousUrl.isNotEmpty() && CALL_LINK.isEmpty()) {
            goButton.performClick()
        }
        findViewById<View>(R.id.clearButton).setOnClickListener {
            urlInput.setText("")
        }
    }

    override fun onDestroy() {
        stopRelay()
        TunnelVpnService.instance?.stopSelf()
        super.onDestroy()
    }

    private fun setupModeSpinner() {
        val spinner = findViewById<Spinner>(R.id.modeSpinner)
        val adapter = ArrayAdapter(this, android.R.layout.simple_spinner_item, TunnelMode.entries.map { it.label })
        adapter.setDropDownViewResource(android.R.layout.simple_spinner_dropdown_item)
        spinner.adapter = adapter
        spinner.setSelection(TunnelMode.entries.indexOf(tunnelMode))
        spinner.onItemSelectedListener = object : AdapterView.OnItemSelectedListener {
            var init = true
            override fun onItemSelected(parent: AdapterView<*>?, view: View?, pos: Int, id: Long) {
                if (init) { init = false; return }
                tunnelMode = TunnelMode.entries[pos]
                getPreferences(MODE_PRIVATE).edit {
                        putString(SharedPrefsKeys.TUNNEL_MODE, tunnelMode.name)
                    }
                appendLog("Mode: ${tunnelMode.label}")
                stopRelay()
                startRelay()
            }
            override fun onNothingSelected(parent: AdapterView<*>?) {}
        }
    }

    private fun startRelay() {
        Log.d("RELAY", "startRelay mode=${tunnelMode.label}")
        if (tunnelMode.isPion) startPionRelay() else startDcRelay()
    }

    private fun stopRelay() {
        pionProcess?.let {
            it.destroy()
            it.waitFor()
        }
        pionProcess = null
    }

    private fun startDcRelay() {
        val cb = LogCallback { msg ->
            appendLog(msg)
            if (msg.contains("browser connected")) updateVpnStatus(VpnStatus.TUNNEL_ACTIVE)
            else if (msg.contains("ws read error")) updateVpnStatus(VpnStatus.TUNNEL_LOST)
        }
        Thread {
            try {
                Mobile.startJoiner(9000, 1080, cb)
            } catch (e: Exception) {
                appendLog("Relay error: ${e.message}")
            }
        }.start()
        appendLog("Relay started DC mode (SOCKS5 :1080, WS :9000)")
    }

    private fun startPionRelay() {
        val relayBin = File(applicationInfo.nativeLibraryDir, "librelay.so")
        if (!relayBin.exists()) {
            appendLog("Pion relay binary not found")
            return
        }
        Thread {
            try {
                val isTelemost = urlInput.text.toString().contains("telemost")
                val mode = tunnelMode.relayMode(isTelemost)
                val pb = ProcessBuilder(
                    relayBin.absolutePath, "--mode", mode, "--ws-port", "9001", "--socks-port", "1080"
                )
                pb.redirectErrorStream(true)
                val proc = pb.start()
                pionProcess = proc
                appendLog("Pion relay started mode=$mode (signaling :9001, SOCKS5 :1080)")
                proc.inputStream.bufferedReader().forEachLine { line ->
                    Log.d("RELAY", line)
                    appendLog(line)
                    if (line.contains("CONNECTED")) updateVpnStatus(VpnStatus.TUNNEL_ACTIVE)
                    else if (line.contains("session cleaned up")) updateVpnStatus(VpnStatus.TUNNEL_LOST)
                }
                appendLog("Pion relay exited: ${proc.exitValue()}")
            } catch (e: Exception) {
                Log.e("RELAY", "Pion relay error", e)
                appendLog("Pion relay error: ${e.message}")
            }
        }.start()
    }

    private fun updateVpnStatus(status: VpnStatus) {
        TunnelVpnService.instance?.updateStatus(status)
    }

    private fun requestVpn() {
        val intent = VpnService.prepare(this)
        if (intent != null) vpnLauncher.launch(intent) else startVpnService()
    }

    private fun startVpnService() {
        startService(Intent(this, TunnelVpnService::class.java))
        appendLog("VPN started")
        updateVpnStatus(VpnStatus.TUNNEL_ACTIVE)
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
                        text.contains("CALL CONNECTED") -> updateVpnStatus(VpnStatus.CALL_CONNECTED)
                        text.contains("DataChannel open") -> updateVpnStatus(VpnStatus.DATACHANNEL_OPEN)
                        text.contains("DataChannel closed") -> updateVpnStatus(VpnStatus.DATACHANNEL_LOST)
                        text.contains("WebSocket connected") -> updateVpnStatus(VpnStatus.TUNNEL_ACTIVE)
                        text.contains("WebSocket disconnected") -> updateVpnStatus(VpnStatus.TUNNEL_LOST)
                        text.contains("Connection state: connecting") -> updateVpnStatus(VpnStatus.CONNECTING)
                        text.contains("Connection state: disconnected") -> updateVpnStatus(VpnStatus.CALL_DISCONNECTED)
                        text.contains("Connection state: failed") -> updateVpnStatus(VpnStatus.CALL_FAILED)
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
                appendLog("Page loaded, injecting hook for ${maskUrl(url)}")
                view.evaluateJavascript(hookForUrl(url), null)
            }
        }
    }

    private fun appendLog(msg: String) {
        runOnUiThread {
            logView.append("${msg.replace("[HOOK] ", "")}\n")
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
                if (!ip.isLoopbackAddress && ip is java.net.Inet4Address) {
                    return ip.hostAddress ?: ""
                }
            }
        } catch (e: Exception) {
            Log.e("RELAY", "getLocalIPAddress error", e)
        }
        return ""
    }

    private fun getTunnelModeFromPrefs(): TunnelMode {
        val preferences = getPreferences(MODE_PRIVATE)
        val tunnelModeString = getPreferences(MODE_PRIVATE)
            .getString(SharedPrefsKeys.TUNNEL_MODE, TunnelMode.DC.name)!!
        return try {
            TunnelMode.valueOf(tunnelModeString)
        } catch (_: IllegalArgumentException) {
            Log.e("SharedPref", "Unknown tunnel mode: $tunnelModeString. Resetting to ${TunnelMode.DC.name}")
            preferences.edit {
                    putString(SharedPrefsKeys.TUNNEL_MODE, TunnelMode.DC.name)
                }
            TunnelMode.DC
        }
    }

    @Suppress("unused")
    inner class JsBridge {
        @JavascriptInterface
        fun log(msg: String) = appendLog(msg)

        @JavascriptInterface
        fun getLocalIP(): String = getLocalIPAddress()

        @JavascriptInterface
        fun resolveHost(hostname: String): String = try {
            java.net.InetAddress.getByName(hostname).hostAddress ?: ""
        } catch (_: Exception) { "" }

        @JavascriptInterface
        fun onTunnelReady() {
            appendLog("Tunnel ready, starting VPN...")
            updateVpnStatus(VpnStatus.TUNNEL_ACTIVE)
            runOnUiThread { requestVpn() }
        }
    }

    private enum class TunnelMode(val label: String, val relayArg: String, val isPion: Boolean) {
        DC("DC", "joiner", false),
        PION_VIDEO("Video", "video", true);

        fun relayMode(isTelemost: Boolean): String {
            if (!isPion) return "joiner"
            val platform = if (isTelemost) "telemost" else "vk"
            return "$platform-$relayArg-joiner"
        }
    }

    companion object {
        private const val CALL_LINK = "" // Open call page on app start (do not delete - I need it for debug)
    }
}
