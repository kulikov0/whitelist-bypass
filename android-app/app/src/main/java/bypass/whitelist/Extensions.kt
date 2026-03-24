package bypass.whitelist

import android.app.Activity
import android.view.inputmethod.InputMethodManager

fun Activity.hideKeyboard() {
    val imm = getSystemService(InputMethodManager::class.java)
    currentFocus?.let { imm.hideSoftInputFromWindow(it.windowToken, 0) }
}