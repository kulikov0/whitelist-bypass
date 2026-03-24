package bypass.whitelist

import android.annotation.SuppressLint
import android.app.AlertDialog
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.graphics.Color
import android.net.VpnService
import android.os.Build
import android.os.Bundle
import android.util.Log
import android.view.View
import android.webkit.*
import android.widget.Button
import android.widget.EditText
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

class MainActivity : AppCompatActivity() {

    private lateinit var webView: WebView
    private lateinit var logView: TextView
    private lateinit var statusText: TextView
    private lateinit var statusDot: View
    private lateinit var connectButton: Button

    private var isConnected = false
    private var isConnecting = false
    private var autoJoinDone = false

    private var tunnelMode = TunnelMode.DC
    private var pionProcess: Process? = null

    private val vpnPrepLauncher = registerForActivityResult(
        ActivityResultContracts.StartActivityForResult()
    ) {}

    private val vpnLauncher = registerForActivityResult(
        ActivityResultContracts.StartActivityForResult()
    ) { result ->
        if (result.resultCode == RESULT_OK) startVpnService()
        else {
            appendLog("VPN permission denied")
            setStatus(VpnStatus.CALL_DISCONNECTED)
        }
    }

    private val hookVk by lazy { assets.open("joiner-vk.js").bufferedReader().readText() }
    private val hookTelemost by lazy { assets.open("joiner-telemost.js").bufferedReader().readText() }
    private val hookPionVk by lazy { assets.open("pion-vk.js").bufferedReader().readText() }
    private val hookPionTelemost by lazy { assets.open("pion-telemost.js").bufferedReader().readText() }

    private fun hookForUrl(url: String): String {
        return if (tunnelMode.isPion) {
            if (url.contains("telemost.yandex")) hookPionTelemost else hookPionVk
        } else {
            if (url.contains("telemost.yandex")) hookTelemost else hookVk
        }
    }

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

        statusDot = findViewById(R.id.statusDot)
        statusText = findViewById(R.id.statusText)
        connectButton = findViewById(R.id.connectButton)
        logView = findViewById(R.id.logView)
        webView = findViewById(R.id.webView)

        setupWebView()
        startRelay()

        // Pre-request VPN permission so it's ready when needed
        VpnService.prepare(this)?.let { vpnPrepLauncher.launch(it) }
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU &&
            checkSelfPermission(android.Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED) {
            requestPermissions(arrayOf(android.Manifest.permission.POST_NOTIFICATIONS), 0)
        }

        connectButton.setOnClickListener {
            if (isConnected || isConnecting) disconnect() else connect()
        }

        statusDot.setOnLongClickListener {
            logView.visibility = if (logView.visibility == View.GONE) View.VISIBLE else View.GONE
            true
        }

        statusText.setOnLongClickListener {
            if (!isConnected && !isConnecting) showServerUrlDialog()
            true
        }

        // Long press connect button = cycle tunnel mode
        connectButton.setOnLongClickListener {
            if (!isConnected && !isConnecting) {
                tunnelMode = if (tunnelMode == TunnelMode.DC) TunnelMode.PION_VIDEO else TunnelMode.DC
                stopRelay()
                startRelay()
                appendLog("Mode switched to: ${tunnelMode.label}")
                logView.visibility = View.VISIBLE
                Toast.makeText(this, "Mode: ${tunnelMode.label}", Toast.LENGTH_SHORT).show()
            }
            true
        }
    }

    override fun onDestroy() {
        stopRelay()
        TunnelVpnService.instance?.stopSelf()
        super.onDestroy()
    }

    private fun connect() {
        if (isConnecting) return
        isConnecting = true
        autoJoinDone = false
        webView.clearCache(true)
        android.webkit.CookieManager.getInstance().removeAllCookies(null)
        setStatus(VpnStatus.CONNECTING)
        appendLog("Fetching call link...")
        Thread {
            var retries = 0
            while (retries < 5) {
                try {
                    val conn = java.net.URL(linkServerUrl()).openConnection() as java.net.HttpURLConnection
                    conn.connectTimeout = 5000
                    conn.readTimeout = 5000
                    val body = conn.inputStream.bufferedReader().readText()
                    val match = Regex("\"link\"\\s*:\\s*\"([^\"]+)\"").find(body)
                    val link = match?.groupValues?.get(1)
                    if (!link.isNullOrEmpty()) {
                        appendLog("Got link, loading call...")
                        runOnUiThread { webView.loadUrl(link) }
                        return@Thread
                    }
                } catch (e: Exception) {
                    appendLog("Server error: ${e.message}")
                }
                retries++
                Thread.sleep(2000)
            }
            runOnUiThread {
                isConnecting = false
                setStatus(VpnStatus.CALL_DISCONNECTED)
                Toast.makeText(this, "Cannot reach server", Toast.LENGTH_SHORT).show()
            }
        }.start()
    }

    private fun disconnect() {
        isConnected = false
        isConnecting = false
        autoJoinDone = false
        webView.loadUrl("about:blank")
        webView.clearCache(true)
        android.webkit.CookieManager.getInstance().removeAllCookies(null)
        startService(Intent(this, TunnelVpnService::class.java).apply {
            action = TunnelVpnService.ACTION_STOP
        })
        setStatus(VpnStatus.CALL_DISCONNECTED)
        appendLog("Disconnected")
    }

    private fun setStatus(status: VpnStatus) {
        runOnUiThread {
            val (dotColor, textColor, text, btnText) = when (status) {
                VpnStatus.STARTING, VpnStatus.CONNECTING, VpnStatus.CALL_CONNECTED,
                VpnStatus.DATACHANNEL_OPEN -> arrayOf(
                    Color.parseColor("#FFA500"), Color.parseColor("#FFA500"),
                    "Подключение...", "Отключить"
                )
                VpnStatus.TUNNEL_ACTIVE -> {
                    isConnected = true
                    isConnecting = false
                    arrayOf(Color.parseColor("#00CC66"), Color.parseColor("#00CC66"),
                        "Подключено", "Отключить")
                }
                VpnStatus.TUNNEL_LOST, VpnStatus.DATACHANNEL_LOST -> arrayOf(
                    Color.parseColor("#FFA500"), Color.parseColor("#FFA500"),
                    "Переподключение...", "Отключить"
                )
                VpnStatus.CALL_DISCONNECTED, VpnStatus.CALL_FAILED -> {
                    isConnected = false
                    isConnecting = false
                    arrayOf(Color.parseColor("#333333"), Color.parseColor("#888888"),
                        "Отключено", "Подключить")
                }
            }
            statusDot.background.setTint(dotColor as Int)
            statusText.setTextColor(textColor as Int)
            statusText.text = text as String
            connectButton.text = btnText as String
        }
    }

    private fun startRelay() {
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
            if (msg.contains("browser connected")) setStatus(VpnStatus.TUNNEL_ACTIVE)
            else if (msg.contains("ws read error")) setStatus(VpnStatus.TUNNEL_LOST)
        }
        Thread {
            try {
                Mobile.startJoiner(9000, 1080, cb)
            } catch (e: Exception) {
                appendLog("Relay error: ${e.message}")
            }
        }.start()
        appendLog("Relay started: DC mode")
    }

    private fun startPionRelay() {
        val relayBin = File(applicationInfo.nativeLibraryDir, "librelay.so")
        if (!relayBin.exists()) {
            appendLog("Pion relay binary not found")
            return
        }
        Thread {
            try {
                val pb = ProcessBuilder(
                    relayBin.absolutePath, "--mode", "vk-video-joiner",
                    "--ws-port", "9001", "--socks-port", "1080"
                )
                pb.redirectErrorStream(true)
                val proc = pb.start()
                pionProcess = proc
                appendLog("Relay started: Pion Video mode")
                proc.inputStream.bufferedReader().forEachLine { line ->
                    Log.d("RELAY", line)
                    appendLog(line)
                    if (line.contains("CONNECTED")) setStatus(VpnStatus.TUNNEL_ACTIVE)
                    else if (line.contains("session cleaned up")) setStatus(VpnStatus.TUNNEL_LOST)
                }
                appendLog("Pion relay exited: ${proc.exitValue()}")
            } catch (e: Exception) {
                appendLog("Pion relay error: ${e.message}")
            }
        }.start()
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
                        text.contains("CALL CONNECTED") -> setStatus(VpnStatus.CALL_CONNECTED)
                        text.contains("DataChannel open") -> setStatus(VpnStatus.DATACHANNEL_OPEN)
                        text.contains("DataChannel closed") -> setStatus(VpnStatus.DATACHANNEL_LOST)
                        text.contains("WebSocket connected") -> setStatus(VpnStatus.TUNNEL_ACTIVE)
                        text.contains("WebSocket disconnected") -> setStatus(VpnStatus.TUNNEL_LOST)
                        text.contains("Connection state: connecting") -> setStatus(VpnStatus.CONNECTING)
                        text.contains("Connection state: disconnected") -> setStatus(VpnStatus.CALL_DISCONNECTED)
                        text.contains("Connection state: failed") -> setStatus(VpnStatus.CALL_FAILED)
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
                        ) headers[k] = v.joinToString(", ")
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
                appendLog("Page: $url")
                view.evaluateJavascript(hookForUrl(url), null)

                // Auto-join: only once per connect attempt
                if (url.contains("/call/join/") && !autoJoinDone) {
                    autoJoinDone = true
                    view.evaluateJavascript("""
                        (function autoJoin() {
                          var attempts = 0;
                          var clickedTexts = {};
                          var JOIN_BTNS = ['Join', 'Войти', 'Присоединиться', 'Войти в звонок'];
                          var t = setInterval(function() {
                            attempts++;
                            var nameInput = document.querySelector('input[placeholder*="name" i], input[placeholder*="имя" i]');
                            var btns = Array.from(document.querySelectorAll('button'));
                            var joinBtn = btns.find(function(b) {
                              return JOIN_BTNS.indexOf(b.textContent.trim()) !== -1;
                            });
                            if (joinBtn) {
                              var txt = joinBtn.textContent.trim();
                              if (!clickedTexts[txt]) {
                                clickedTexts[txt] = true;
                                if (nameInput && !nameInput.value) {
                                  var setter = Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value').set;
                                  setter.call(nameInput, 'User');
                                  nameInput.dispatchEvent(new Event('input', { bubbles: true }));
                                  nameInput.dispatchEvent(new Event('change', { bubbles: true }));
                                }
                                console.log('[HOOK] Auto-joining: ' + txt);
                                joinBtn.click();
                              }
                            }
                            if (attempts > 60) clearInterval(t);
                          }, 500);
                        })();
                    """.trimIndent(), null)
                }
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

    private fun requestVpn() {
        val intent = VpnService.prepare(this)
        if (intent != null) vpnLauncher.launch(intent) else startVpnService()
    }

    private fun startVpnService() {
        startService(Intent(this, TunnelVpnService::class.java))
        appendLog("VPN started")
        setStatus(VpnStatus.TUNNEL_ACTIVE)
    }

    private fun linkServerUrl(): String =
        getSharedPreferences("settings", Context.MODE_PRIVATE)
            .getString("link_server_url", DEFAULT_LINK_SERVER_URL) ?: DEFAULT_LINK_SERVER_URL

    private fun showServerUrlDialog() {
        val prefs = getSharedPreferences("settings", Context.MODE_PRIVATE)
        val current = prefs.getString("link_server_url", DEFAULT_LINK_SERVER_URL) ?: DEFAULT_LINK_SERVER_URL
        val input = EditText(this).apply { setText(current) }
        AlertDialog.Builder(this)
            .setTitle("Link server URL")
            .setView(input)
            .setPositiveButton("OK") { _, _ ->
                val url = input.text.toString().trim()
                if (url.isNotEmpty()) prefs.edit().putString("link_server_url", url).apply()
            }
            .setNegativeButton("Cancel", null)
            .show()
    }

    private fun getLocalIPAddress(): String {
        try {
            val cm = getSystemService(CONNECTIVITY_SERVICE) as android.net.ConnectivityManager
            val network = cm.activeNetwork ?: return ""
            val props = cm.getLinkProperties(network) ?: return ""
            for (addr in props.linkAddresses) {
                val ip = addr.address
                if (!ip.isLoopbackAddress && ip is java.net.Inet4Address) return ip.hostAddress ?: ""
            }
        } catch (e: Exception) {
            Log.e("RELAY", "getLocalIPAddress error", e)
        }
        return ""
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
            setStatus(VpnStatus.TUNNEL_ACTIVE)
            runOnUiThread { requestVpn() }
        }
    }

    private enum class TunnelMode(val label: String, val isPion: Boolean) {
        DC("DataChannel", false),
        PION_VIDEO("Pion Video", true)
    }

    companion object {
        private const val DEFAULT_LINK_SERVER_URL = "http://YOUR_SERVER_IP:8080/link"
    }
}
