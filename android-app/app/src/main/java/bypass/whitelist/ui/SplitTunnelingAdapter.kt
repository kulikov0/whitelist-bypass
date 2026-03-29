package bypass.whitelist.ui

import android.content.res.Configuration
import android.graphics.Color
import android.graphics.drawable.Drawable
import android.view.LayoutInflater
import android.view.View
import android.view.ViewGroup
import android.widget.BaseAdapter
import android.widget.CheckBox
import android.widget.ImageView
import android.widget.TextView
import bypass.whitelist.R

data class SplitTunnelingAppItem(
    val packageName: String,
    val label: String,
    val icon: Drawable,
    var isSelected: Boolean = false,
    val isUserApp: Boolean = false,
)

class SplitTunnelingAdapter(
    private val inflater: LayoutInflater,
    private val selectedPackages: MutableSet<String>,
) : BaseAdapter() {

    var items: List<SplitTunnelingAppItem> = emptyList()
        set(value) {
            field = value
            notifyDataSetChanged()
        }

    override fun getCount() = items.size
    override fun getItem(position: Int) = items[position]
    override fun getItemId(position: Int) = position.toLong()

    override fun getView(position: Int, convertView: View?, parent: ViewGroup): View {
        val item = getItem(position)
        val view = convertView ?: inflater.inflate(R.layout.split_tunneling_app_list_item, parent, false)
        val iconView = view.findViewById<ImageView>(R.id.appIcon)
        val labelView = view.findViewById<TextView>(R.id.appLabel)
        val packageView = view.findViewById<TextView>(R.id.appPackage)
        val checkbox = view.findViewById<CheckBox>(R.id.appCheckbox)

        iconView.setImageDrawable(item.icon)
        labelView.text = item.label
        val isDark = (view.resources.configuration.uiMode and Configuration.UI_MODE_NIGHT_MASK) == Configuration.UI_MODE_NIGHT_YES
        labelView.setTextColor(if (isDark) Color.WHITE else Color.BLACK)
        packageView.text = item.packageName

        val toggle = { checked: Boolean ->
            item.isSelected = checked
            if (checked) selectedPackages.add(item.packageName)
            else selectedPackages.remove(item.packageName)
        }

        view.setOnClickListener {
            val newState = !checkbox.isChecked
            checkbox.isChecked = newState
            toggle(newState)
        }
        checkbox.setOnCheckedChangeListener { _, isChecked -> toggle(isChecked) }
        checkbox.isChecked = item.isSelected

        return view
    }
}
