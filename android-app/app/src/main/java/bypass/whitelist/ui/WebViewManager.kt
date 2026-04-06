package bypass.whitelist.ui

import android.annotation.SuppressLint
import android.graphics.Bitmap
import android.util.Log
import android.view.View
import android.webkit.ConsoleMessage
import android.webkit.PermissionRequest
import android.webkit.WebChromeClient
import android.webkit.WebResourceRequest
import android.webkit.WebResourceResponse
import android.webkit.WebView
import android.webkit.WebViewClient
import android.widget.ImageView
import android.widget.TextView
import androidx.appcompat.app.AppCompatActivity
import bypass.whitelist.R
import bypass.whitelist.tunnel.CallPlatform
import bypass.whitelist.tunnel.TunnelMode
import bypass.whitelist.tunnel.VpnStatus
import bypass.whitelist.util.ParamCallback
import bypass.whitelist.util.maskUrl

private data class HookKey(val isPion: Boolean, val platform: CallPlatform)

class WebViewManager(
    private val activity: AppCompatActivity,
    private val webView: WebView,
    private val toggleButton: View,
    private val toggleArrow: ImageView,
    private val toggleLabel: TextView,
    private val onLog: ParamCallback<String>,
    private val onStatus: ParamCallback<VpnStatus>,
) {
    var tunnelMode: TunnelMode = TunnelMode.DC
    private var expanded = false
    private var callUrl = ""

    private val hooks = mapOf(
        HookKey(false, CallPlatform.VK) to lazy { loadAsset("dc-joiner-vk.js") },
        HookKey(false, CallPlatform.TELEMOST) to lazy { loadAsset("dc-joiner-telemost.js") },
        HookKey(true, CallPlatform.VK) to lazy { loadAsset("video-vk.js") },
        HookKey(true, CallPlatform.TELEMOST) to lazy { loadAsset("video-telemost.js") },
    )

    private val autoclickers = mapOf(
        CallPlatform.VK to lazy { loadAsset("autoclick-vk.js") },
        CallPlatform.TELEMOST to lazy { loadAsset("autoclick-telemost.js") },
    )

    private val muteAudioContext by lazy { loadAsset("mute-audio-context.js") }

    private fun loadAsset(name: String): String =
        activity.assets.open(name).bufferedReader().readText()

    private fun setExpanded(value: Boolean) {
        expanded = value
        webView.visibility = if (value) View.VISIBLE else View.GONE
        toggleArrow.rotation = if (value) 180f else 0f
        toggleLabel.setText(if (value) R.string.collapse_webview else R.string.expand_webview)
    }

    fun collapse() {
        setExpanded(false)
    }

    fun expand() {
        setExpanded(true)
    }

    @SuppressLint("SetJavaScriptEnabled")
    fun setup(jsBridge: Any) {
        toggleButton.setOnClickListener { setExpanded(!expanded) }
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

        webView.addJavascriptInterface(jsBridge, "AndroidBridge")

        webView.webChromeClient = object : WebChromeClient() {
            override fun onPermissionRequest(request: PermissionRequest) {
                activity.runOnUiThread { request.grant(request.resources) }
            }

            override fun onConsoleMessage(msg: ConsoleMessage): Boolean {
                val text = msg.message()
                Log.d("HOOK", text)
                if (text.contains("[HOOK]")) {
                    onLog(text)
                    when {
                        text.contains("CALL CONNECTED") -> onStatus(VpnStatus.CALL_CONNECTED)
                        text.contains("DataChannel open") -> onStatus(VpnStatus.DATACHANNEL_OPEN)
                        text.contains("DataChannel closed") -> onStatus(VpnStatus.DATACHANNEL_LOST)
                        text.contains("WebSocket connected") -> onStatus(VpnStatus.TUNNEL_ACTIVE)
                        text.contains("WebSocket disconnected") -> onStatus(VpnStatus.TUNNEL_LOST)
                        text.contains("Connection state: connecting") -> onStatus(VpnStatus.CONNECTING)
                        text.contains("Connection state: disconnected") -> onStatus(VpnStatus.CALL_DISCONNECTED)
                        text.contains("Connection state: failed") -> onStatus(VpnStatus.CALL_FAILED)
                    }
                }
                return true
            }
        }

        webView.webViewClient = object : WebViewClient() {
            override fun shouldInterceptRequest(view: WebView, request: WebResourceRequest): WebResourceResponse? {
                val url = request.url.toString()
                val platform = CallPlatform.fromUrl(url)
                if (platform != CallPlatform.TELEMOST || !url.contains("/j/") || request.method != "GET") return null
                return stripCsp(url, request)
            }

            override fun onPageStarted(view: WebView, url: String, favicon: Bitmap?) {
                if (url.contains("about:blank")) return
                view.evaluateJavascript(muteAudioContext, null)
            }

            override fun onPageFinished(view: WebView, url: String) {
                if (url.contains("about:blank")) return
                if (!expanded && url != callUrl) activity.runOnUiThread { setExpanded(true) }
                view.evaluateJavascript("!!window.__hookInstalled") { result ->
                    if (result == "true") {
                        Log.d("HOOK", "Hook already injected, skipping")
                        return@evaluateJavascript
                    }
                    val platform = CallPlatform.fromUrl(url)
                    onLog("Page loaded, injecting hook for ${maskUrl(url)}")
                    view.evaluateJavascript("window.WS_PORT=${mobile.Mobile.activeWsPort()}", null)
                    view.evaluateJavascript(hookForPlatform(platform), null)
                    onLog("Injecting autoclick for ${maskUrl(url)}")
                    view.evaluateJavascript(autoclickers[platform]!!.value, null)
                }
            }
        }
    }

    fun loadUrl(url: String) {
        callUrl = url
        webView.loadUrl(url)
    }

    fun loadBlank() {
        callUrl = ""
        webView.loadUrl("about:blank")
    }

    private fun hookForPlatform(platform: CallPlatform): String =
        hooks[HookKey(tunnelMode.isPion, platform)]!!.value

    private fun stripCsp(url: String, request: WebResourceRequest): WebResourceResponse? {
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
}
