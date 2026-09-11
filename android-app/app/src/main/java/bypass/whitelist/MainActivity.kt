package bypass.whitelist

import android.Manifest
import android.animation.ArgbEvaluator
import android.content.ClipData
import android.content.ClipboardManager
import android.content.Intent
import android.content.pm.PackageManager
import android.net.VpnService
import android.os.Build
import android.os.Bundle
import android.view.View
import android.view.animation.AccelerateDecelerateInterpolator
import android.widget.ImageView
import android.widget.LinearLayout
import android.widget.ScrollView
import android.widget.TextView
import android.widget.Toast
import androidx.activity.enableEdgeToEdge
import androidx.activity.result.contract.ActivityResultContracts
import androidx.appcompat.app.AppCompatActivity
import androidx.core.content.ContextCompat
import androidx.core.content.FileProvider
import androidx.core.view.ViewCompat
import androidx.core.view.WindowInsetsCompat
import androidx.core.view.doOnLayout
import androidx.fragment.app.Fragment
import androidx.fragment.app.FragmentManager
import androidx.viewpager2.adapter.FragmentStateAdapter
import androidx.viewpager2.widget.ViewPager2
import bypass.whitelist.tunnel.CallConfig
import bypass.whitelist.tunnel.CallPlatform
import bypass.whitelist.tunnel.HeadlessJoinController
import bypass.whitelist.tunnel.HeadlessSessionService
import bypass.whitelist.tunnel.PortGuard
import bypass.whitelist.tunnel.ProxyService
import bypass.whitelist.tunnel.TunnelMode
import bypass.whitelist.tunnel.TunnelServiceState
import bypass.whitelist.tunnel.TunnelVpnService
import bypass.whitelist.tunnel.VpnStatus
import bypass.whitelist.ui.CallsListener
import bypass.whitelist.ui.HeadlessVkFragment
import bypass.whitelist.ui.JoinFragmentHost
import bypass.whitelist.ui.JoinSessionShutdown
import bypass.whitelist.ui.JsHookJoinFragment
import bypass.whitelist.ui.LogsFragment
import bypass.whitelist.ui.MainActivityHost
import bypass.whitelist.ui.MainFragment
import bypass.whitelist.ui.SettingsScreenFragment
import bypass.whitelist.util.LogWriter
import bypass.whitelist.util.Net
import bypass.whitelist.util.Prefs
import bypass.whitelist.util.SocksAuth
import bypass.whitelist.util.maskUrl
import java.net.InetSocketAddress
import java.net.Socket
import kotlin.concurrent.thread

class MainActivity :
    AppCompatActivity(),
    JoinFragmentHost,
    MainActivityHost,
    MainFragment.Host,
    SettingsScreenFragment.Host,
    LogsFragment.Host,
    CallsListener {

    private val logWriter by lazy { LogWriter(cacheDir) }

    private lateinit var bottomNav: View
    private lateinit var navMain: LinearLayout
    private lateinit var navSettings: LinearLayout
    private lateinit var navLogs: LinearLayout
    private lateinit var navMainIcon: ImageView
    private lateinit var navSettingsIcon: ImageView
    private lateinit var navLogsIcon: ImageView
    private lateinit var navMainLabel: TextView
    private lateinit var navSettingsLabel: TextView
    private lateinit var navLogsLabel: TextView
    private lateinit var tabContainer: ViewPager2
    private lateinit var navIndicator: View
    private lateinit var subPageContainer: View
    private lateinit var joinOverlayContainer: View
    private lateinit var overlayLogs: View
    private lateinit var overlayLogsText: TextView
    private lateinit var overlayLogsScroll: ScrollView

    private var currentTabId: Int = 0
    private var lastStatus: VpnStatus? = null
    private var connected: Boolean = false
    private var activeJoinUrl: String = ""
    private var activeHeadlessController: HeadlessJoinController? = null
    private var navPageChangeCallback: ViewPager2.OnPageChangeCallback? = null
    private var navScrollState: Int = ViewPager2.SCROLL_STATE_IDLE
    @Volatile private var resetInProgress: Boolean = false
    @Volatile private var overlayVisible: Boolean = false
    @Volatile private var resetGeneration: Long = 0L
    private var pendingConnectConfig: CallConfig? = null
    private val navColorEvaluator = ArgbEvaluator()

    private val vpnLauncher = registerForActivityResult(
        ActivityResultContracts.StartActivityForResult()
    ) { result ->
        if (result.resultCode == RESULT_OK) startVpnService()
        else appendLog("VPN permission denied")
    }

    private val notificationPermissionLauncher = registerForActivityResult(
        ActivityResultContracts.RequestPermission()
    ) { granted ->
        if (!granted) appendLog("Notification permission denied")
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        setContentView(R.layout.activity_main)

        requestNotificationPermissionIfNeeded()

        bottomNav = findViewById(R.id.bottomNav)
        navMain = findViewById(R.id.navMain)
        navSettings = findViewById(R.id.navSettings)
        navLogs = findViewById(R.id.navLogs)
        navMainIcon = findViewById(R.id.navMainIcon)
        navSettingsIcon = findViewById(R.id.navSettingsIcon)
        navLogsIcon = findViewById(R.id.navLogsIcon)
        navMainLabel = findViewById(R.id.navMainLabel)
        navSettingsLabel = findViewById(R.id.navSettingsLabel)
        navLogsLabel = findViewById(R.id.navLogsLabel)
        tabContainer = findViewById(R.id.tabContainer)
        navIndicator = findViewById(R.id.navIndicator)
        subPageContainer = findViewById(R.id.subPageContainer)
        joinOverlayContainer = findViewById(R.id.joinOverlayContainer)
        overlayLogs = findViewById(R.id.overlayLogs)
        overlayLogsText = findViewById(R.id.overlayLogsText)
        overlayLogsScroll = findViewById(R.id.overlayLogsScroll)

        tabContainer.adapter = object : FragmentStateAdapter(this) {
            override fun getItemCount(): Int = 3
            override fun createFragment(position: Int): Fragment {
                return when (position) {
                    TAB_MAIN -> MainFragment()
                    TAB_SETTINGS -> SettingsScreenFragment()
                    else -> LogsFragment()
                }
            }
        }
        tabContainer.offscreenPageLimit = 3

        navPageChangeCallback = object : ViewPager2.OnPageChangeCallback() {
            override fun onPageScrolled(position: Int, positionOffset: Float, positionOffsetPixels: Int) {
                moveNavIndicatorForPager(position, positionOffset)
                interpolateNavSelection(position, positionOffset)
            }

            override fun onPageSelected(position: Int) {
                currentTabId = when (position) {
                    TAB_MAIN -> R.id.navMain
                    TAB_SETTINGS -> R.id.navSettings
                    TAB_LOGS -> R.id.navLogs
                    else -> return
                }
                if (navScrollState == ViewPager2.SCROLL_STATE_IDLE) {
                    updateNavSelection(currentTabId)
                }
                settingsFragment()?.refresh()
            }

            override fun onPageScrollStateChanged(state: Int) {
                navScrollState = state
                if (state == ViewPager2.SCROLL_STATE_IDLE) {
                    updateNavSelection(currentTabId)
                    moveNavIndicatorTo(currentTabId, animate = false)
                }
            }
        }.also(tabContainer::registerOnPageChangeCallback)

        findViewById<View>(R.id.overlayCopyButton).setOnClickListener { copyLogs() }
        findViewById<View>(R.id.overlayShareButton).setOnClickListener { shareLogs() }

        val baseTabPaddingTop = findViewById<View>(R.id.tabContainerWrap).paddingTop
        val bottomWrap = findViewById<View>(R.id.bottomWrap)
        val baseBottomWrapPaddingBottom = bottomWrap.paddingBottom
        ViewCompat.setOnApplyWindowInsetsListener(findViewById(R.id.main)) { _, insets ->
            val bars = insets.getInsets(WindowInsetsCompat.Type.systemBars())
            findViewById<View>(R.id.tabContainerWrap).setPadding(
                bars.left,
                baseTabPaddingTop + bars.top,
                bars.right,
                0
            )
            subPageContainer.setPadding(bars.left, bars.top, bars.right, 0)
            joinOverlayContainer.setPadding(bars.left, bars.top, bars.right, 0)
            bottomWrap.setPadding(
                bars.left,
                0,
                bars.right,
                baseBottomWrapPaddingBottom + bars.bottom
            )
            insets
        }

        navMain.setOnClickListener { selectNavTab(R.id.navMain) }
        navSettings.setOnClickListener { selectNavTab(R.id.navSettings) }
        navLogs.setOnClickListener { selectNavTab(R.id.navLogs) }

        val restoredTabId =
            savedInstanceState?.getInt(STATE_CURRENT_TAB_ID, R.id.navMain) ?: R.id.navMain
        selectNavTab(restoredTabId, animatePager = false)
        findViewById<View>(R.id.navItemsRow).doOnLayout {
            moveNavIndicatorTo(currentTabId, animate = false)
        }

        TunnelVpnService.onDisconnect = { runOnUiThread { onDisconnectFromService() } }
        ProxyService.onDisconnect = { runOnUiThread { onDisconnectFromService() } }

        if (CALL_LINK.isNotEmpty() && !TunnelServiceState.isAnyTunnelComponentRunning(this)) {
            startJoinFor(CallConfig.newWith(name = CallConfig.suggestNameFor(CALL_LINK), url = CALL_LINK))
        } else if (Prefs.connectOnStart && !TunnelServiceState.isAnyTunnelComponentRunning(this)) {
            Prefs.activeDestination?.let(::startJoinFor)
        }

        handleIntent(intent)
    }

    override fun onResume() {
        super.onResume()
        TunnelVpnService.onDisconnect = { runOnUiThread { onDisconnectFromService() } }
        ProxyService.onDisconnect = { runOnUiThread { onDisconnectFromService() } }

        TunnelServiceState.vpnStatusCallback = { status ->
            runOnUiThread {
                if (resetInProgress) {
                    mainFragment()?.onStatusChanged(VpnStatus.STOPPING)
                    mainFragment()?.onStatusTextChanged("Stopping previous session...")
                    return@runOnUiThread
                }
                lastStatus = status
                mainFragment()?.onStatusChanged(status)
                if (status == VpnStatus.TUNNEL_ACTIVE) {
                    if (!connected) {
                        connected = true
                        mainFragment()?.onConnectedChanged(true)
                    }
                } else if (status == VpnStatus.CALL_FAILED || status == VpnStatus.CALL_DISCONNECTED || status == VpnStatus.TUNNEL_LOST) {
                    if (connected) {
                        connected = false
                        mainFragment()?.onConnectedChanged(false)
                    }
                }
            }
        }

        TunnelServiceState.logCallback = { message ->
            runOnUiThread { appendLog(message) }
        }

        when {
            resetInProgress -> {
                connected = false
                lastStatus = VpnStatus.STOPPING
                mainFragment()?.onConnectedChanged(false)
                mainFragment()?.onStatusChanged(VpnStatus.STOPPING)
                mainFragment()?.onStatusTextChanged("Stopping previous session...")
            }
            TunnelServiceState.isTunnelActive(this) -> {
                if (!connected || lastStatus != VpnStatus.TUNNEL_ACTIVE) {
                    connected = true
                    lastStatus = VpnStatus.TUNNEL_ACTIVE
                    mainFragment()?.onStatusChanged(VpnStatus.TUNNEL_ACTIVE)
                    mainFragment()?.onConnectedChanged(true)
                }
            }
            TunnelServiceState.isHeadlessSessionRunning(this) -> {
                connected = false
                lastStatus = VpnStatus.CONNECTING
                mainFragment()?.onConnectedChanged(false)
                mainFragment()?.onStatusChanged(VpnStatus.CONNECTING)
            }
            connected && lastStatus == VpnStatus.TUNNEL_ACTIVE -> {
                onDisconnectFromService()
            }
        }
    }

    override fun onPause() {
        super.onPause()
        TunnelServiceState.vpnStatusCallback = null
        TunnelServiceState.logCallback = null
    }

    override fun onDestroy() {
        navPageChangeCallback?.let(tabContainer::unregisterOnPageChangeCallback)
        navPageChangeCallback = null
        TunnelVpnService.onDisconnect = null
        ProxyService.onDisconnect = null
        logWriter.close()
        super.onDestroy()
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        handleIntent(intent)
    }

    override fun onSaveInstanceState(outState: Bundle) {
        super.onSaveInstanceState(outState)
        outState.putInt(STATE_CURRENT_TAB_ID, currentTabId)
    }

    override fun onConnectPressed(config: CallConfig) {
        if (resetInProgress) {
            pendingConnectConfig = config
            appendLog("Queued connect after previous session stops")
            mainFragment()?.onStatusTextChanged("Stopping previous session...")
            return
        }
        if (TunnelServiceState.isAnyTunnelComponentRunning(this) || !PortGuard.isPortAvailable(Prefs.socksPort)) {
            pendingConnectConfig = config
            appendLog("Waiting for previous local tunnel to stop")
            fullReset()
            return
        }
        startJoinFor(config)
    }

    override fun onDisconnectPressed() {
        pendingConnectConfig = null
        if (resetInProgress) {
            forceUnlockReset("Stopped waiting for previous session")
            return
        }
        fullReset()
    }

    override fun onPingPressed(callback: (Boolean, Int) -> Unit) {
        thread {
            val started = System.nanoTime()
            val ok = try {
                probeViaSocks5(host = "google.com", port = 443)
            } catch (_: Exception) {
                false
            }
            val rtt = ((System.nanoTime() - started) / 1_000_000).toInt()
            runOnUiThread { callback(ok, rtt) }
        }
    }

    override fun isTunnelActive(): Boolean = connected

    override fun currentStatus(): VpnStatus? = lastStatus

    override fun onDestinationSelected(config: CallConfig) {
        Prefs.activeDestinationId = config.id
        mainFragment()?.onDestinationsChanged()
    }

    override fun onDestinationsChanged() {
        mainFragment()?.onDestinationsChanged()
    }

    override fun onTunnelModeChanged(mode: TunnelMode) {
        fullReset()
    }

    override fun onForgetAllDestinations() {
        Prefs.savedDestinations = emptyList()
        Prefs.activeDestinationId = ""
        mainFragment()?.onDestinationsChanged()
        Toast.makeText(this, R.string.settings_toast_destinations_cleared, Toast.LENGTH_SHORT)
            .show()
    }

    override fun onResetAllSettings() {
        Prefs.resetAllSettings()
        App.applyTheme(Prefs.themeMode)
        settingsFragment()?.refresh()
        Toast.makeText(this, R.string.settings_toast_reset_done, Toast.LENGTH_SHORT).show()
    }

    override fun activityLogLines(): List<String> {
        val text = logWriter.displayText()
        if (text.isEmpty()) return emptyList()
        return text.split('\n').filter { it.isNotBlank() }
    }

    override fun activityLogRevision(): Long = logWriter.revision()

    override fun copyLogs() {
        val contents =
            if (logWriter.file.exists()) logWriter.file.readText() else logWriter.displayText()
        val clipboard =
            getSystemService(CLIPBOARD_SERVICE) as ClipboardManager
        clipboard.setPrimaryClip(ClipData.newPlainText("relay.log", contents))
        Toast.makeText(this, R.string.copy_logs_toast, Toast.LENGTH_SHORT).show()
    }

    override fun shareLogs() {
        val uri = FileProvider.getUriForFile(
            this,
            "${packageName}.fileprovider",
            logWriter.file
        )
        val share = Intent(Intent.ACTION_SEND).apply {
            type = "text/plain"
            putExtra(Intent.EXTRA_STREAM, uri)
            addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
        }
        startActivity(Intent.createChooser(share, getString(R.string.share_logs)))
    }

    override fun appendLog(message: String) {
        logWriter.append(message)
        if (overlayVisible) {
            runOnUiThread {
                overlayLogsText.text = logWriter.displayText()
                overlayLogsScroll.post { overlayLogsScroll.fullScroll(View.FOCUS_DOWN) }
            }
        }
    }

    override fun onJoinStatusText(text: String) {
        if (resetInProgress) return
        runOnUiThread { mainFragment()?.onStatusTextChanged(text) }
    }

    override fun onJoinStatus(status: VpnStatus) {
        if (resetInProgress && status != VpnStatus.CALL_FAILED) return
        TunnelVpnService.instance?.updateStatus(status)
        ProxyService.instance?.updateStatus(status)
        lastStatus = status
        runOnUiThread {
            if (status == VpnStatus.CALL_FAILED) {
                fullReset()
                lastStatus = VpnStatus.CALL_FAILED
                mainFragment()?.onStatusChanged(VpnStatus.CALL_FAILED)
                return@runOnUiThread
            }
            mainFragment()?.onStatusChanged(status)
            if (status == VpnStatus.TUNNEL_ACTIVE) {
                connected = true
                mainFragment()?.onConnectedChanged(true)
            }
        }
    }

    override fun pushSubPage(fragment: Fragment) {
        subPageContainer.visibility = View.VISIBLE
        supportFragmentManager.beginTransaction()
            .replace(R.id.subPageContainer, fragment, SUB_PAGE_TAG)
            .addToBackStack(SUB_PAGE_TAG)
            .commit()
    }

    override fun popSubPage() {
        supportFragmentManager.popBackStackImmediate(
            SUB_PAGE_TAG,
            FragmentManager.POP_BACK_STACK_INCLUSIVE
        )
        subPageContainer.visibility = View.GONE
    }

    override fun onJoinCancel() {
        pendingConnectConfig = null
        runOnUiThread { fullReset() }
    }

    override fun setJoinUiVisible(visible: Boolean) {
        runOnUiThread { setJoinOverlayVisible(visible) }
    }

    override fun requestVpn() {
        if (Prefs.proxyOnly) {
            appendLog("Proxy only mode, skipping VPN")
            startService(Intent(this, ProxyService::class.java))
            onJoinStatus(VpnStatus.TUNNEL_ACTIVE)
            return
        }
        if (TunnelServiceState.hasForeignVpn(this)) {
            appendLog("Another VPN is active, requesting system VPN switch")
            mainFragment()?.onStatusTextChanged("Requesting VPN replacement...")
        }
        val intent = VpnService.prepare(this)
        if (intent != null) vpnLauncher.launch(intent) else startVpnService()
    }

    private fun requestNotificationPermissionIfNeeded() {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.TIRAMISU) return
        val granted = ContextCompat.checkSelfPermission(
            this,
            Manifest.permission.POST_NOTIFICATIONS
        ) == PackageManager.PERMISSION_GRANTED
        if (granted) return
        notificationPermissionLauncher.launch(Manifest.permission.POST_NOTIFICATIONS)
    }

    private fun handleIntent(intent: Intent?) {
        if (intent?.action != ACTION_AUTO_START) return
        intent.action = null
        val isConnecting = lastStatus == VpnStatus.CONNECTING
        if (!connected && !isConnecting && !TunnelServiceState.isAnyTunnelComponentRunning(this)) {
            Prefs.activeDestination?.let(::onConnectPressed) ?: run {
                Toast.makeText(this, R.string.error_no_destination, Toast.LENGTH_SHORT).show()
            }
        } else if (connected) {
            onDisconnectPressed()
        }
    }

    private fun selectNavTab(itemId: Int, animatePager: Boolean = true) {
        if (currentTabId == itemId) return
        currentTabId = itemId
        dismissSubPage()
        val index = when (itemId) {
            R.id.navMain -> TAB_MAIN
            R.id.navSettings -> TAB_SETTINGS
            R.id.navLogs -> TAB_LOGS
            else -> TAB_MAIN
        }
        updateNavSelection(itemId)
        val animateIndicator = tabContainer.currentItem != index
        if (!animatePager || tabContainer.currentItem == index) {
            moveNavIndicatorTo(itemId, animate = animateIndicator)
        }
        tabContainer.setCurrentItem(index, animatePager)
    }

    private fun updateNavSelection(itemId: Int) {
        applyNavSelectionState(0f, when (itemId) {
            R.id.navMain -> TAB_MAIN
            R.id.navSettings -> TAB_SETTINGS
            R.id.navLogs -> TAB_LOGS
            else -> TAB_MAIN
        })
    }

    private fun moveNavIndicatorTo(itemId: Int, animate: Boolean) {
        val target = when (itemId) {
            R.id.navMain -> navMain
            R.id.navSettings -> navSettings
            R.id.navLogs -> navLogs
            else -> null
        } ?: return

        navIndicator.layoutParams = navIndicator.layoutParams.apply {
            width = target.width
            height = target.height
        }
        navIndicator.requestLayout()
        val targetTranslation = target.left.toFloat()
        if (animate) {
            navIndicator.animate()
                .translationX(targetTranslation)
                .setDuration(220L)
                .setInterpolator(AccelerateDecelerateInterpolator())
                .start()
        } else {
            navIndicator.animate().cancel()
            navIndicator.translationX = targetTranslation
        }
    }

    private fun moveNavIndicatorForPager(position: Int, positionOffset: Float) {
        val targets = listOf(navMain, navSettings, navLogs)
        val current = targets.getOrNull(position) ?: return
        val next = targets.getOrNull(position + 1)
        val targetLeft = if (next != null) {
            current.left + ((next.left - current.left) * positionOffset)
        } else {
            current.left.toFloat()
        }
        val lp = navIndicator.layoutParams
        if (lp.width != current.width || lp.height != current.height) {
            navIndicator.layoutParams = lp.apply {
                width = current.width
                height = current.height
            }
            navIndicator.requestLayout()
        }
        navIndicator.animate().cancel()
        navIndicator.translationX = targetLeft
    }

    private fun interpolateNavSelection(position: Int, positionOffset: Float) {
        if (navScrollState == ViewPager2.SCROLL_STATE_IDLE) return
        val clampedOffset = positionOffset.coerceIn(0f, 1f)
        applyNavSelectionState(clampedOffset, position)
    }

    private fun applyNavSelectionState(positionOffset: Float, position: Int) {
        val emphasis = floatArrayOf(0f, 0f, 0f)
        val baseIndex = position.coerceIn(0, emphasis.lastIndex)
        emphasis[baseIndex] = 1f - positionOffset
        val nextIndex = (baseIndex + 1).coerceAtMost(emphasis.lastIndex)
        if (nextIndex != baseIndex) {
            emphasis[nextIndex] = positionOffset
        }

        applyNavVisual(navMainIcon, navMainLabel, emphasis[0])
        applyNavVisual(navSettingsIcon, navSettingsLabel, emphasis[1])
        applyNavVisual(navLogsIcon, navLogsLabel, emphasis[2])
    }

    private fun applyNavVisual(icon: ImageView, label: TextView, emphasis: Float) {
        val accent = getColor(R.color.accent_emerald)
        val ink = getColor(R.color.ink_3)
        val blended = navColorEvaluator.evaluate(emphasis, ink, accent) as Int
        icon.setColorFilter(blended)
        icon.alpha = 0.72f + (0.28f * emphasis)
        label.setTextColor(blended)
        label.alpha = 0.74f + (0.26f * emphasis)
        label.scaleX = 1f + (0.06f * emphasis)
        label.scaleY = 1f + (0.06f * emphasis)
        label.paint.isFakeBoldText = emphasis > 0.92f
    }

    private fun mainFragment(): MainFragment? =
        supportFragmentManager.fragments.firstOrNull { it is MainFragment } as? MainFragment

    private fun settingsFragment(): SettingsScreenFragment? =
        supportFragmentManager.fragments.firstOrNull { it is SettingsScreenFragment } as? SettingsScreenFragment

    private fun logsFragment(): LogsFragment? =
        supportFragmentManager.fragments.firstOrNull { it is LogsFragment } as? LogsFragment

    private fun probeViaSocks5(host: String, port: Int): Boolean {
        Socket().use { socket ->
            socket.connect(InetSocketAddress(Net.LOCALHOST, Prefs.socksPort.toInt()), 5000)
            socket.soTimeout = 15000
            val output = socket.getOutputStream()
            val input = socket.getInputStream()

            output.write(byteArrayOf(0x05, 0x01, 0x02))
            output.flush()
            if (input.read() != 0x05 || input.read() != 0x02) return false

            val userBytes = SocksAuth.user.toByteArray(Charsets.US_ASCII)
            val passBytes = SocksAuth.pass.toByteArray(Charsets.US_ASCII)
            val authPacket = ByteArray(3 + userBytes.size + passBytes.size)
            authPacket[0] = 0x01
            authPacket[1] = userBytes.size.toByte()
            System.arraycopy(userBytes, 0, authPacket, 2, userBytes.size)
            authPacket[2 + userBytes.size] = passBytes.size.toByte()
            System.arraycopy(passBytes, 0, authPacket, 3 + userBytes.size, passBytes.size)
            output.write(authPacket)
            output.flush()
            if (input.read() != 0x01 || input.read() != 0x00) return false

            val hostBytes = host.toByteArray(Charsets.US_ASCII)
            val request = ByteArray(4 + 1 + hostBytes.size + 2)
            request[0] = 0x05
            request[1] = 0x01
            request[2] = 0x00
            request[3] = 0x03
            request[4] = hostBytes.size.toByte()
            System.arraycopy(hostBytes, 0, request, 5, hostBytes.size)
            request[5 + hostBytes.size] = ((port shr 8) and 0xff).toByte()
            request[6 + hostBytes.size] = (port and 0xff).toByte()
            output.write(request)
            output.flush()

            return input.read() == 0x05 && input.read() == 0x00
        }
    }

    private fun dismissSubPage() {
        if (supportFragmentManager.backStackEntryCount > 0) {
            supportFragmentManager.popBackStack(
                SUB_PAGE_TAG,
                FragmentManager.POP_BACK_STACK_INCLUSIVE
            )
        }
        subPageContainer.visibility = View.GONE
    }

    private fun setJoinOverlayVisible(visible: Boolean) {
        joinOverlayContainer.visibility = if (visible) View.VISIBLE else View.GONE
        overlayLogs.visibility = if (visible) View.VISIBLE else View.GONE
        bottomNav.visibility = if (visible) View.GONE else View.VISIBLE
        overlayVisible = visible
        if (visible) {
            overlayLogsText.text = logWriter.displayText()
            overlayLogsScroll.post { overlayLogsScroll.fullScroll(View.FOCUS_DOWN) }
        }
    }

    private fun startJoinFor(config: CallConfig) {
        if (resetInProgress) {
            pendingConnectConfig = config
            appendLog("Queued connect after previous session stops")
            mainFragment()?.onStatusTextChanged("Stopping previous session...")
            return
        }
        if (TunnelServiceState.isAnyTunnelComponentRunning(this) || !PortGuard.isPortAvailable(Prefs.socksPort)) {
            pendingConnectConfig = config
            appendLog("Waiting for previous local tunnel to stop")
            fullReset()
            return
        }
        val url = config.url.trim()
        if (url.isEmpty()) return

        val platform = config.platform
        if (Prefs.activeTunnelMode == TunnelMode.DC &&
            (platform == CallPlatform.TELEMOST || platform == CallPlatform.DION)
        ) {
            Toast.makeText(this, R.string.dc_mode_not_supported, Toast.LENGTH_SHORT).show()
        }

        if (connected) {
            fullReset()
        }

        activeJoinUrl = url
        logWriter.reset()
        runOnUiThread { logsFragment()?.refresh() }
        appendLog("Loading: ${maskUrl(url)}")
        lastStatus = VpnStatus.CONNECTING
        mainFragment()?.onStatusChanged(VpnStatus.CONNECTING)
        mainFragment()?.onConnectedChanged(false)

        val headlessMode =
            Prefs.headless || platform == CallPlatform.WBSTREAM || platform == CallPlatform.DION || platform == CallPlatform.BITRIX

        if (headlessMode && platform != CallPlatform.VK) {
            setJoinOverlayVisible(false)
            activeHeadlessController = HeadlessJoinController(
                applicationInfo.nativeLibraryDir,
                this,
                platform,
                url,
            ).also { it.start() }
            return
        }

        val joinFragment = if (headlessMode) {
            HeadlessVkFragment.newInstance(url)
        } else {
            JsHookJoinFragment.newInstance(url)
        }

        setJoinOverlayVisible(!headlessMode)

        supportFragmentManager.beginTransaction()
            .replace(R.id.joinOverlayContainer, joinFragment)
            .commit()
    }

    private fun startVpnService() {
        startService(Intent(this, TunnelVpnService::class.java))
        appendLog("VPN start requested")
        onJoinStatus(VpnStatus.STARTING)
    }

    private fun onDisconnectFromService() {
        if (resetInProgress) {
            maybeFinishReset()
            return
        }
        connected = false
        lastStatus = null
        closeActiveHeadlessController()
        removeJoinFragment()
        setJoinOverlayVisible(false)
        mainFragment()?.onConnectedChanged(false)
        mainFragment()?.onStatusChanged(VpnStatus.CALL_DISCONNECTED)
    }

    private fun fullReset() {
        if (resetInProgress) return
        resetInProgress = true
        val resetId = ++resetGeneration
        connected = false
        lastStatus = VpnStatus.STOPPING
        val controller = activeHeadlessController
        activeHeadlessController = null
        activeJoinUrl = ""

        removeJoinFragment()
        TunnelVpnService.requestStop(this)
        ProxyService.requestStop(this)
        HeadlessSessionService.requestStop(this)
        setJoinOverlayVisible(false)
        mainFragment()?.onConnectedChanged(false)
        mainFragment()?.onStatusChanged(VpnStatus.STOPPING)
        mainFragment()?.onStatusTextChanged("Stopping previous session...")
        thread(name = "full-reset-shutdown") {
            controller?.close()
            var attempts = 0
            while (
                attempts < 40 &&
                (TunnelServiceState.isAnyTunnelComponentRunning(this@MainActivity) ||
                    !PortGuard.isPortAvailable(Prefs.socksPort))
            ) {
                if (!isResetCurrent(resetId)) return@thread
                Thread.sleep(100)
                attempts++
            }
            if (!isResetCurrent(resetId)) return@thread
            if (TunnelServiceState.isAnyTunnelComponentRunning(this@MainActivity) || !PortGuard.isPortAvailable(Prefs.socksPort)) {
                TunnelVpnService.requestStop(this@MainActivity)
                ProxyService.requestStop(this@MainActivity)
                HeadlessSessionService.requestStop(this@MainActivity)
                PortGuard.ensurePortFree(Prefs.socksPort)
                Thread.sleep(150)
            }
            if (!isResetCurrent(resetId)) return@thread
            if (TunnelServiceState.isAnyTunnelComponentRunning(this@MainActivity) || !PortGuard.isPortAvailable(Prefs.socksPort)) {
                runOnUiThread {
                    if (isResetCurrent(resetId)) {
                        forceUnlockReset("Previous session is still shutting down. Try connect again.")
                    }
                }
                return@thread
            }
            Thread.sleep(400)
            runOnUiThread {
                if (isResetCurrent(resetId)) {
                    maybeFinishReset(resetId)
                }
            }
        }
    }

    private fun maybeFinishReset(expectedResetId: Long? = null) {
        if (!resetInProgress) return
        if (expectedResetId != null && expectedResetId != resetGeneration) return
        if (TunnelServiceState.isAnyTunnelComponentRunning(this) || !PortGuard.isPortAvailable(Prefs.socksPort)) return
        resetInProgress = false
        connected = false
        lastStatus = null
        activeJoinUrl = ""
        removeJoinFragment()
        setJoinOverlayVisible(false)
        mainFragment()?.onConnectedChanged(false)
        mainFragment()?.onStatusChanged(VpnStatus.CALL_DISCONNECTED)
        val pendingConfig = pendingConnectConfig
        pendingConnectConfig = null
        if (pendingConfig != null) {
            appendLog("Previous session stopped, starting new connection")
            startJoinFor(pendingConfig)
        }
    }

    private fun forceUnlockReset(message: String) {
        resetInProgress = false
        pendingConnectConfig = null
        connected = false
        activeJoinUrl = ""
        lastStatus = if (PortGuard.isPortAvailable(Prefs.socksPort)) VpnStatus.CALL_DISCONNECTED else VpnStatus.PORT_BUSY
        closeActiveHeadlessController()
        removeJoinFragment()
        setJoinOverlayVisible(false)
        TunnelVpnService.requestStop(this)
        ProxyService.requestStop(this)
        HeadlessSessionService.requestStop(this)
        mainFragment()?.onConnectedChanged(false)
        mainFragment()?.onStatusChanged(lastStatus ?: VpnStatus.CALL_DISCONNECTED)
        mainFragment()?.onStatusTextChanged(message)
        appendLog(message)
    }

    private fun closeActiveHeadlessController() {
        val controller = activeHeadlessController
        activeHeadlessController = null
        if (controller != null) {
            thread(name = "headless-shutdown") { controller.close() }
        }
    }

    private fun isResetCurrent(resetId: Long): Boolean =
        resetInProgress && resetGeneration == resetId

    private fun shutdownJoinFragment() {
        val fragment = supportFragmentManager.findFragmentById(R.id.joinOverlayContainer)
        (fragment as? JoinSessionShutdown)?.shutdownSession()
    }

    private fun removeJoinFragment() {
        shutdownJoinFragment()
        if (isDestroyed || supportFragmentManager.isStateSaved) return
        val fragment = supportFragmentManager.findFragmentById(R.id.joinOverlayContainer)
        if (fragment != null) {
            supportFragmentManager.beginTransaction()
                .remove(fragment)
                .commitAllowingStateLoss()
        }
    }

    companion object {
        const val ACTION_AUTO_START = "bypass.whitelist.AUTO_START"
        private const val SUB_PAGE_TAG = "sub_page"
        private const val STATE_CURRENT_TAB_ID = "current_tab_id"
        private const val CALL_LINK = ""
        private const val TAB_MAIN = 0
        private const val TAB_SETTINGS = 1
        private const val TAB_LOGS = 2
    }
}
