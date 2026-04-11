package bypass.whitelist.ui

import android.annotation.SuppressLint
import android.graphics.Bitmap
import android.net.ConnectivityManager
import android.os.Bundle
import android.util.Log
import android.view.LayoutInflater
import android.view.View
import android.view.ViewGroup
import android.webkit.ConsoleMessage
import android.webkit.JavascriptInterface
import android.webkit.PermissionRequest
import android.webkit.WebChromeClient
import android.webkit.WebResourceRequest
import android.webkit.WebResourceResponse
import android.webkit.WebView
import android.webkit.WebViewClient
import android.widget.ImageView
import android.widget.TextView
import androidx.fragment.app.Fragment
import bypass.whitelist.R
import bypass.whitelist.tunnel.CallPlatform
import bypass.whitelist.tunnel.RelayController
import bypass.whitelist.tunnel.TunnelMode
import bypass.whitelist.tunnel.VpnStatus
import bypass.whitelist.util.BLANK_URL
import bypass.whitelist.util.DESKTOP_USER_AGENT
import bypass.whitelist.util.Prefs
import bypass.whitelist.util.maskUrl
import java.net.Inet4Address
import java.net.InetAddress

private data class HookKey(val isPion: Boolean, val platform: CallPlatform)

class JsHookJoinFragment : Fragment() {

    private lateinit var webView: WebView
    private lateinit var toggleButton: View
    private lateinit var toggleArrow: ImageView
    private lateinit var toggleLabel: TextView
    private lateinit var relay: RelayController

    private var expanded = false
    private var callUrl = ""

    private val host: JoinFragmentHost?
        get() = activity as? JoinFragmentHost

    private val tunnelMode: TunnelMode
        get() = Prefs.tunnelMode

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
        requireContext().assets.open(name).bufferedReader().readText()

    override fun onCreateView(
        inflater: LayoutInflater,
        container: ViewGroup?,
        savedInstanceState: Bundle?,
    ): View = inflater.inflate(R.layout.fragment_jshook_join, container, false)

    @SuppressLint("SetJavaScriptEnabled")
    override fun onViewCreated(view: View, savedInstanceState: Bundle?) {
        webView = view.findViewById(R.id.webView)
        toggleButton = view.findViewById(R.id.toggleWebViewButton)
        toggleArrow = view.findViewById(R.id.toggleWebViewArrow)
        toggleLabel = view.findViewById(R.id.toggleWebViewLabel)

        relay = RelayController(
            nativeLibDir = requireContext().applicationInfo.nativeLibraryDir,
            onLog = { host?.appendLog(it) },
            onStatus = { status ->
                if (!relay.isRunning) return@RelayController
                host?.onJoinStatus(status)
            },
        )

        val platform = CallPlatform.fromUrl(requireArguments().getString(ARG_URL, ""))
        relay.start(tunnelMode, platform)

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
            userAgentString = DESKTOP_USER_AGENT
        }

        webView.addJavascriptInterface(JsBridge(), "AndroidBridge")

        webView.webChromeClient = object : WebChromeClient() {
            override fun onPermissionRequest(request: PermissionRequest) {
                activity?.runOnUiThread { request.grant(request.resources) }
            }

            override fun onConsoleMessage(msg: ConsoleMessage): Boolean {
                val text = msg.message()
                Log.d("HOOK", text)
                if (text.contains("[HOOK]")) {
                    host?.appendLog(text)
                    when {
                        text.contains("CALL CONNECTED") -> host?.onJoinStatus(VpnStatus.CALL_CONNECTED)
                        text.contains("DataChannel open") -> host?.onJoinStatus(VpnStatus.DATACHANNEL_OPEN)
                        text.contains("DataChannel closed") -> host?.onJoinStatus(VpnStatus.DATACHANNEL_LOST)
                        text.contains("WebSocket connected") -> host?.onJoinStatus(VpnStatus.TUNNEL_ACTIVE)
                        text.contains("WebSocket disconnected") -> host?.onJoinStatus(VpnStatus.TUNNEL_LOST)
                        text.contains("Connection state: connecting") -> host?.onJoinStatus(VpnStatus.CONNECTING)
                        text.contains("Connection state: disconnected") -> host?.onJoinStatus(VpnStatus.CALL_DISCONNECTED)
                        text.contains("Connection state: failed") -> host?.onJoinStatus(VpnStatus.CALL_FAILED)
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
                if (url.contains(BLANK_URL)) return
                view.evaluateJavascript(muteAudioContext, null)
            }

            override fun onPageFinished(view: WebView, url: String) {
                if (url.contains(BLANK_URL)) return
                if (!expanded && url != callUrl) activity?.runOnUiThread { setExpanded(true) }
                view.evaluateJavascript("!!window.__hookInstalled") { result ->
                    if (result == "true") {
                        Log.d("HOOK", "Hook already injected, skipping")
                        return@evaluateJavascript
                    }
                    val platform = CallPlatform.fromUrl(url)
                    host?.appendLog("Page loaded, injecting hook for ${maskUrl(url)}")
                    view.evaluateJavascript("window.WS_PORT=${mobile.Mobile.activeWsPort()}", null)
                    view.evaluateJavascript(hookForPlatform(platform), null)
                    if (Prefs.autoclickEnabled) {
                        host?.appendLog("Injecting autoclick for ${maskUrl(url)}")
                        view.evaluateJavascript("window.autofillName='${Prefs.autoclickName}'", null)
                        view.evaluateJavascript(autoclickers[platform]!!.value, null)
                    }
                }
            }
        }

        val url = requireArguments().getString(ARG_URL, "")
        if (url.isNotEmpty()) {
            callUrl = url
            webView.loadUrl(url)
        }
    }

    override fun onDestroyView() {
        webView.stopLoading()
        webView.loadUrl(BLANK_URL)
        webView.destroy()
        relay.stop()
        super.onDestroyView()
    }

    fun expand() {
        setExpanded(true)
    }

    private fun setExpanded(value: Boolean) {
        expanded = value
        webView.visibility = if (value) View.VISIBLE else View.GONE
        toggleArrow.rotation = if (value) 180f else 0f
        toggleLabel.setText(if (value) R.string.collapse_webview else R.string.expand_webview)
    }

    private fun hookForPlatform(platform: CallPlatform): String =
        hooks[HookKey(tunnelMode.isPion, platform)]!!.value

    private fun stripCsp(url: String, request: WebResourceRequest): WebResourceResponse? {
        return try {
            val conn = java.net.URL(url).openConnection() as java.net.HttpURLConnection
            conn.requestMethod = "GET"
            request.requestHeaders?.forEach { (key, value) -> conn.setRequestProperty(key, value) }
            val headers = mutableMapOf<String, String>()
            conn.headerFields?.forEach { (key, values) ->
                if (key != null
                    && !key.equals("content-security-policy", ignoreCase = true)
                    && !key.equals("content-security-policy-report-only", ignoreCase = true)
                ) {
                    headers[key] = values.joinToString(", ")
                }
            }
            WebResourceResponse(
                conn.contentType?.split(";")?.firstOrNull() ?: "text/html",
                "utf-8", conn.responseCode, conn.responseMessage ?: "OK",
                headers, conn.inputStream
            )
        } catch (_: Exception) { null }
    }

    private fun getLocalIPAddress(): String {
        try {
            val context = context ?: return ""
            val connectivityManager = context.getSystemService(android.content.Context.CONNECTIVITY_SERVICE) as ConnectivityManager
            val network = connectivityManager.activeNetwork ?: return ""
            val linkProperties = connectivityManager.getLinkProperties(network) ?: return ""
            for (addr in linkProperties.linkAddresses) {
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
        fun log(msg: String) = host?.appendLog(msg)

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
            host?.appendLog("Tunnel ready, starting VPN...")
            host?.onJoinStatus(VpnStatus.TUNNEL_ACTIVE)
            activity?.runOnUiThread { host?.requestVpn() }
        }

        @JavascriptInterface
        fun onCaptchaDetected(isDone: Boolean) {
            if (!isDone) {
                host?.onJoinStatus(VpnStatus.ACTION_REQUIRED_CAPTCHA)
                activity?.runOnUiThread { expand() }
            } else {
                host?.onJoinStatus(VpnStatus.CONNECTING)
            }
        }
    }

    companion object {
        const val ARG_URL = "url"

        fun newInstance(url: String): JsHookJoinFragment {
            return JsHookJoinFragment().apply {
                arguments = Bundle().apply {
                    putString(ARG_URL, url)
                }
            }
        }
    }
}
