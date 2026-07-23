package bypass.whitelist.ui

import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.view.View
import androidx.fragment.app.Fragment
import bypass.whitelist.R
import bypass.whitelist.tunnel.CallConfig
import bypass.whitelist.tunnel.CallPlatform
import bypass.whitelist.tunnel.TunnelMode
import bypass.whitelist.tunnel.VpnStatus
import bypass.whitelist.util.Prefs

class MainFragment : Fragment(R.layout.fragment_main_screen) {

    private var content: MainFragmentView? = null
    private var pendingStatus: VpnStatus? = null
    private var connectedSinceMs: Long = 0L
    private val tickHandler = Handler(Looper.getMainLooper())
    private val tickRunnable = object : Runnable {
        override fun run() {
            refreshStats()
            tickHandler.postDelayed(this, 1000L)
        }
    }

    interface Host {
        fun onConnectPressed(config: CallConfig)
        fun onDisconnectPressed()
        fun onPingPressed(callback: (success: Boolean, rttMs: Int) -> Unit)
        fun isTunnelActive(): Boolean
        fun currentStatus(): VpnStatus?
    }

    override fun onViewCreated(rootView: View, savedInstanceState: Bundle?) {
        val container = MainFragmentView(rootView)
        content = container

        container.bindCalls(Prefs.savedDestinations, Prefs.activeDestinationId)
        container.bindHero(connected = isHostConnected(), status = hostStatus())
        if (!isResumed) container.pauseAnimations()

        container.onAddCallClicked = {
            AddDestinationSheet.show(parentFragmentManager)
        }
        container.onHeroPressed = {
            if (isHostConnected() || isHostConnecting()) {
                host()?.onDisconnectPressed()
            } else {
                val active = Prefs.activeDestination
                if (active != null) {
                    host()?.onConnectPressed(active)
                }
            }
        }
        container.onPingPressed = {
            container.showPingRunning()
            host()?.onPingPressed { success, rttMs ->
                container.showPingResult(success, rttMs)
            }
        }
        container.onCallSelected = { config ->
            Prefs.activeDestinationId = config.id
            container.bindCalls(Prefs.savedDestinations, Prefs.activeDestinationId)
        }
        container.onCallLongPressed = ::showRowMenu

        pendingStatus?.let { container.bindStatus(it) }
        pendingStatus = null
    }

    override fun onResume() {
        super.onResume()
        content?.bindCalls(Prefs.savedDestinations, Prefs.activeDestinationId)
        content?.bindHero(connected = isHostConnected(), status = hostStatus())
        content?.resumeAnimations()
        if (isHostConnected()) {
            tickHandler.removeCallbacks(tickRunnable)
            tickHandler.postDelayed(tickRunnable, 1000L)
        }
    }

    override fun onPause() {
        super.onPause()
        content?.pauseAnimations()
        tickHandler.removeCallbacks(tickRunnable)
    }

    override fun onDestroyView() {
        tickHandler.removeCallbacks(tickRunnable)
        content?.detach()
        content = null
        super.onDestroyView()
    }

    fun onStatusChanged(status: VpnStatus) {
        val container = content
        if (container != null) {
            container.bindStatus(status)
        } else {
            pendingStatus = status
        }
        if (isHostConnected()) refreshStats()
    }

    fun onStatusTextChanged(text: String) {
        content?.bindStatusText(text)
    }

    fun onConnectedChanged(connected: Boolean) {
        if (connected) {
            if (connectedSinceMs == 0L) connectedSinceMs = System.currentTimeMillis()
        } else {
            connectedSinceMs = 0L
        }
        if (!isResumed) return
        content?.bindHero(connected = connected, status = hostStatus())
        if (connected) {
            refreshStats()
            tickHandler.removeCallbacks(tickRunnable)
            tickHandler.postDelayed(tickRunnable, 1000L)
        } else {
            tickHandler.removeCallbacks(tickRunnable)
        }
    }

    fun onDestinationsChanged() {
        content?.bindCalls(Prefs.savedDestinations, Prefs.activeDestinationId)
    }

    private fun showRowMenu(config: CallConfig) {
        val tunnelMode = (config.tunnelMode ?: Prefs.tunnelMode).forPlatform(config.platform)
        val vp8Sub = buildString {
            append(getString(R.string.settings_row_vp8_sub, config.vp8Fps ?: Prefs.vp8Fps, config.vp8Batch ?: Prefs.vp8Batch))
            if (config.dualTrack ?: Prefs.dualTrack) append(" / ").append(getString(R.string.settings_row_vp8_flag_dual))
            if (config.reliable ?: Prefs.reliable) append(" / ").append(getString(R.string.settings_row_vp8_flag_kcp))
        }
        MenuActionSheet.show(
            manager = parentFragmentManager,
            title = config.name,
            subtitle = config.url,
            items = listOf(
                MenuActionSheet.MenuItem("tunnel", getString(R.string.settings_row_tunnel_mode), R.drawable.ic_setting_tunnel, value = tunnelMode.label),
                MenuActionSheet.MenuItem("vp8", getString(R.string.settings_row_vp8), R.drawable.ic_setting_vp8, value = vp8Sub),
                MenuActionSheet.MenuItem("rename", getString(R.string.destination_menu_rename), R.drawable.ic_action_pencil),
                MenuActionSheet.MenuItem("delete", getString(R.string.destination_menu_delete), R.drawable.ic_setting_trash, danger = true),
            ),
        ) { item ->
            when (item.id) {
                "tunnel" -> editTunnelMode(config)
                "vp8" -> editVp8(config)
                "rename" -> promptRename(config)
                "delete" -> confirmDelete(config)
            }
        }
    }

    private fun editTunnelMode(config: CallConfig) {
        val current = (config.tunnelMode ?: Prefs.tunnelMode).forPlatform(config.platform)
        ChoiceActionSheet.show(
            manager = parentFragmentManager,
            title = getString(R.string.settings_row_tunnel_mode),
            options = TunnelMode.entries.filter { it == TunnelMode.VIDEO || (config.platform != CallPlatform.TELEMOST && config.platform != CallPlatform.DION) }.map { ChoiceActionSheet.Option(it.name, it.label) },
            selectedId = current.name,
        ) { picked ->
            val newMode = TunnelMode.valueOf(picked.id)
            Prefs.updateDestination(config.copy(tunnelMode = newMode))
            onDestinationsChanged()
            if (Prefs.activeDestinationId == config.id) {
                (activity as? SettingsScreenFragment.Host)?.onTunnelModeChanged(newMode)
            }
        }
    }

    private fun editVp8(config: CallConfig) {
        Vp8ActionSheet.show(
            parentFragmentManager,
            config.vp8Fps ?: Prefs.vp8Fps,
            config.vp8Batch ?: Prefs.vp8Batch,
            config.dualTrack ?: Prefs.dualTrack,
            config.reliable ?: Prefs.reliable,
        ) { fps, batch, dual, reliable ->
            Prefs.updateDestination(config.copy(vp8Fps = fps, vp8Batch = batch, dualTrack = dual, reliable = reliable))
            onDestinationsChanged()
        }
    }

    private fun promptRename(config: CallConfig) {
        InputActionSheet.show(
            manager = parentFragmentManager,
            title = getString(R.string.destination_rename_title),
            fieldLabel = getString(R.string.sheet_field_name),
            initialValue = config.name,
        ) { newName ->
            if (newName != config.name) {
                Prefs.renameDestination(config.id, newName)
                onDestinationsChanged()
            }
        }
    }

    private fun confirmDelete(config: CallConfig) {
        ConfirmActionSheet.show(
            manager = parentFragmentManager,
            title = getString(R.string.destination_delete_title),
            subtitle = getString(R.string.destination_delete_confirm, config.name),
            confirmLabel = getString(R.string.confirm_delete),
            cancelLabel = getString(R.string.sheet_cancel),
            destructive = true,
        ) {
            Prefs.removeDestination(config.id)
            onDestinationsChanged()
        }
    }

    private fun refreshStats() {
        val view = content ?: return
        val uptimeMs = if (connectedSinceMs > 0L) System.currentTimeMillis() - connectedSinceMs else 0L
        val active = Prefs.activeDestination
        val mode = if (active != null) Prefs.activeTunnelMode.forPlatform(active.platform) else Prefs.tunnelMode
        view.setStats(uptimeText = formatUptime(uptimeMs), mode = mode.label)
    }

    private fun formatUptime(ms: Long): String {
        if (ms <= 0L) return "00:00:00"
        val totalSeconds = ms / 1000L
        val hours = totalSeconds / 3600L
        val minutes = (totalSeconds / 60L) % 60L
        val seconds = totalSeconds % 60L
        return "%02d:%02d:%02d".format(hours, minutes, seconds)
    }

    private fun host(): Host? = activity as? Host

    private fun isHostConnected(): Boolean = host()?.isTunnelActive() ?: false

    private fun isHostConnecting(): Boolean = when (hostStatus()) {
        VpnStatus.STOPPING,
        VpnStatus.CONNECTING,
        VpnStatus.STARTING,
        VpnStatus.CALL_CONNECTED,
        VpnStatus.DATACHANNEL_OPEN -> true
        else -> false
    }

    private fun hostStatus(): VpnStatus? = host()?.currentStatus()
}
