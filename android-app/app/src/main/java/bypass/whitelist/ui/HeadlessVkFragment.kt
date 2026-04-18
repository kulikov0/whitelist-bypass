package bypass.whitelist.ui

import android.os.Bundle
import android.util.Log
import android.view.LayoutInflater
import android.view.View
import android.view.ViewGroup
import android.webkit.WebView
import android.webkit.WebViewClient
import androidx.core.view.isVisible
import androidx.fragment.app.Fragment
import bypass.whitelist.R
import bypass.whitelist.util.BLANK_URL
import bypass.whitelist.tunnel.HeadlessRelayController
import bypass.whitelist.tunnel.VpnStatus
import bypass.whitelist.util.Prefs

class HeadlessVkFragment : Fragment() {

    private lateinit var relay: HeadlessRelayController
    private lateinit var webView: WebView

    private val host: JoinFragmentHost?
        get() = activity as? JoinFragmentHost

    override fun onCreateView(
        inflater: LayoutInflater,
        container: ViewGroup?,
        savedInstanceState: Bundle?,
    ): View = inflater.inflate(R.layout.fragment_headless_vk, container, false)

    override fun onViewCreated(view: View, savedInstanceState: Bundle?) {
        webView = view.findViewById(R.id.captchaWebView)
        val url = requireArguments().getString(ARG_URL, "")
        val displayName = Prefs.autoclickName

        webView.settings.javaScriptEnabled = true
        webView.settings.domStorageEnabled = true
        webView.webViewClient = WebViewClient()
        webView.isVisible = false

        relay = HeadlessRelayController(
            requireContext().applicationInfo.nativeLibraryDir,
            onLog = { message ->
                if (message.contains("ERROR:") && !message.contains("ortc ERROR")) {
                    host?.onJoinStatusText(message)
                }
                host?.appendLog(message)
            },
            onStatus = { status ->
                Log.d("HEADLESS-VK", "status: $status")
                host?.onJoinStatus(status)
                if (status == VpnStatus.TUNNEL_ACTIVE) {
                    activity?.runOnUiThread {
                        webView.stopLoading()
                        webView.loadUrl(BLANK_URL)
                        webView.isVisible = false
                        host?.requestVpn()
                    }
                }
            },
            onCaptchaUrl = { captchaUrl ->
                Log.d("HEADLESS-VK", "captcha URL: $captchaUrl")
                activity?.runOnUiThread {
                    webView.isVisible = true
                    webView.loadUrl(captchaUrl)
                }
            },
        )
        relay.start()
        relay.sendAuth(url, displayName, Prefs.tunnelMode.relayArg)
    }

    override fun onDestroyView() {
        webView.stopLoading()
        webView.loadUrl(BLANK_URL)
        webView.destroy()
        relay.stop()
        super.onDestroyView()
    }

    companion object {
        const val ARG_URL = "url"

        fun newInstance(url: String): HeadlessVkFragment {
            return HeadlessVkFragment().apply {
                arguments = Bundle().apply {
                    putString(ARG_URL, url)
                }
            }
        }
    }
}
