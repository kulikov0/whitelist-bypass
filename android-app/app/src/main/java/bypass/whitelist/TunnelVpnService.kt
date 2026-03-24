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
            // Route all public IPs through VPN, exclude private RFC1918 ranges
            // 0.0.0.0/5, 8.0.0.0/7 ... split around 10/8, 172.16/12, 192.168/16
            .addRoute("0.0.0.0", 5)      // 0.0.0.0 - 7.255.255.255
            .addRoute("8.0.0.0", 7)      // 8.0.0.0 - 9.255.255.255
            // skip 10.0.0.0/8 (private)
            .addRoute("11.0.0.0", 8)
            .addRoute("12.0.0.0", 6)
            .addRoute("16.0.0.0", 4)
            .addRoute("32.0.0.0", 3)
            .addRoute("64.0.0.0", 2)
            .addRoute("128.0.0.0", 3)
            .addRoute("160.0.0.0", 5)
            .addRoute("168.0.0.0", 8)
            .addRoute("169.0.0.0", 9)
            .addRoute("169.128.0.0", 10)
            .addRoute("169.192.0.0", 11)
            .addRoute("169.240.0.0", 12)
            .addRoute("169.252.0.0", 14)
            // skip 169.254.0.0/16 (link-local)
            .addRoute("169.255.0.0", 16)
            .addRoute("170.0.0.0", 7)
            .addRoute("172.0.0.0", 12)
            // skip 172.16.0.0/12 (private)
            .addRoute("172.32.0.0", 11)
            .addRoute("172.64.0.0", 10)
            .addRoute("172.128.0.0", 9)
            .addRoute("173.0.0.0", 8)
            .addRoute("174.0.0.0", 7)
            .addRoute("176.0.0.0", 4)
            .addRoute("192.0.0.0", 9)
            .addRoute("192.128.0.0", 11)
            .addRoute("192.160.0.0", 13)
            // skip 192.168.0.0/16 (private)
            .addRoute("192.169.0.0", 16)
            .addRoute("192.170.0.0", 15)
            .addRoute("192.172.0.0", 14)
            .addRoute("192.176.0.0", 12)
            .addRoute("192.192.0.0", 10)
            .addRoute("193.0.0.0", 8)
            .addRoute("194.0.0.0", 7)
            .addRoute("196.0.0.0", 6)
            .addRoute("200.0.0.0", 5)
            .addRoute("208.0.0.0", 4)
            .addRoute("224.0.0.0", 3)
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
