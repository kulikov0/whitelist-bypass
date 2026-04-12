package bypass.whitelist.tunnel

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Intent
import android.net.ConnectivityManager
import android.net.VpnService
import android.os.Build
import android.os.ParcelFileDescriptor
import android.util.Log
import bypass.whitelist.MainActivity
import bypass.whitelist.R
import bypass.whitelist.util.Callback
import bypass.whitelist.util.Prefs
import bypass.whitelist.util.SocksAuth
import bypass.whitelist.util.Vpn
import mobile.Mobile

class TunnelVpnService : VpnService() {

    companion object {
        const val TAG = "TunnelVPN"
        const val CHANNEL_ID = "vpn_channel"
        const val NOTIFICATION_ID = 1
        const val ACTION_STOP = "bypass.whitelist.STOP_VPN"
        @Volatile var instance: TunnelVpnService? = null
        @Volatile var onDisconnect: Callback? = null
    }

    @Volatile var isRunning: Boolean = false
    private var vpnFd: ParcelFileDescriptor? = null
    private var tun2socksThread: Thread? = null

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
        try {
            Mobile.stopTun2Socks()
        } catch (e: Exception) {
            Log.e(TAG, "tun2socks stop error: ${e.message}")
        }
        tun2socksThread?.join(3000)
        tun2socksThread = null
        vpnFd = null
        @Suppress("DEPRECATION")
        stopForeground(true)
        stopSelf()
        onDisconnect?.invoke()
    }

    private fun start() {
        if (isRunning) return

        startForegroundNotification()

        val builder = Builder()
            .setSession(Vpn.SESSION_NAME)
            .addAddress(Vpn.ADDRESS, Vpn.PREFIX_LENGTH)
            .addRoute(Vpn.ROUTE, 0)
            .setMtu(Vpn.MTU)

        val systemDns = getSystemDnsServers()
        if (systemDns.isNotEmpty()) {
            for (dns in systemDns) builder.addDnsServer(dns)
        } else {
            builder.addDnsServer(Vpn.DNS_PRIMARY)
            builder.addDnsServer(Vpn.DNS_SECONDARY)
        }

        try {
            when (Prefs.splitTunnelingMode) {
                SplitTunnelingMode.NONE -> {
                    builder.addDisallowedApplication(packageName)
                }
                SplitTunnelingMode.BYPASS -> {
                    builder.addDisallowedApplication(packageName)
                    Prefs.splitTunnelingPackages.forEach {
                        try {
                            builder.addDisallowedApplication(it)
                        } catch (ignored: Exception) {
                        }
                    }
                }
                SplitTunnelingMode.ONLY -> {
                    Prefs.splitTunnelingPackages.forEach {
                        try {
                            builder.addAllowedApplication(it)
                        } catch (ignored: Exception) {
                        }
                    }
                }
            }
        } catch (e: Exception) {
            Log.e(TAG, "Split tunneling failed: ${e.message}")
        }

        vpnFd = builder.establish()
        if (vpnFd == null) {
            Log.e(TAG, "Failed to establish VPN")
            return
        }

        isRunning = true
        val fd = vpnFd!!.detachFd()
        vpnFd = null
        Log.i(TAG, "VPN established, fd=$fd, SOCKS5 ${SocksAuth.user}:${SocksAuth.pass}@127.0.0.1:${Prefs.socksPort}")
        updateStatus(VpnStatus.TUNNEL_ACTIVE)

        tun2socksThread = Thread {
            try {
                Mobile.startTun2Socks(fd.toLong(), Vpn.MTU.toLong(), Prefs.socksPort, SocksAuth.user, SocksAuth.pass)
            } catch (e: Exception) {
                Log.e(TAG, "tun2socks error: ${e.message}")
                isRunning = false
            }
        }.also { it.start() }
    }

    private fun getSystemDnsServers(): List<String> {
        val connectivityManager = getSystemService(ConnectivityManager::class.java) ?: return emptyList()
        val network = connectivityManager.activeNetwork ?: return emptyList()
        val linkProperties = connectivityManager.getLinkProperties(network) ?: return emptyList()
        return linkProperties.dnsServers.mapNotNull { it.hostAddress }
    }

    private fun startForegroundNotification() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            val channel = NotificationChannel(
                CHANNEL_ID, "VPN Tunnel", NotificationManager.IMPORTANCE_LOW
            )
            val nm = getSystemService(NotificationManager::class.java)
            nm.createNotificationChannel(channel)
        }

        startForeground(NOTIFICATION_ID, buildNotification(getString(VpnStatus.STARTING.labelRes)))
    }

    private fun buildNotification(text: String): Notification {
        val openIntent = Intent(this, MainActivity::class.java).apply {
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
            .setContentTitle(getString(R.string.notification_vpn_title))
            .setContentText(text)
            .setSmallIcon(android.R.drawable.ic_lock_lock)
            .setOngoing(true)
            .setContentIntent(openPending)
            .addAction(Notification.Action.Builder(null, getString(R.string.notification_disconnect), stopPending).build())
            .build()
    }
}
