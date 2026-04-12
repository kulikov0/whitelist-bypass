package bypass.whitelist.ui

import android.app.Dialog
import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import android.os.Bundle
import android.view.View
import android.widget.Button
import android.widget.CheckBox
import android.widget.EditText
import android.widget.RadioButton
import android.widget.RadioGroup
import android.widget.Toast
import androidx.appcompat.app.AlertDialog
import androidx.fragment.app.DialogFragment
import bypass.whitelist.R
import bypass.whitelist.util.Callback
import bypass.whitelist.util.Prefs
import bypass.whitelist.util.SocksAuthMode

class ProxySettingsDialogFragment(
    private val onChanged: Callback? = null,
) : DialogFragment() {

    override fun onCreateDialog(savedInstanceState: Bundle?): Dialog {
        val view = layoutInflater.inflate(R.layout.dialog_proxy_settings, null)

        val portInput = view.findViewById<EditText>(R.id.proxyPortInput)
        val authModeGroup = view.findViewById<RadioGroup>(R.id.authModeGroup)
        val authModeAuto = view.findViewById<RadioButton>(R.id.authModeAuto)
        val authModeManual = view.findViewById<RadioButton>(R.id.authModeManual)
        val manualAuthContainer = view.findViewById<View>(R.id.manualAuthContainer)
        val userInput = view.findViewById<EditText>(R.id.proxyUserInput)
        val passInput = view.findViewById<EditText>(R.id.proxyPassInput)
        val copyButton = view.findViewById<Button>(R.id.copyProxyButton)
        val proxyOnlyCheckbox = view.findViewById<CheckBox>(R.id.proxyOnlyCheckbox)

        portInput.setText(Prefs.socksPort.toString())
        proxyOnlyCheckbox.isChecked = Prefs.proxyOnly

        when (Prefs.socksAuthMode) {
            SocksAuthMode.AUTO -> authModeAuto.isChecked = true
            SocksAuthMode.MANUAL -> authModeManual.isChecked = true
        }
        manualAuthContainer.visibility = if (Prefs.socksAuthMode == SocksAuthMode.MANUAL) View.VISIBLE else View.GONE
        userInput.setText(Prefs.socksUser)
        passInput.setText(Prefs.socksPass)

        authModeGroup.setOnCheckedChangeListener { _, checkedId ->
            manualAuthContainer.visibility = if (checkedId == R.id.authModeManual) View.VISIBLE else View.GONE
        }

        copyButton.setOnClickListener {
            val port = portInput.text.toString()
            val user = userInput.text.toString()
            val pass = passInput.text.toString()
            val proxyUrl = "socks5://$user:$pass@127.0.0.1:$port"
            val clipboard = requireContext().getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager
            clipboard.setPrimaryClip(ClipData.newPlainText("proxy", proxyUrl))
            Toast.makeText(requireContext(), R.string.proxy_copied, Toast.LENGTH_SHORT).show()
        }

        return AlertDialog.Builder(requireContext())
            .setTitle(R.string.proxy_settings_title)
            .setView(view)
            .setPositiveButton(android.R.string.ok) { _, _ ->
                val port = portInput.text.toString().toLongOrNull()
                if (port != null && port in 1..65535) {
                    Prefs.socksPort = port
                }
                Prefs.socksAuthMode = if (authModeManual.isChecked) SocksAuthMode.MANUAL else SocksAuthMode.AUTO
                if (authModeManual.isChecked) {
                    Prefs.socksUser = userInput.text.toString()
                    Prefs.socksPass = passInput.text.toString()
                }
                Prefs.proxyOnly = proxyOnlyCheckbox.isChecked
                onChanged?.invoke()
            }
            .setNegativeButton(android.R.string.cancel, null)
            .create()
    }

    companion object {
        const val TAG = "ProxySettingsDialog"
    }
}
