package bypass.whitelist

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Intent
import android.net.VpnService
import android.os.Build
import android.os.ParcelFileDescriptor
import android.util.Log
import mobile.Mobile

enum class VpnStatus(val label: String) {
    STARTING("Starting..."),
    CONNECTING("Connecting..."),
    CALL_CONNECTED("Call connected"),
    DATACHANNEL_OPEN("DataChannel open"),
    DATACHANNEL_LOST("DataChannel lost"),
    TUNNEL_ACTIVE("Tunnel active"),
    TUNNEL_LOST("Tunnel lost, reconnecting..."),
    CALL_DISCONNECTED("Call disconnected"),
    CALL_FAILED("Call failed")
}

class TunnelVpnService : VpnService() {

    companion object {
        const val TAG = "TunnelVPN"
        const val SOCKS_PORT = 1080
        const val MTU = 1500
        const val CHANNEL_ID = "vpn_channel"
        const val NOTIFICATION_ID = 1
        const val ACTION_STOP = "bypass.whitelist.STOP_VPN"
        var isRunning = false
        var instance: TunnelVpnService? = null
    }

    private var vpnFd: ParcelFileDescriptor? = null

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
        nm.notify(NOTIFICATION_ID, buildNotification(status.label))
    }

    private fun startForegroundNotification() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            val channel = NotificationChannel(
                CHANNEL_ID, "VPN Tunnel", NotificationManager.IMPORTANCE_DEFAULT
            )
            val nm = getSystemService(NotificationManager::class.java)
            nm.createNotificationChannel(channel)
        }

        startForeground(NOTIFICATION_ID, buildNotification(VpnStatus.STARTING.label))
    }

    private fun start() {
        if (isRunning) return

        startForegroundNotification()

        val builder = Builder()
            .setSession("WhitelistBypass")
            .addAddress("10.0.0.2", 32)
            .addRoute("0.0.0.0", 0)
            .addDnsServer("8.8.8.8")
            .addDnsServer("8.8.4.4")
            .setMtu(MTU)

        try {
            builder.addDisallowedApplication(packageName)
        } catch (e: Exception) {
            Log.e(TAG, "Cannot exclude self: ${e.message}")
        }

        vpnFd = builder.establish()
        if (vpnFd == null) {
            Log.e(TAG, "Failed to establish VPN")
            return
        }

        isRunning = true
        val fd = vpnFd!!.detachFd()
        vpnFd = null
        Log.i(TAG, "VPN established, fd=$fd")
        updateStatus(VpnStatus.TUNNEL_ACTIVE)

        Thread {
            try {
                Mobile.startTun2Socks(fd.toLong(), MTU.toLong(), SOCKS_PORT.toLong())
            } catch (e: Exception) {
                Log.e(TAG, "tun2socks error: ${e.message}")
                isRunning = false
            }
        }.start()
    }

    private fun stop() {
        isRunning = false
        try {
            Mobile.stopTun2Socks()
        } catch (e: Exception) {
            Log.e(TAG, "tun2socks stop error: ${e.message}")
        }
        vpnFd = null
        @Suppress("DEPRECATION")
        stopForeground(true)
        stopSelf()
    }

    private fun buildNotification(text: String): Notification {
        val openIntent = Intent(this, bypass.whitelist.MainActivity::class.java).apply {
            flags = Intent.FLAG_ACTIVITY_SINGLE_TOP or Intent.FLAG_ACTIVITY_CLEAR_TOP
        }
        val openPending = PendingIntent.getActivity(
            this, 1, openIntent, PendingIntent.FLAG_IMMUTABLE
        )
        val stopIntent = Intent(this, TunnelVpnService::class.java).apply {
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
            .setContentTitle("VPN active")
            .setContentText(text)
            .setSmallIcon(android.R.drawable.ic_lock_lock)
            .setOngoing(true)
            .setContentIntent(openPending)
            .addAction(Notification.Action.Builder(null, "Disconnect", stopPending).build())
            .build()
    }
}
