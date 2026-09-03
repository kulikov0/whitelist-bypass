package bypass.whitelist.ui

import android.os.Bundle
import android.view.View
import android.view.ViewGroup
import android.widget.ImageView
import android.widget.LinearLayout
import android.widget.TextView
import androidx.core.view.isNotEmpty
import androidx.fragment.app.Fragment
import bypass.whitelist.App
import bypass.whitelist.R
import bypass.whitelist.tunnel.SplitTunnelingMode
import bypass.whitelist.tunnel.TunnelMode
import bypass.whitelist.util.BackgroundRestrictionsManager
import bypass.whitelist.util.Callback
import bypass.whitelist.util.ParamCallback
import bypass.whitelist.util.Prefs
import bypass.whitelist.util.ThemeMode
import com.google.android.material.materialswitch.MaterialSwitch

class SettingsScreenFragment : Fragment(R.layout.fragment_settings_screen) {

    interface Host {
        fun onTunnelModeChanged(mode: TunnelMode)
        fun onForgetAllDestinations()
        fun onResetAllSettings()
    }

    override fun onViewCreated(view: View, savedInstanceState: Bundle?) {
        val root = view.findViewById<LinearLayout>(R.id.settingsContent)
        root.removeAllViews()
        root.addView(buildAppearanceSection())
        root.addView(buildTunnelSection())
        root.addView(buildNetworkSection())
        root.addView(buildBehaviorSection())
        root.addView(buildDangerSection())
    }

    override fun onResume() {
        super.onResume()
        rebuild()
    }

    fun refresh() {
        rebuild()
    }

    private fun host(): Host? = activity as? Host

    private fun buildAppearanceSection(): View {
        val section = newSection(R.string.settings_section_appearance)
        val card = section.findViewById<LinearLayout>(R.id.sectionCard)
        addRow(card, R.drawable.ic_setting_theme, getString(R.string.settings_row_theme), getString(R.string.settings_row_theme_sub), Prefs.themeMode.label) {
            ChoiceActionSheet.show(
                manager = parentFragmentManager,
                title = getString(R.string.settings_row_theme),
                subtitle = getString(R.string.settings_row_theme_sub),
                options = ThemeMode.entries.map { ChoiceActionSheet.Option(it.name, it.label) },
                selectedId = Prefs.themeMode.name,
            ) { picked ->
                val mode = ThemeMode.valueOf(picked.id)
                if (mode != Prefs.themeMode) {
                    Prefs.themeMode = mode
                    App.applyTheme(mode)
                    rebuild()
                }
            }
        }
        return section
    }

    private fun buildTunnelSection(): View {
        val section = newSection(R.string.settings_section_tunnel)
        val card = section.findViewById<LinearLayout>(R.id.sectionCard)

        addRow(card, R.drawable.ic_setting_tunnel, getString(R.string.settings_row_tunnel_mode), null, Prefs.tunnelMode.label) {
            ChoiceActionSheet.show(
                manager = parentFragmentManager,
                title = getString(R.string.settings_row_tunnel_mode),
                options = TunnelMode.entries.map { ChoiceActionSheet.Option(it.name, it.label) },
                selectedId = Prefs.tunnelMode.name,
            ) { picked ->
                val newMode = TunnelMode.valueOf(picked.id)
                if (newMode != Prefs.tunnelMode) {
                    Prefs.tunnelMode = newMode
                    host()?.onTunnelModeChanged(newMode)
                    rebuild()
                }
            }
        }

        val vp8Sub = buildString {
            append(getString(R.string.settings_row_vp8_sub, Prefs.vp8Fps, Prefs.vp8Batch))
            if (Prefs.dualTrack) append(" / ").append(getString(R.string.settings_row_vp8_flag_dual))
            if (Prefs.reliable) append(" / ").append(getString(R.string.settings_row_vp8_flag_kcp))
        }
        addRow(card, R.drawable.ic_setting_vp8, getString(R.string.settings_row_vp8), vp8Sub, null) {
            Vp8ActionSheet.show(parentFragmentManager, Prefs.vp8Fps, Prefs.vp8Batch, Prefs.dualTrack, Prefs.reliable) { fps, batch, dual, reliable ->
                Prefs.vp8Fps = fps
                Prefs.vp8Batch = batch
                Prefs.dualTrack = dual
                Prefs.reliable = reliable
                rebuild()
            }
        }

        addRow(card, R.drawable.ic_setting_autofill, getString(R.string.settings_row_autofill), if (Prefs.autofillEnabled) Prefs.autofillName else getString(R.string.settings_row_autofill_off), null) {
            AutofillActionSheet.show(parentFragmentManager) { rebuild() }
        }

        return section
    }

    private fun buildNetworkSection(): View {
        val section = newSection(R.string.settings_section_network)
        val card = section.findViewById<LinearLayout>(R.id.sectionCard)

        val splitSummary = if (Prefs.splitTunnelingMode == SplitTunnelingMode.NONE) {
            Prefs.splitTunnelingMode.label
        } else {
            resources.getQuantityString(R.plurals.split_tunneling_summary_count, Prefs.splitTunnelingPackages.size, Prefs.splitTunnelingMode.label, Prefs.splitTunnelingPackages.size)
        }
        addRow(card, R.drawable.ic_setting_split, getString(R.string.settings_row_split), splitSummary, null) {
            (activity as? MainActivityHost)?.pushSubPage(SplitTunnelingScreenFragment())
        }

        addRow(card, R.drawable.ic_setting_proxy, getString(R.string.settings_row_proxy), getString(R.string.settings_row_proxy_sub, Prefs.socksPort), null) {
            ProxyActionSheet.show(parentFragmentManager) { rebuild() }
        }

        addRow(card, R.drawable.ic_setting_dns, getString(R.string.settings_row_dns), Prefs.dnsMode.label, null) {
            DnsActionSheet.show(parentFragmentManager) { rebuild() }
        }

        return section
    }

    private fun buildBehaviorSection(): View {
        val section = newSection(R.string.settings_section_behavior)
        val card = section.findViewById<LinearLayout>(R.id.sectionCard)

        addSwitchRow(card, R.drawable.ic_setting_headless, getString(R.string.settings_row_headless), getString(R.string.settings_row_headless_sub), Prefs.headless) { checked ->
            Prefs.headless = checked
        }
        addSwitchRow(card, R.drawable.ic_setting_reconnect, getString(R.string.settings_row_reconnect), getString(R.string.settings_row_reconnect_sub), Prefs.connectOnStart) { checked ->
            Prefs.connectOnStart = checked
        }
        addSwitchRow(card, R.drawable.ic_setting_debug, getString(R.string.settings_row_debug), getString(R.string.settings_row_debug_sub), Prefs.debug) { checked ->
            Prefs.debug = checked
        }
        addRow(card, R.drawable.ic_power, getString(R.string.settings_row_power), getString(R.string.settings_row_power_sub), null) {
            BackgroundRestrictionsManager.requestDisableBatteryOptimization(this.requireActivity());
        }
        return section
    }

    private fun buildDangerSection(): View {
        val section = newSection(R.string.settings_section_danger)
        val card = section.findViewById<LinearLayout>(R.id.sectionCard)
        addRow(card, R.drawable.ic_setting_reset, getString(R.string.settings_reset_all), getString(R.string.settings_reset_all_sub), null, danger = true) {
            ConfirmActionSheet.show(
                manager = parentFragmentManager,
                title = getString(R.string.settings_reset_all),
                subtitle = getString(R.string.settings_reset_all_sub),
                confirmLabel = getString(R.string.confirm_reset),
                cancelLabel = getString(R.string.sheet_cancel),
                destructive = true,
            ) { host()?.onResetAllSettings() }
        }
        addRow(card, R.drawable.ic_setting_trash, getString(R.string.settings_forget_all_destinations), getString(R.string.settings_forget_all_destinations_sub), null, danger = true) {
            ConfirmActionSheet.show(
                manager = parentFragmentManager,
                title = getString(R.string.settings_forget_all_destinations),
                subtitle = getString(R.string.settings_forget_all_destinations_sub),
                confirmLabel = getString(R.string.confirm_forget),
                cancelLabel = getString(R.string.sheet_cancel),
                destructive = true,
            ) { host()?.onForgetAllDestinations() }
        }
        return section
    }

    private fun newSection(labelRes: Int): View {
        val parent = view as ViewGroup?
        val v = layoutInflater.inflate(R.layout.item_settings_section, parent, false)
        v.findViewById<TextView>(R.id.sectionLabel).setText(labelRes)
        v.findViewById<View>(R.id.sectionCard).clipToOutline = true
        return v
    }

    private fun addRow(
        card: LinearLayout,
        iconRes: Int,
        title: String,
        sub: String?,
        trail: String?,
        danger: Boolean = false,
        onClick: Callback,
    ) {
        val row = layoutInflater.inflate(R.layout.item_settings_row, card, false)
        row.findViewById<ImageView>(R.id.rowIcon).setImageResource(iconRes)
        if (danger) {
            row.findViewById<View>(R.id.rowIconBox).setBackgroundResource(R.drawable.bg_settings_row_icon_danger)
            row.findViewById<ImageView>(R.id.rowIcon).setColorFilter(requireContext().getColor(R.color.error_red))
            row.findViewById<TextView>(R.id.rowTitle).setTextColor(requireContext().getColor(R.color.error_red))
        }
        row.findViewById<TextView>(R.id.rowTitle).text = title
        row.findViewById<TextView>(R.id.rowSub).apply {
            if (sub.isNullOrBlank()) { visibility = View.GONE } else { text = sub; visibility = View.VISIBLE }
        }
        row.findViewById<TextView>(R.id.rowTrail).apply {
            if (trail.isNullOrBlank()) { visibility = View.GONE } else { text = trail; visibility = View.VISIBLE }
        }
        row.findViewById<ImageView>(R.id.rowChev).visibility = View.VISIBLE
        row.setOnClickListener { onClick() }
        if (card.isNotEmpty()) addDividerTo(card)
        card.addView(row)
    }

    private fun addSwitchRow(
        card: LinearLayout,
        iconRes: Int,
        title: String,
        sub: String?,
        initial: Boolean,
        onToggled: ParamCallback<Boolean>,
    ) {
        val row = layoutInflater.inflate(R.layout.item_settings_row, card, false)
        row.findViewById<ImageView>(R.id.rowIcon).setImageResource(iconRes)
        row.findViewById<TextView>(R.id.rowTitle).text = title
        row.findViewById<TextView>(R.id.rowSub).apply {
            if (sub.isNullOrBlank()) { visibility = View.GONE } else { text = sub; visibility = View.VISIBLE }
        }
        val sw = row.findViewById<MaterialSwitch>(R.id.rowSwitch)
        sw.visibility = View.VISIBLE
        sw.isChecked = initial
        row.setOnClickListener {
            sw.isChecked = !sw.isChecked
            onToggled(sw.isChecked)
        }
        if (card.isNotEmpty()) addDividerTo(card)
        card.addView(row)
    }

    private fun addDividerTo(card: LinearLayout) {
        val divider = View(requireContext()).apply {
            layoutParams = LinearLayout.LayoutParams(LinearLayout.LayoutParams.MATCH_PARENT, 1).apply {
                marginStart = (14 * resources.displayMetrics.density).toInt()
                marginEnd = (14 * resources.displayMetrics.density).toInt()
            }
            setBackgroundColor(requireContext().getColor(R.color.hair))
        }
        card.addView(divider)
    }

    private fun rebuild() {
        if (!isAdded) return
        val rootView = view ?: return
        onViewCreated(rootView, null)
    }
}
