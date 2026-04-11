package bypass.whitelist.ui

import bypass.whitelist.tunnel.VpnStatus

interface JoinFragmentHost {
    fun appendLog(message: String)
    fun onJoinStatus(status: VpnStatus)
    fun requestVpn()
}
