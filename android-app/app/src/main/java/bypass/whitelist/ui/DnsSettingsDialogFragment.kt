package bypass.whitelist.ui

import android.app.Dialog
import android.os.Bundle
import android.view.View
import android.widget.EditText
import android.widget.RadioButton
import android.widget.RadioGroup
import androidx.appcompat.app.AlertDialog
import androidx.fragment.app.DialogFragment
import bypass.whitelist.R
import bypass.whitelist.util.DnsMode
import bypass.whitelist.util.Prefs

class DnsSettingsDialogFragment : DialogFragment() {

    override fun onCreateDialog(savedInstanceState: Bundle?): Dialog {
        val view = layoutInflater.inflate(R.layout.dialog_dns_settings, null)

        val dnsModeGroup = view.findViewById<RadioGroup>(R.id.dnsModeGroup)
        val dnsModeSystem = view.findViewById<RadioButton>(R.id.dnsModeSystem)
        val dnsModeCustom = view.findViewById<RadioButton>(R.id.dnsModeCustom)
        val customDnsContainer = view.findViewById<View>(R.id.customDnsContainer)
        val primaryInput = view.findViewById<EditText>(R.id.dnsPrimaryInput)
        val secondaryInput = view.findViewById<EditText>(R.id.dnsSecondaryInput)

        when (Prefs.dnsMode) {
            DnsMode.SYSTEM -> dnsModeSystem.isChecked = true
            DnsMode.CUSTOM -> dnsModeCustom.isChecked = true
        }
        customDnsContainer.visibility = if (Prefs.dnsMode == DnsMode.CUSTOM) View.VISIBLE else View.GONE
        primaryInput.setText(Prefs.dnsPrimary)
        secondaryInput.setText(Prefs.dnsSecondary)

        dnsModeGroup.setOnCheckedChangeListener { _, checkedId ->
            customDnsContainer.visibility = if (checkedId == R.id.dnsModeCustom) View.VISIBLE else View.GONE
        }

        return AlertDialog.Builder(requireContext())
            .setTitle(R.string.dns_settings_title)
            .setView(view)
            .setPositiveButton(android.R.string.ok) { _, _ ->
                Prefs.dnsMode = if (dnsModeCustom.isChecked) DnsMode.CUSTOM else DnsMode.SYSTEM
                if (dnsModeCustom.isChecked) {
                    Prefs.dnsPrimary = primaryInput.text.toString()
                    Prefs.dnsSecondary = secondaryInput.text.toString()
                }
            }
            .setNegativeButton(android.R.string.cancel, null)
            .create()
    }

    companion object {
        const val TAG = "DnsSettingsDialog"
    }
}
