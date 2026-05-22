package bypass.whitelist

import android.content.Intent
import android.content.pm.PackageManager
import android.net.VpnService
import android.os.Build
import android.os.Bundle
import android.util.TypedValue
import android.view.View
import android.widget.Button
import android.widget.EditText
import android.widget.ImageButton
import android.widget.LinearLayout
import android.widget.Toast
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
import bypass.whitelist.ui.HeadlessDionFragment
import bypass.whitelist.ui.HeadlessTelemostFragment
import bypass.whitelist.ui.HeadlessWBStreamFragment
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
    private lateinit var joinFragmentContainer: View

    private var defaultLogHeightPx: Int = 0

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
        joinFragmentContainer = findViewById(R.id.joinFragmentContainer)
        defaultLogHeightPx = TypedValue.applyDimension(
            TypedValue.COMPLEX_UNIT_DIP, 120f, resources.displayMetrics
        ).toInt()

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

        val profileSpinner = findViewById<android.widget.Spinner>(R.id.profileSpinner)
        val profiles = arrayOf("Profile 1", "Profile 2", "Profile 3")
        profileSpinner.adapter = android.widget.ArrayAdapter(this, android.R.layout.simple_spinner_dropdown_item, profiles)
        profileSpinner.setSelection(Prefs.currentProfileId)

        profileSpinner.onItemSelectedListener = object : android.widget.AdapterView.OnItemSelectedListener {
            override fun onItemSelected(parent: android.widget.AdapterView<*>?, view: View?, position: Int, id: Long) {
                if (Prefs.currentProfileId != position) {
                    Prefs.currentProfileId = position
                    previousUrl = Prefs.lastUrl
                    urlInput.setText(previousUrl)
                }
            }
            override fun onNothingSelected(parent: android.widget.AdapterView<*>?) {}
        }

        urlInput.addTextChangedListener(object: android.text.TextWatcher {
            override fun afterTextChanged(s: android.text.Editable?) {
                Prefs.lastUrl = s.toString()
            }
            override fun beforeTextChanged(s: CharSequence?, start: Int, count: Int, after: Int) {}
            override fun onTextChanged(s: CharSequence?, start: Int, before: Int, count: Int) {}
        })

        statusCtrl.tunnelMode = Prefs.tunnelMode
        statusCtrl.setIdle()
        logContainer.visibility = if (Prefs.showLogs) View.VISIBLE else View.GONE

        findViewById<Button>(R.id.goButton).setOnClickListener { onGoPressed() }
        findViewById<ImageButton>(R.id.shareLogsButton).setOnClickListener { logCtrl.shareLogs() }
        findViewById<ImageButton>(R.id.copyLogsButton).setOnClickListener { logCtrl.copyLogs() }
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

    private fun setLogFullscreen(fullscreen: Boolean) {
        val logLp = logContainer.layoutParams as LinearLayout.LayoutParams
        val joinLp = joinFragmentContainer.layoutParams as LinearLayout.LayoutParams
        if (fullscreen) {
            logLp.height = 0
            logLp.weight = 1f
            joinLp.height = 0
            joinLp.weight = 0f
            logContainer.visibility = View.VISIBLE
        } else {
            logLp.height = defaultLogHeightPx
            logLp.weight = 0f
            joinLp.height = 0
            joinLp.weight = 1f
            logContainer.visibility = if (Prefs.showLogs) View.VISIBLE else View.GONE
        }
        logContainer.layoutParams = logLp
        joinFragmentContainer.layoutParams = joinLp
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

        val platform = CallPlatform.fromUrl(url)
        if (Prefs.tunnelMode == TunnelMode.DC && (platform == CallPlatform.TELEMOST || platform == CallPlatform.DION || platform == CallPlatform.WBSTREAM)) {
            Prefs.tunnelMode = TunnelMode.VIDEO
            statusCtrl.tunnelMode = TunnelMode.VIDEO
            Toast.makeText(this, R.string.dc_mode_not_supported, Toast.LENGTH_SHORT).show()
        }

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

        val headlessMode = Prefs.headless || platform == CallPlatform.WBSTREAM || platform == CallPlatform.DION
        val fragment = if (headlessMode) {
            when (platform) {
                CallPlatform.VK -> HeadlessVkFragment.newInstance(url)
                CallPlatform.TELEMOST -> HeadlessTelemostFragment.newInstance(url)
                CallPlatform.WBSTREAM -> HeadlessWBStreamFragment.newInstance(url)
                CallPlatform.DION -> HeadlessDionFragment.newInstance(url)
            }
        } else {
            JsHookJoinFragment.newInstance(url)
        }
        setLogFullscreen(headlessMode && platform != CallPlatform.VK)

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
        setLogFullscreen(false)
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
