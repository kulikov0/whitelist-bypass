package bypass.whitelist.ui

import android.app.Dialog
import android.content.pm.ApplicationInfo
import android.text.Editable
import android.text.TextWatcher
import android.content.pm.PackageManager
import android.os.Bundle
import android.view.LayoutInflater
import android.view.View
import android.view.ViewGroup
import android.widget.Button
import android.widget.CheckBox
import android.widget.EditText
import android.widget.LinearLayout
import android.widget.ListView
import android.widget.ProgressBar
import android.widget.TextView
import android.widget.Toast
import androidx.appcompat.app.AlertDialog
import androidx.fragment.app.DialogFragment
import bypass.whitelist.R
import bypass.whitelist.tunnel.SplitTunnelingMode
import bypass.whitelist.tunnel.TunnelMode
import bypass.whitelist.tunnel.TunnelVpnService
import bypass.whitelist.util.Prefs

class SettingsDialogFragment : DialogFragment() {

    interface Listener {
        fun onTunnelModeChanged(mode: TunnelMode)
        fun onShowLogsChanged(visible: Boolean)
        fun onShareLogs()
        fun onReset()
    }

    private var listener: Listener? = null

    private var tunnelMode: TunnelMode = Prefs.tunnelMode
    private var splitTunnelingMode: SplitTunnelingMode = Prefs.splitTunnelingMode
    private var splitTunnelingPackages: MutableSet<String> = Prefs.splitTunnelingPackages.toMutableSet()

    override fun onCreateDialog(savedInstanceState: Bundle?): Dialog {
        val dialog = super.onCreateDialog(savedInstanceState)
        dialog.setTitle(R.string.cd_settings)
        return dialog
    }

    override fun onCreateView(
        inflater: LayoutInflater,
        container: ViewGroup?,
        savedInstanceState: Bundle?,
    ): View = inflater.inflate(R.layout.fragment_settings, container, false)

    override fun onViewCreated(view: View, savedInstanceState: Bundle?) {
        listener = activity as? Listener

        val tunnelModeItem = view.findViewById<TextView>(R.id.tunnelModeItem)
        val splitTunnelingItem = view.findViewById<TextView>(R.id.splitTunnelingItem)
        val splitTunnelingAppsItem = view.findViewById<TextView>(R.id.splitTunnelingAppsItem)
        val proxyItem = view.findViewById<TextView>(R.id.proxyItem)
        val autoclickItem = view.findViewById<TextView>(R.id.autoclickItem)
        val headlessCheckbox = view.findViewById<CheckBox>(R.id.headlessCheckbox)
        val reconnectCheckbox = view.findViewById<CheckBox>(R.id.reconnectOnStartCheckbox)
        val showLogsCheckbox = view.findViewById<CheckBox>(R.id.showLogsCheckbox)
        val shareLogsItem = view.findViewById<TextView>(R.id.shareLogsItem)
        val resetItem = view.findViewById<TextView>(R.id.resetItem)
        val closeButton = view.findViewById<Button>(R.id.closeButton)

        updateTunnelModeLabel(tunnelModeItem)
        updateSplitTunnelingLabel(splitTunnelingItem)
        updateSplitTunnelingAppsEnabled(splitTunnelingAppsItem)

        headlessCheckbox.isChecked = Prefs.headless
        reconnectCheckbox.isChecked = Prefs.connectOnStart
        showLogsCheckbox.isChecked = Prefs.showLogs

        tunnelModeItem.setOnClickListener {
            showModeDialog(tunnelModeItem)
        }

        splitTunnelingItem.setOnClickListener {
            showSplitTunnelingDialog(splitTunnelingItem, splitTunnelingAppsItem)
        }

        splitTunnelingAppsItem.setOnClickListener {
            if (splitTunnelingMode != SplitTunnelingMode.NONE) {
                showSplitTunnelingAppSelection()
            }
        }

        proxyItem.setOnClickListener {
            ProxySettingsDialogFragment {
                listener?.onReset()
                dismiss()
            }.show(childFragmentManager, ProxySettingsDialogFragment.TAG)
        }

        autoclickItem.setOnClickListener {
            showAutoclickSettingsDialog()
        }

        headlessCheckbox.setOnCheckedChangeListener { _, checked ->
            Prefs.headless = checked
            listener?.onReset()
        }

        reconnectCheckbox.setOnCheckedChangeListener { _, checked ->
            Prefs.connectOnStart = checked
        }

        showLogsCheckbox.setOnCheckedChangeListener { _, checked ->
            Prefs.showLogs = checked
            listener?.onShowLogsChanged(checked)
        }

        shareLogsItem.setOnClickListener {
            listener?.onShareLogs()
            dismiss()
        }

        resetItem.setOnClickListener {
            listener?.onReset()
            dismiss()
        }

        closeButton.setOnClickListener {
            dismiss()
        }
    }

    private fun updateTunnelModeLabel(textView: TextView) {
        textView.text = getString(R.string.menu_tunnel, tunnelMode.label)
    }

    private fun updateSplitTunnelingLabel(textView: TextView) {
        textView.text = getString(R.string.menu_split_tunneling, splitTunnelingMode.label)
    }

    private fun updateSplitTunnelingAppsEnabled(textView: TextView) {
        val enabled = splitTunnelingMode != SplitTunnelingMode.NONE
        textView.isEnabled = enabled
        textView.alpha = if (enabled) 1.0f else 0.4f
    }

    private fun showModeDialog(tunnelModeItem: TextView) {
        val modes = TunnelMode.entries
        val labels = modes.map { it.label }.toTypedArray()
        val current = modes.indexOf(tunnelMode)
        AlertDialog.Builder(requireContext())
            .setSingleChoiceItems(labels, current) { dialog, which ->
                dialog.dismiss()
                val mode = modes[which]
                if (mode != tunnelMode) {
                    tunnelMode = mode
                    Prefs.tunnelMode = mode
                    updateTunnelModeLabel(tunnelModeItem)
                    listener?.onTunnelModeChanged(mode)
                }
            }
            .show()
    }

    private fun showSplitTunnelingDialog(
        splitTunnelingItem: TextView,
        splitTunnelingAppsItem: TextView,
    ) {
        val modes = SplitTunnelingMode.entries.toTypedArray()
        val labels = modes.map { it.label }.toTypedArray()
        val selectedIndex = modes.indexOf(splitTunnelingMode)

        AlertDialog.Builder(requireContext())
            .setTitle(R.string.split_tunneling_mode_prompt)
            .setSingleChoiceItems(labels, selectedIndex) { dialog, which ->
                splitTunnelingMode = modes[which]
                Prefs.splitTunnelingMode = splitTunnelingMode
                updateSplitTunnelingLabel(splitTunnelingItem)
                updateSplitTunnelingAppsEnabled(splitTunnelingAppsItem)
                dialog.dismiss()
                if (TunnelVpnService.instance?.isRunning == true) {
                    Toast.makeText(requireContext(), R.string.split_tunneling_mode_changed, Toast.LENGTH_SHORT).show()
                }
            }
            .setNegativeButton(android.R.string.cancel, null)
            .show()
    }

    private fun showSplitTunnelingAppSelection() {
        val dialogLayout = layoutInflater.inflate(R.layout.split_tunneling_app_list_dialog, null)
        val loadingProgressBar = dialogLayout.findViewById<ProgressBar>(R.id.loading_progress_bar)
        val appListContainer = dialogLayout.findViewById<LinearLayout>(R.id.app_list_container)
        val searchEditText = dialogLayout.findViewById<EditText>(R.id.search_input)
        val systemAppsCheckbox = dialogLayout.findViewById<CheckBox>(R.id.system_apps_checkbox)
        val listView = dialogLayout.findViewById<ListView>(R.id.app_list_view)

        AlertDialog.Builder(requireContext())
            .setTitle(R.string.split_tunneling_apps_prompt)
            .setView(dialogLayout)
            .setPositiveButton(android.R.string.ok) { _, _ ->
                Prefs.splitTunnelingMode = splitTunnelingMode
                Prefs.splitTunnelingPackages = splitTunnelingPackages
                if (TunnelVpnService.instance?.isRunning == true) {
                    Toast.makeText(requireContext(), R.string.split_tunneling_mode_changed, Toast.LENGTH_SHORT).show()
                }
            }
            .setNegativeButton(android.R.string.cancel, null)
            .show()

        loadingProgressBar.visibility = View.VISIBLE
        appListContainer.visibility = View.GONE

        Thread {
            var includeSystemApps = false
            val context = requireContext()
            val pm = context.packageManager
            val ownPackage = context.packageName

            val installedApps = pm.getInstalledApplications(PackageManager.GET_META_DATA)
                .filter { it.packageName != ownPackage }
                .mapNotNull { appInfo ->
                    val pkg = appInfo.packageName
                    if (pkg.isBlank()) return@mapNotNull null
                    val label = appInfo.loadLabel(pm).toString().takeIf { it.isNotBlank() } ?: pkg
                    SplitTunnelingAppItem(
                        pkg, label, pm.getApplicationIcon(pkg),
                        splitTunnelingPackages.contains(pkg),
                        (appInfo.flags and ApplicationInfo.FLAG_SYSTEM) == 0,
                    )
                }
                .distinctBy { it.packageName }
                .sortedWith(compareByDescending<SplitTunnelingAppItem> { it.isSelected }.thenBy { it.label.lowercase() })

            activity?.runOnUiThread {
                loadingProgressBar.visibility = View.GONE
                appListContainer.visibility = View.VISIBLE

                fun buildAppList(query: String, includeSystemApps: Boolean): List<SplitTunnelingAppItem> {
                    val baseList = installedApps.filter { includeSystemApps || it.isUserApp }
                    return if (query.isBlank()) {
                        baseList
                    } else {
                        baseList.filter {
                            it.label.contains(query, ignoreCase = true) ||
                            it.packageName.contains(query, ignoreCase = true)
                        }
                    }
                }

                val adapter = SplitTunnelingAdapter(layoutInflater, splitTunnelingPackages)
                adapter.items = buildAppList("", includeSystemApps)

                if (adapter.items.isEmpty()) return@runOnUiThread

                listView.choiceMode = ListView.CHOICE_MODE_MULTIPLE
                listView.adapter = adapter

                systemAppsCheckbox.isChecked = includeSystemApps
                systemAppsCheckbox.setOnCheckedChangeListener { _, checked ->
                    includeSystemApps = checked
                    adapter.items = buildAppList(searchEditText.text.toString(), includeSystemApps)
                }

                searchEditText.addTextChangedListener(object : TextWatcher {
                    override fun beforeTextChanged(s: CharSequence?, start: Int, count: Int, after: Int) {}
                    override fun onTextChanged(s: CharSequence?, start: Int, before: Int, count: Int) {
                        adapter.items = buildAppList(s.toString(), includeSystemApps)
                    }
                    override fun afterTextChanged(s: Editable?) {}
                })
            }
        }.start()
    }

    private fun showAutoclickSettingsDialog() {
        val dialogLayout = layoutInflater.inflate(R.layout.autoclick_settings_dialog, null)
        val autoclickCheckbox = dialogLayout.findViewById<CheckBox>(R.id.autoclick_checkbox)
        val nameInput = dialogLayout.findViewById<EditText>(R.id.autoclick_name_input)
        val generateButton = dialogLayout.findViewById<Button>(R.id.autoclick_generate_random_button)

        autoclickCheckbox.isChecked = Prefs.autoclickEnabled
        nameInput.setText(Prefs.autoclickName)

        generateButton.setOnClickListener {
            val names = requireContext().assets.open("names.txt").bufferedReader().readLines()
            val randomName = names.random()
            nameInput.setText(randomName)
        }

        AlertDialog.Builder(requireContext())
            .setTitle(R.string.autoclick_settings_title)
            .setView(dialogLayout)
            .setPositiveButton(android.R.string.ok) { _, _ ->
                Prefs.autoclickEnabled = autoclickCheckbox.isChecked
                Prefs.autoclickName = nameInput.text.toString()
            }
            .setNegativeButton(android.R.string.cancel, null)
            .show()
    }

    companion object {
        const val TAG = "SettingsDialogFragment"
    }
}
