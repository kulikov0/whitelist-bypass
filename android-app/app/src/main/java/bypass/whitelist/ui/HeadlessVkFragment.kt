package bypass.whitelist.ui

import android.os.Bundle
import android.util.Log
import android.view.LayoutInflater
import android.view.View
import android.view.ViewGroup
import android.webkit.WebView
import androidx.fragment.app.Fragment
import bypass.whitelist.R
import bypass.whitelist.util.BLANK_URL
import bypass.whitelist.tunnel.HeadlessRelayController
import bypass.whitelist.tunnel.VpnStatus
import bypass.whitelist.util.Prefs
import org.json.JSONObject

class HeadlessVkFragment : Fragment() {

    private lateinit var relay: HeadlessRelayController
    private lateinit var captchaView: VkCaptchaWebView
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

        relay = HeadlessRelayController(
            requireContext().applicationInfo.nativeLibraryDir,
            onLog = { message ->
                if (message.contains("ERROR:")) {
                    activity?.runOnUiThread {
                        webView.evaluateJavascript(
                            "document.getElementById('status').textContent='${message.replace("'", "\\'")}'", null
                        )
                    }
                }
                host?.appendLog(message)
            },
            onStatus = { status ->
                Log.d("HEADLESS-VK", "status: $status")
                host?.onJoinStatus(status)
                if (status == VpnStatus.TUNNEL_ACTIVE) {
                    activity?.runOnUiThread { host?.requestVpn() }
                }
            },
        )
        relay.start()

        captchaView = VkCaptchaWebView(requireActivity() as androidx.appcompat.app.AppCompatActivity, webView) { joinJson ->
            Log.d("HEADLESS-VK", "Auth complete, sending join params to relay")
            val params = JSONObject(joinJson)
            params.put("tunnelMode", Prefs.tunnelMode.relayArg)
            relay.sendJoinParams(params.toString())
        }
        captchaView.setup()
        captchaView.start(url, displayName)
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
