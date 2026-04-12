package bypass.whitelist.tunnel

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Intent
import android.os.Build
import android.os.IBinder
import bypass.whitelist.MainActivity
import bypass.whitelist.util.Callback
import bypass.whitelist.R

class ProxyService : Service() {

    companion object {
        const val CHANNEL_ID = "proxy_channel"
        const val NOTIFICATION_ID = 2
        const val ACTION_STOP = "bypass.whitelist.STOP_PROXY"
        @Volatile var instance: ProxyService? = null
        @Volatile var onDisconnect: Callback? = null
    }

    @Volatile var isRunning = false
        private set

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onCreate() {
        super.onCreate()
        instance = this
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        if (intent?.action == ACTION_STOP) {
            stop()
            return START_NOT_STICKY
        }
        start()
        return START_STICKY
    }

    override fun onDestroy() {
        stop()
        super.onDestroy()
    }

    fun updateStatus(status: VpnStatus) {
        val nm = getSystemService(NotificationManager::class.java)
        nm.notify(NOTIFICATION_ID, buildNotification(getString(status.labelRes)))
    }

    @Synchronized
    fun stop() {
        if (!isRunning) return
        isRunning = false
        @Suppress("DEPRECATION")
        stopForeground(true)
        stopSelf()
        onDisconnect?.invoke()
    }

    private fun start() {
        if (isRunning) return
        isRunning = true

        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            val channel = NotificationChannel(
                CHANNEL_ID, "Proxy Tunnel", NotificationManager.IMPORTANCE_LOW
            )
            val nm = getSystemService(NotificationManager::class.java)
            nm.createNotificationChannel(channel)
        }

        startForeground(NOTIFICATION_ID, buildNotification(getString(R.string.notification_proxy_title)))
    }

    private fun buildNotification(text: String): Notification {
        val openIntent = Intent(this, MainActivity::class.java).apply {
            flags = Intent.FLAG_ACTIVITY_SINGLE_TOP or Intent.FLAG_ACTIVITY_CLEAR_TOP
        }
        val openPending = PendingIntent.getActivity(
            this, 1, openIntent, PendingIntent.FLAG_IMMUTABLE
        )
        val stopIntent = Intent(this, ProxyService::class.java).apply {
            action = ACTION_STOP
        }
        val stopPending = PendingIntent.getService(
            this, 0, stopIntent, PendingIntent.FLAG_IMMUTABLE
        )
        @Suppress("DEPRECATION")
        val builder = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            Notification.Builder(this, CHANNEL_ID)
        } else {
            Notification.Builder(this)
        }
        return builder
            .setContentTitle(getString(R.string.notification_proxy_title))
            .setContentText(text)
            .setSmallIcon(android.R.drawable.ic_lock_lock)
            .setOngoing(true)
            .setContentIntent(openPending)
            .addAction(Notification.Action.Builder(null, getString(R.string.notification_disconnect), stopPending).build())
            .build()
    }
}
