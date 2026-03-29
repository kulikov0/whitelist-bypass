package bypass.whitelist

import android.app.Application
import bypass.whitelist.util.Prefs

class App : Application() {
    override fun onCreate() {
        super.onCreate()
        Prefs.init(this)
    }
}
