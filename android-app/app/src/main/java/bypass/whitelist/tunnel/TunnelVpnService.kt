package bypass.whitelist.tunnel

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.net.ConnectivityManager
import android.net.VpnService
import android.os.Build
import android.os.Handler
import android.os.Looper
import android.os.ParcelFileDescriptor
import android.util.Log
import bypass.whitelist.MainActivity
import bypass.whitelist.R
import bypass.whitelist.util.Callback
import bypass.whitelist.util.DnsMode
import bypass.whitelist.util.Prefs
import bypass.whitelist.util.SocksAuth
import bypass.whitelist.util.Vpn
import androidbind.Androidbind
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import kotlin.concurrent.thread

class TunnelVpnService : VpnService() {

    companion object {
        const val TAG = "TunnelVPN"
        const val CHANNEL_ID = "vpn_channel"
        const val NOTIFICATION_ID = 1
        const val ACTION_STOP = "bypass.whitelist.STOP_VPN"
        @Volatile var instance: TunnelVpnService? = null
        @Volatile var onDisconnect: Callback? = null

        fun requestStop(context: Context) {
            val running = instance?.let { it.isRunning || it.startInProgress || it.stopInProgress } == true
            val intent = Intent(context, TunnelVpnService::class.java)
            try {
                if (running) {
                    context.startService(intent.apply { action = ACTION_STOP })
                } else {
                    context.stopService(intent)
                    TunnelServiceState.requestTileRefresh(context)
                }
            } catch (t: Throwable) {
                Log.w(TAG, "requestStop failed: ${t.message}")
            }
        }
    }

    @Volatile var isRunning: Boolean = false
    @Volatile internal var startInProgress: Boolean = false
    @Volatile internal var stopInProgress: Boolean = false
    private var vpnFd: ParcelFileDescriptor? = null
    private var tun2socksThread: Thread? = null
    @Volatile private var tunGeneration: Long = 0

    override fun onCreate() {
        super.onCreate()
        instance = this
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        if (intent?.action == ACTION_STOP) {
            if (!isRunning && !startInProgress && !stopInProgress) {
                safeStopSelf()
                return START_NOT_STICKY
            }
            stop()
            return START_NOT_STICKY
        }
        start()
        return START_STICKY
    }

    override fun onDestroy() {
        if ((isRunning || startInProgress) && !stopInProgress) {
            stop()
        }
        if (instance === this) {
            instance = null
        }
        onDisconnect = null
        super.onDestroy()
    }

    fun updateStatus(status: VpnStatus) {
        val nm = getSystemService(NotificationManager::class.java)
        nm.notify(NOTIFICATION_ID, buildNotification(getString(status.labelRes)))
        TunnelServiceState.vpnStatusCallback?.invoke(status)
        TunnelServiceState.requestTileRefresh(this)
    }

    @Synchronized
    fun stop() {
        if (stopInProgress) return
        if (!isRunning && !startInProgress) {
            safeStopSelf()
            return
        }
        isRunning = false
        startInProgress = false
        stopInProgress = true
        bumpTunGeneration()
        val disconnectCallback = onDisconnect

        thread(name = "vpn-stop") {
            stopTun2SocksWithTimeout()

            try {
                tun2socksThread?.join(1000)
            } catch (e: Exception) {}

            Handler(Looper.getMainLooper()).post {
                try {
                    tun2socksThread = null
                    vpnFd = null
                    @Suppress("DEPRECATION")
                    stopForeground(true)
                    disconnectCallback?.invoke()
                    TunnelServiceState.requestTileRefresh(this@TunnelVpnService)
                    stopSelf()
                } catch (t: Throwable) {
                    stopInProgress = false
                    Log.e(TAG, "Crash during VPN stop: ${t.message}", t)
                }
            }
        }
    }

    private fun stopTun2SocksWithTimeout() {
        val stopDone = CountDownLatch(1)
        thread(name = "tun2socks-stop") {
            try {
                Androidbind.stopTun2Socks()
            } catch (e: Exception) {
                Log.e(TAG, "tun2socks stop error: ${e.message}")
            } finally {
                stopDone.countDown()
            }
        }
        stopDone.await(2000, TimeUnit.MILLISECONDS)
    }

    private fun start() {
        if (isRunning || startInProgress) return
        startInProgress = true

        startForegroundNotification()

        val builder = Builder()
            .setSession(Vpn.SESSION_NAME)
            .addAddress(Vpn.ADDRESS, Vpn.PREFIX_LENGTH)
            .addRoute(Vpn.ROUTE, 0)
            .addAddress(Vpn.ADDRESS6, Vpn.PREFIX_LENGTH6)
            .addRoute(Vpn.ROUTE6, 0)
            .setMtu(Vpn.MTU)

        when (Prefs.dnsMode) {
            DnsMode.SYSTEM -> {
                val systemDns = getSystemDnsServers()
                if (systemDns.isNotEmpty()) {
                    for (dns in systemDns) builder.addDnsServer(dns)
                } else {
                    builder.addDnsServer(Vpn.DNS_PRIMARY)
                    builder.addDnsServer(Vpn.DNS_SECONDARY)
                }
            }
            DnsMode.CUSTOM -> {
                val primary = Prefs.dnsPrimary.trim()
                val secondary = Prefs.dnsSecondary.trim()
                if (primary.isNotEmpty()) builder.addDnsServer(primary)
                if (secondary.isNotEmpty()) builder.addDnsServer(secondary)
                if (primary.isEmpty() && secondary.isEmpty()) {
                    builder.addDnsServer(Vpn.DNS_PRIMARY)
                    builder.addDnsServer(Vpn.DNS_SECONDARY)
                }
            }
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
            startInProgress = false
            TunnelServiceState.logCallback?.invoke("Failed to establish VPN")
            TunnelServiceState.vpnStatusCallback?.invoke(VpnStatus.CALL_FAILED)
            stopSelf()
            return
        }

        isRunning = true
        startInProgress = false
        val fd = vpnFd!!.detachFd()
        vpnFd = null
        Log.i(TAG, "VPN established, fd=$fd, SOCKS5 ${SocksAuth.user}:${SocksAuth.pass}@${Prefs.socksHost}:${Prefs.socksPort}")
        updateStatus(VpnStatus.TUNNEL_ACTIVE)
        val startGeneration = bumpTunGeneration()

        tun2socksThread = Thread {
            if (!isRunning || stopInProgress || !isTunGenerationCurrent(startGeneration)) {
                closeRawFd(fd)
                return@Thread
            }
            try {
                Androidbind.startTun2Socks(fd.toLong(), Vpn.MTU.toLong(), Prefs.socksPort, SocksAuth.user, SocksAuth.pass)
            } catch (e: Exception) {
                Log.e(TAG, "tun2socks error: ${e.message}")
                isRunning = false
                startInProgress = false
                stopInProgress = false
                TunnelServiceState.logCallback?.invoke("tun2socks error: ${e.message}")
                TunnelServiceState.vpnStatusCallback?.invoke(VpnStatus.TUNNEL_LOST)
            }
        }.also { it.start() }
    }

    private fun bumpTunGeneration(): Long = synchronized(this) {
        tunGeneration += 1
        tunGeneration
    }

    private fun isTunGenerationCurrent(generation: Long): Boolean = synchronized(this) {
        tunGeneration == generation
    }

    private fun closeRawFd(fd: Int) {
        runCatching { ParcelFileDescriptor.adoptFd(fd).close() }
            .onFailure { Log.w(TAG, "Failed to close stale tun fd=$fd: ${it.message}") }
    }

    private fun safeStopSelf() {
        stopInProgress = false
        isRunning = false
        startInProgress = false
        runCatching {
            @Suppress("DEPRECATION")
            stopForeground(true)
        }
        TunnelServiceState.requestTileRefresh(this)
        stopSelf()
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
