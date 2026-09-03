package bypass.whitelist.util

import android.annotation.SuppressLint
import android.app.Activity
import android.content.ActivityNotFoundException
import android.content.Context
import android.content.Intent
import android.os.Build
import android.os.PowerManager
import android.provider.Settings
import androidx.core.net.toUri

object BackgroundRestrictionsManager {

    fun requestDisableBatteryOptimization(activity: Activity): Boolean {
        return when {
            isXiaomiDevice() -> openXiaomiBatterySettings(activity)
            else -> requestStandardAndroidOptimization(activity)
        }
    }

    fun isIgnoringBatteryOptimizations(context: Context): Boolean {
        return if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.M) {
            (context.getSystemService(Context.POWER_SERVICE) as PowerManager)
                .isIgnoringBatteryOptimizations(context.packageName)
        } else {
            true
        }
    }

    private fun isXiaomiDevice(): Boolean {
        return Build.MANUFACTURER.lowercase() in setOf("xiaomi", "redmi", "poco")
    }

    @SuppressLint("BatteryLife")
    fun requestStandardAndroidOptimization(activity: Activity): Boolean {
        try {
            val intent = Intent().apply {
                action = Settings.ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS
                data = "package:${activity.packageName}".toUri()
            }
            activity.startActivity(intent)
            return true
        } catch (e: Exception) {
            val intent = Intent(Settings.ACTION_APPLICATION_DETAILS_SETTINGS).apply {
                data = "package:${activity.packageName}".toUri()
            }
            activity.startActivity(intent)
            return true
        }
    }

    fun openXiaomiBatterySettings(activity: Activity): Boolean {
        try {
            val intent = Intent().apply {
                setClassName(
                    "com.miui.powerkeeper",
                    "com.miui.powerkeeper.ui.HiddenAppsConfigActivity"
                )
                putExtra("package_name", activity.packageName)
            }
            activity.startActivity(intent)
        } catch (e: ActivityNotFoundException) {
            return requestStandardAndroidOptimization(activity)
        }
        return true
    }

}