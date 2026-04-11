package bypass.whitelist.ui

import android.os.Bundle
import android.util.Log
import android.view.LayoutInflater
import android.view.View
import android.view.ViewGroup
import android.widget.TextView
import androidx.fragment.app.Fragment
import bypass.whitelist.R
import bypass.whitelist.tunnel.HeadlessRelayController
import bypass.whitelist.tunnel.VpnStatus
import bypass.whitelist.util.Prefs

class HeadlessTelemostFragment : Fragment() {

    private lateinit var relay: HeadlessRelayController
    private lateinit var statusText: TextView

    private val host: JoinFragmentHost?
        get() = activity as? JoinFragmentHost

    override fun onCreateView(
        inflater: LayoutInflater,
        container: ViewGroup?,
        savedInstanceState: Bundle?,
    ): View = inflater.inflate(R.layout.fragment_headless_telemost, container, false)

    override fun onViewCreated(view: View, savedInstanceState: Bundle?) {
        statusText = view.findViewById(R.id.statusText)
        statusText.setText(R.string.headless_telemost_starting)

        val joinLink = requireArguments().getString(ARG_URL, "")
        val displayName = Prefs.autoclickName
        val tunnelMode = Prefs.tunnelMode.relayArg

        relay = HeadlessRelayController(
            requireContext().applicationInfo.nativeLibraryDir,
            relayMode = "telemost-headless-joiner",
            onLog = { host?.appendLog(it) },
            onStatus = { status ->
                Log.d("TM-HEADLESS", "status: $status")
                activity?.runOnUiThread { statusText.text = status.name }
                host?.onJoinStatus(status)
                when (status) {
                    VpnStatus.STARTING -> {
                        val params = org.json.JSONObject().apply {
                            put("joinLink", joinLink)
                            put("displayName", displayName)
                            put("tunnelMode", tunnelMode)
                        }
                        relay.sendJoinParams(params.toString())
                    }
                    VpnStatus.TUNNEL_ACTIVE -> activity?.runOnUiThread { host?.requestVpn() }
                    else -> {}
                }
            },
        )
        relay.start()
    }

    override fun onDestroyView() {
        if (::relay.isInitialized) relay.stop()
        super.onDestroyView()
    }

    companion object {
        const val ARG_URL = "url"

        fun newInstance(url: String): HeadlessTelemostFragment {
            return HeadlessTelemostFragment().apply {
                arguments = Bundle().apply {
                    putString(ARG_URL, url)
                }
            }
        }
    }
}
