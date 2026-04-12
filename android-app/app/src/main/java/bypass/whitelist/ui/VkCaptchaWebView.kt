package bypass.whitelist.ui

import android.annotation.SuppressLint
import android.util.Log
import android.webkit.ConsoleMessage
import android.webkit.JavascriptInterface
import android.webkit.WebChromeClient
import android.webkit.WebResourceRequest
import android.webkit.WebResourceResponse
import android.webkit.WebView
import android.webkit.WebViewClient
import androidx.appcompat.app.AppCompatActivity
import bypass.whitelist.util.DESKTOP_USER_AGENT
import bypass.whitelist.util.ParamCallback
import org.json.JSONObject
import java.net.HttpURLConnection
import java.net.URL

@SuppressLint("SetJavaScriptEnabled")
class VkCaptchaWebView(
    private val activity: AppCompatActivity,
    private val webView: WebView,
    private val onStatus: ParamCallback<String>,
    private val onToken: ParamCallback<String>,
) {

    private val authScript by lazy {
        activity.assets.open("headless-auth-vk.js").bufferedReader().readText()
    }

    private val authPageHTML = """
        <!DOCTYPE html>
        <html>
        <body>
        <iframe id="captcha" style="width:100%;height:100vh;border:none;display:none;"></iframe>
        </body>
        </html>
    """.trimIndent()

    private var pendingHTML: String? = null
    private var pendingJoinLink = ""
    private var pendingAnonName = ""

    fun setup() {
        webView.settings.apply {
            javaScriptEnabled = true
            domStorageEnabled = true
            allowContentAccess = true
            userAgentString = DESKTOP_USER_AGENT
        }

        webView.addJavascriptInterface(CaptchaBridge(), "AndroidBridge")
        webView.addJavascriptInterface(AuthBridge(), "AndroidCaptchaBridge")

        webView.webChromeClient = object : WebChromeClient() {
            override fun onConsoleMessage(msg: ConsoleMessage): Boolean {
                Log.d("CAPTCHA", "${msg.sourceId()?.takeLast(30)}:${msg.lineNumber()} ${msg.message()}")
                return true
            }
        }

        webView.webViewClient = object : WebViewClient() {
            override fun shouldInterceptRequest(view: WebView, request: WebResourceRequest): WebResourceResponse? {
                val url = request.url.toString()
                if (url.contains("vk.com/blank.php") && pendingHTML != null) {
                    val html = pendingHTML!!
                    pendingHTML = null
                    return WebResourceResponse(
                        "text/html", "utf-8", 200, "OK",
                        mapOf("Access-Control-Allow-Origin" to "*"),
                        html.byteInputStream()
                    )
                }
                if (url.contains("id.vk.com") || url.contains("okcdn.ru")) {
                    Log.d("CAPTCHA", "REQ: $url")
                    return stripHeaders(url, request)
                }
                if (url.contains("captchaNotRobot") || url.contains("captcha")) {
                    Log.d("CAPTCHA", "REQ: $url")
                }
                return null
            }

            override fun onPageFinished(view: WebView, url: String) {
                if (!url.contains("blank.php")) return
                view.evaluateJavascript("window.JOIN_LINK='${escapeJs(pendingJoinLink)}';window.ANON_NAME='${escapeJs(pendingAnonName)}';", null)
                view.evaluateJavascript(authScript, null)
            }
        }
    }

    fun start(joinLink: String, anonName: String) {
        pendingJoinLink = joinLink
        pendingAnonName = anonName
        pendingHTML = authPageHTML
        webView.loadUrl("https://vk.com/blank.php")
    }

    @Suppress("unused")
    inner class AuthBridge {
        @JavascriptInterface
        fun onJoined(json: String) {
            Log.d("CAPTCHA", "onJoined: ${json.take(200)}")
            onToken(json)
        }

        @JavascriptInterface
        fun setStatus(message: String) {
            Log.d("CAPTCHA", "status: $message")
            activity.runOnUiThread { onStatus(message) }
        }
    }

    @Suppress("unused", "FunctionName")
    inner class CaptchaBridge {
        @JavascriptInterface
        fun VKCaptchaGetResult(json: String) {
            Log.d("CAPTCHA", "VKCaptchaGetResult: ${json.take(200)}")
            val token = try {
                JSONObject(json).getString("token")
            } catch (e: Exception) { json }
            webView.post {
                webView.evaluateJavascript("if(typeof retryCaptcha==='function')retryCaptcha('${token.replace("'", "\\'")}');", null)
            }
        }

        @JavascriptInterface
        fun VKCaptchaClose(json: String) {
            Log.d("CAPTCHA", "VKCaptchaClose: $json")
        }
    }

    private fun escapeJs(value: String): String =
        value.replace("\\", "\\\\").replace("'", "\\'").replace("\n", "\\n")

    private fun stripHeaders(url: String, request: WebResourceRequest): WebResourceResponse? {
        return try {
            val conn = URL(url).openConnection() as HttpURLConnection
            conn.requestMethod = request.method ?: "GET"
            request.requestHeaders?.forEach { (key, value) -> conn.setRequestProperty(key, value) }
            val headers = mutableMapOf<String, String>()
            conn.headerFields?.forEach { (key, values) ->
                if (key != null
                    && !key.equals("content-security-policy", ignoreCase = true)
                    && !key.equals("content-security-policy-report-only", ignoreCase = true)
                    && !key.equals("x-frame-options", ignoreCase = true)
                ) {
                    headers[key] = values.joinToString(", ")
                }
            }
            headers["Access-Control-Allow-Origin"] = "*"
            WebResourceResponse(
                conn.contentType?.split(";")?.firstOrNull() ?: "text/html",
                "utf-8", conn.responseCode, conn.responseMessage ?: "OK",
                headers, conn.inputStream
            )
        } catch (_: Exception) { null }
    }
}
