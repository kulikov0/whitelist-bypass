package bypass.whitelist.ui

import android.content.pm.ApplicationInfo
import android.content.pm.PackageManager
import android.view.View
import android.widget.CheckBox
import android.widget.LinearLayout
import android.widget.ListView
import android.widget.PopupMenu
import android.widget.EditText
import android.widget.Button
import android.widget.ProgressBar
import android.widget.Toast
import androidx.appcompat.app.AlertDialog
import androidx.appcompat.app.AppCompatActivity
import bypass.whitelist.R
import bypass.whitelist.tunnel.SplitTunnelingMode
import bypass.whitelist.tunnel.TunnelMode
import bypass.whitelist.tunnel.TunnelVpnService
import bypass.whitelist.util.Callback
import bypass.whitelist.util.ParamCallback
import bypass.whitelist.util.Prefs

class SettingsMenuController(
    private val activity: AppCompatActivity,
    private val onModeChanged: ParamCallback<TunnelMode>,
    private val onShareLogs: Callback,
    private val onReset: Callback,
) {
    var tunnelMode: TunnelMode = Prefs.tunnelMode
    var splitTunnelingMode: SplitTunnelingMode = Prefs.splitTunnelingMode
    var splitTunnelingPackages: MutableSet<String> = Prefs.splitTunnelingPackages.toMutableSet()
    var showLogs: Boolean = Prefs.showLogs

    var onShowLogsChanged: ParamCallback<Boolean> = {}

    private enum class MenuItem(val id: Int, val stringRes: Int) {
        MODE(99, R.string.menu_tunnel),
        SPLIT_TUNNELING(98, R.string.menu_split_tunneling),
        SPLIT_TUNNELING_APPS(97, R.string.menu_split_tunneling_manage),
        AUTOCLICK_SETTINGS(104, R.string.menu_autoclick_settings),
        RECONNECT_ON_START(100, R.string.menu_reconnect_on_start),
        SHOW_LOGS(101, R.string.menu_show_logs),
        SHARE_LOGS(102, R.string.menu_share_logs),
        RESET(200, R.string.menu_reset),
    }

    fun show(anchor: View) {
        val popup = PopupMenu(activity, anchor)
        val menu = popup.menu

        menu.add(0, MenuItem.MODE.id, 0, activity.getString(MenuItem.MODE.stringRes, tunnelMode.label))
        menu.add(0, MenuItem.SPLIT_TUNNELING.id, 0, activity.getString(MenuItem.SPLIT_TUNNELING.stringRes, splitTunnelingMode.label))
        menu.add(0, MenuItem.SPLIT_TUNNELING_APPS.id, 0, MenuItem.SPLIT_TUNNELING_APPS.stringRes).apply {
            isEnabled = splitTunnelingMode != SplitTunnelingMode.NONE
        }
        menu.add(0, MenuItem.AUTOCLICK_SETTINGS.id, 0, activity.getString(MenuItem.AUTOCLICK_SETTINGS.stringRes))
        menu.add(0, MenuItem.RECONNECT_ON_START.id, 0, MenuItem.RECONNECT_ON_START.stringRes).apply {
            isCheckable = true
            isChecked = Prefs.connectOnStart
        }
        menu.add(0, MenuItem.SHOW_LOGS.id, 0, MenuItem.SHOW_LOGS.stringRes).apply {
            isCheckable = true
            isChecked = showLogs
        }
        menu.add(0, MenuItem.SHARE_LOGS.id, 0, MenuItem.SHARE_LOGS.stringRes)
        menu.add(0, MenuItem.RESET.id, 0, MenuItem.RESET.stringRes)

        popup.setOnMenuItemClickListener { item ->
            when (item.itemId) {
                MenuItem.RECONNECT_ON_START.id -> {
                    Prefs.connectOnStart = !item.isChecked
                    true
                }
                MenuItem.SPLIT_TUNNELING.id -> {
                    showSplitTunnelingDialog()
                    true
                }
                MenuItem.SPLIT_TUNNELING_APPS.id -> {
                    showSplitTunnelingAppSelection()
                    true
                }
                MenuItem.SHOW_LOGS.id -> {
                    showLogs = !item.isChecked
                    Prefs.showLogs = showLogs
                    onShowLogsChanged(showLogs)
                    true
                }
                MenuItem.SHARE_LOGS.id -> {
                    onShareLogs()
                    true
                }
                MenuItem.AUTOCLICK_SETTINGS.id -> {
                    showAutoclickSettingsDialog()
                    true
                }
                MenuItem.RESET.id -> {
                    onReset()
                    true
                }
                MenuItem.MODE.id -> {
                    showModeDialog()
                    true
                }
                else -> false
            }
        }
        popup.show()
    }

    private fun showModeDialog() {
        val modes = TunnelMode.entries
        val labels = modes.map { it.label }.toTypedArray()
        val current = modes.indexOf(tunnelMode)
        AlertDialog.Builder(activity)
            .setSingleChoiceItems(labels, current) { dialog, which ->
                dialog.dismiss()
                val mode = modes[which]
                if (mode != tunnelMode) {
                    tunnelMode = mode
                    Prefs.tunnelMode = mode
                    onModeChanged(mode)
                }
            }
            .show()
    }

    private fun showSplitTunnelingDialog() {
        val modes = SplitTunnelingMode.entries.toTypedArray()
        val labels = modes.map { it.label }.toTypedArray()
        val selectedIndex = modes.indexOf(splitTunnelingMode)

        AlertDialog.Builder(activity)
            .setTitle(R.string.split_tunneling_mode_prompt)
            .setSingleChoiceItems(labels, selectedIndex) { dialog, which ->
                splitTunnelingMode = modes[which]
                Prefs.splitTunnelingMode = splitTunnelingMode
                dialog.dismiss()
                if (TunnelVpnService.instance?.isRunning == true) {
                    Toast.makeText(activity, R.string.split_tunneling_mode_changed, Toast.LENGTH_SHORT).show()
                }
            }
            .setNegativeButton(android.R.string.cancel, null)
            .show()
    }

    private fun showSplitTunnelingAppSelection() {
        val dialogLayout = activity.layoutInflater.inflate(R.layout.split_tunneling_app_list_dialog, null)
        val loadingProgressBar = dialogLayout.findViewById<ProgressBar>(R.id.loading_progress_bar)
        val appListContainer = dialogLayout.findViewById<LinearLayout>(R.id.app_list_container)
        val searchEditText = dialogLayout.findViewById<EditText>(R.id.search_input)
        val systemAppsCheckbox = dialogLayout.findViewById<CheckBox>(R.id.system_apps_checkbox)
        val listView = dialogLayout.findViewById<ListView>(R.id.app_list_view)

        val dialog = AlertDialog.Builder(activity)
            .setTitle(R.string.split_tunneling_apps_prompt)
            .setView(dialogLayout)
            .setPositiveButton(android.R.string.ok) { _, _ ->
                Prefs.splitTunnelingMode = splitTunnelingMode
                Prefs.splitTunnelingPackages = splitTunnelingPackages
                if (TunnelVpnService.instance?.isRunning == true) {
                    Toast.makeText(activity, R.string.split_tunneling_mode_changed, Toast.LENGTH_SHORT).show()
                }
            }
            .setNegativeButton(android.R.string.cancel, null)
            .show()

        loadingProgressBar.visibility = View.VISIBLE
        appListContainer.visibility = View.GONE

        Thread {
            var includeSystemApps = false
            val pm = activity.packageManager

            val installedApps = pm.getInstalledApplications(PackageManager.GET_META_DATA)
                .filter { it.packageName != activity.packageName }
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

            activity.runOnUiThread {
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

                val adapter = SplitTunnelingAdapter(activity.layoutInflater, splitTunnelingPackages)
                adapter.items = buildAppList("", includeSystemApps)

                if (adapter.items.isEmpty()) return@runOnUiThread

                listView.choiceMode = ListView.CHOICE_MODE_MULTIPLE
                listView.adapter = adapter

                systemAppsCheckbox.isChecked = includeSystemApps
                systemAppsCheckbox.setOnCheckedChangeListener { _, checked ->
                    includeSystemApps = checked
                    adapter.items = buildAppList(searchEditText.text.toString(), includeSystemApps)
                }

                searchEditText.addTextChangedListener(object : android.text.TextWatcher {
                    override fun beforeTextChanged(s: CharSequence?, start: Int, count: Int, after: Int) {}
                    override fun onTextChanged(s: CharSequence?, start: Int, before: Int, count: Int) {
                        adapter.items = buildAppList(s.toString(), includeSystemApps)
                    }
                    override fun afterTextChanged(s: android.text.Editable?) {}
                })
            }
        }.start()
    }

    private fun showAutoclickSettingsDialog() {
        val dialogLayout = activity.layoutInflater.inflate(R.layout.autoclick_settings_dialog, null)
        var autoclickCheckbox = dialogLayout.findViewById<CheckBox>(R.id.autoclick_checkbox)
        val nameInput = dialogLayout.findViewById<EditText>(R.id.autoclick_name_input)
        val generateButton = dialogLayout.findViewById<Button>(R.id.autoclick_generate_random_button)

        autoclickCheckbox.isChecked = Prefs.autoclickEnabled
        nameInput.setText(Prefs.autoclickName)

        generateButton.setOnClickListener {
            val names = activity.assets.open("names.txt").bufferedReader().readLines()
            val randomName = names.random()
            nameInput.setText(randomName)
        }

        AlertDialog.Builder(activity)
            .setTitle(R.string.autoclick_settings_title)
            .setView(dialogLayout)
            .setPositiveButton(android.R.string.ok) { _, _ ->
                Prefs.autoclickEnabled = autoclickCheckbox.isChecked
                Prefs.autoclickName = nameInput.text.toString()
            }
            .setNegativeButton(android.R.string.cancel, null)
            .show()
    }
}
