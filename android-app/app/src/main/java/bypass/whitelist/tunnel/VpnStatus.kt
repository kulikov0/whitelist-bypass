package bypass.whitelist.tunnel

import androidx.annotation.StringRes
import bypass.whitelist.R

enum class VpnStatus(@StringRes val labelRes: Int) {
    STARTING(R.string.vpn_starting),
    CONNECTING(R.string.vpn_connecting),
    CALL_CONNECTED(R.string.vpn_call_connected),
    DATACHANNEL_OPEN(R.string.vpn_datachannel_open),
    DATACHANNEL_LOST(R.string.vpn_datachannel_lost),
    TUNNEL_ACTIVE(R.string.vpn_tunnel_active),
    TUNNEL_LOST(R.string.vpn_tunnel_lost),
    CALL_DISCONNECTED(R.string.vpn_call_disconnected),
    CALL_FAILED(R.string.vpn_call_failed),
    PORT_BUSY(R.string.vpn_port_busy),
    ACTION_REQUIRED_CAPTCHA(R.string.action_required_captcha)
}
