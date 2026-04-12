package bypass.whitelist

import android.content.Intent
import android.content.pm.PackageManager
import android.net.VpnService
import android.os.Build
import android.os.Bundle
import android.view.View
import android.widget.Button
import android.widget.EditText
import android.widget.ImageButton
import androidx.activity.enableEdgeToEdge
import androidx.activity.result.contract.ActivityResultContracts
import androidx.appcompat.app.AppCompatActivity
import androidx.core.view.ViewCompat
import androidx.core.view.WindowInsetsCompat
import bypass.whitelist.tunnel.CallPlatform
import bypass.whitelist.tunnel.TunnelMode
import bypass.whitelist.tunnel.ProxyService
import bypass.whitelist.tunnel.TunnelVpnService
import bypass.whitelist.tunnel.VpnStatus
import bypass.whitelist.ui.HeadlessTelemostFragment
import bypass.whitelist.ui.HeadlessVkFragment
import bypass.whitelist.ui.JoinFragmentHost
import bypass.whitelist.ui.JsHookJoinFragment
import bypass.whitelist.ui.LogViewController
import bypass.whitelist.ui.SettingsDialogFragment
import bypass.whitelist.ui.StatusBarController
import bypass.whitelist.util.LogWriter
import bypass.whitelist.util.Prefs
import bypass.whitelist.util.hideKeyboard
import bypass.whitelist.util.maskUrl

class MainActivity : AppCompatActivity(), SettingsDialogFragment.Listener, JoinFragmentHost {

    private val logWriter by lazy { LogWriter(cacheDir) }

    private lateinit var urlInput: EditText
    private lateinit var logCtrl: LogViewController
    private lateinit var statusCtrl: StatusBarController
    private lateinit var logContainer: View

    private var previousUrl = ""

    private val vpnPrepLauncher = registerForActivityResult(
        ActivityResultContracts.StartActivityForResult()
    ) {}

    private val vpnLauncher = registerForActivityResult(
        ActivityResultContracts.StartActivityForResult()
    ) { result ->
        if (result.resultCode == RESULT_OK) startVpnService()
        else logCtrl.append("VPN permission denied")
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        setContentView(R.layout.activity_main)
        ViewCompat.setOnApplyWindowInsetsListener(findViewById(R.id.main)) { v, insets ->
            val systemBars = insets.getInsets(WindowInsetsCompat.Type.systemBars())
            v.setPadding(systemBars.left, systemBars.top, systemBars.right, systemBars.bottom)
            insets
        }

        urlInput = findViewById(R.id.urlInput)
        logContainer = findViewById(R.id.logContainer)

        logCtrl = LogViewController(this, findViewById(R.id.logView), logWriter)
        logCtrl.reset()

        statusCtrl = StatusBarController(
            activity = this,
            statusBar = findViewById(R.id.statusBar),
            goButton = findViewById(R.id.goButton),
            onConnect = ::onGoPressed,
            onDisconnect = ::fullReset,
        )

        previousUrl = Prefs.lastUrl
        urlInput.setText(previousUrl)
        statusCtrl.tunnelMode = Prefs.tunnelMode
        statusCtrl.setIdle()
        logContainer.visibility = if (Prefs.showLogs) View.VISIBLE else View.GONE

        findViewById<Button>(R.id.goButton).setOnClickListener { onGoPressed() }
        findViewById<ImageButton>(R.id.shareLogsButton).setOnClickListener { logCtrl.shareLogs() }
        findViewById<ImageButton>(R.id.gearButton).setOnClickListener {
            SettingsDialogFragment().show(supportFragmentManager, SettingsDialogFragment.TAG)
        }
        findViewById<View>(R.id.clearButton).setOnClickListener { urlInput.setText("") }

        TunnelVpnService.onDisconnect = { runOnUiThread { resetState() } }
        ProxyService.onDisconnect = { runOnUiThread { resetState() } }

        VpnService.prepare(this)?.let { vpnPrepLauncher.launch(it) }
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU &&
            checkSelfPermission(android.Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED
        ) {
            requestPermissions(arrayOf(android.Manifest.permission.POST_NOTIFICATIONS), 0)
        }

        if (CALL_LINK.isNotEmpty()) {
            urlInput.setText(CALL_LINK)
            onGoPressed()
        } else if (Prefs.connectOnStart && previousUrl.isNotEmpty()) {
            onGoPressed()
        }
    }

    override fun onDestroy() {
        TunnelVpnService.onDisconnect = null
        TunnelVpnService.instance?.stop()
        ProxyService.onDisconnect = null
        ProxyService.instance?.stop()
        logCtrl.close()
        super.onDestroy()
    }

    override fun onTunnelModeChanged(mode: TunnelMode) {
        statusCtrl.tunnelMode = mode
        fullReset()
    }

    override fun onShowLogsChanged(visible: Boolean) {
        logContainer.visibility = if (visible) View.VISIBLE else View.GONE
    }

    override fun onShareLogs() {
        logCtrl.shareLogs()
    }

    override fun onReset() {
        fullReset()
    }

    override fun appendLog(message: String) {
        logCtrl.append(message)
    }

    override fun onJoinStatusText(text: String) {
        runOnUiThread { statusCtrl.setStatusText(text) }
    }

    override fun onJoinStatus(status: VpnStatus) {
        TunnelVpnService.instance?.updateStatus(status)
        ProxyService.instance?.updateStatus(status)
        runOnUiThread {
            statusCtrl.setStatus(status)
            if (status == VpnStatus.TUNNEL_ACTIVE) statusCtrl.setConnected(true)
        }
    }

    override fun requestVpn() {
        if (Prefs.proxyOnly) {
            logCtrl.append("Proxy only mode, skipping VPN")
            startService(Intent(this, ProxyService::class.java))
            onJoinStatus(VpnStatus.TUNNEL_ACTIVE)
            return
        }
        val intent = VpnService.prepare(this)
        if (intent != null) vpnLauncher.launch(intent) else startVpnService()
    }

    private fun onGoPressed() {
        val url = urlInput.text.toString().trim()
        if (url.isEmpty()) return

        logCtrl.reset()
        hideKeyboard()
        urlInput.clearFocus()
        statusCtrl.setConnected(false)
        statusCtrl.setStatus(VpnStatus.CONNECTING)
        logCtrl.append("Loading: ${maskUrl(url)}")

        if (previousUrl != url) {
            previousUrl = url
            Prefs.lastUrl = url
        }

        val fragment = if (Prefs.headless) {
            when (CallPlatform.fromUrl(url)) {
                CallPlatform.VK -> HeadlessVkFragment.newInstance(url)
                CallPlatform.TELEMOST -> HeadlessTelemostFragment.newInstance(url)
            }
        } else {
            JsHookJoinFragment.newInstance(url)
        }

        supportFragmentManager.beginTransaction()
            .replace(R.id.joinFragmentContainer, fragment)
            .commit()
    }

    private fun startVpnService() {
        startService(Intent(this, TunnelVpnService::class.java))
        logCtrl.append("VPN started")
        onJoinStatus(VpnStatus.TUNNEL_ACTIVE)
    }

    private fun resetState() {
        removeJoinFragment()
        logCtrl.reset()
        statusCtrl.setConnected(false)
        statusCtrl.setIdle()
    }

    private fun fullReset() {
        resetState()
        TunnelVpnService.instance?.stop()
        ProxyService.instance?.stop()
    }

    private fun removeJoinFragment() {
        val fragment = supportFragmentManager.findFragmentById(R.id.joinFragmentContainer)
        if (fragment != null) {
            supportFragmentManager.beginTransaction()
                .remove(fragment)
                .commitNowAllowingStateLoss()
        }
    }

    companion object {
        private const val CALL_LINK = "" // Open call page on app start (do not delete - I need it for debug)
    }
}
