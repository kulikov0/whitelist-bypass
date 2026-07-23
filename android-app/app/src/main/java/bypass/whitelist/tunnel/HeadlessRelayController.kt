package bypass.whitelist.tunnel

import android.os.Build
import android.util.Log
import bypass.whitelist.util.ParamCallback
import bypass.whitelist.util.Ports
import bypass.whitelist.util.Prefs
import bypass.whitelist.util.SocksAuth
import org.json.JSONObject
import java.io.BufferedWriter
import java.io.File
import java.io.OutputStreamWriter
import java.net.Inet4Address
import java.net.InetAddress
import java.util.concurrent.TimeUnit

class HeadlessRelayController(
    private val nativeLibDir: String,
    private val relayMode: String = "vk-headless-joiner",
    private val onLog: ParamCallback<String>,
    private val onStatus: ParamCallback<VpnStatus>,
    private val onCaptchaUrl: ParamCallback<String>? = null,
) {
    private var process: Process? = null
    private var thread: Thread? = null
    private var stdinWriter: BufferedWriter? = null
    private val pendingCommands = mutableListOf<String>()

    @Volatile
    var isRunning = false
        private set

    fun start() {
        stop()
        isRunning = true

        val relayBin = File(nativeLibDir, "librelay.so")
        if (!relayBin.exists()) {
            onLog.invoke("Relay binary not found")
            return
        }

        thread = Thread {
            try {
                val socksPort = Prefs.socksPort
                if (!PortGuard.ensurePortFree(socksPort)) {
                    onLog.invoke("SOCKS5 port $socksPort is busy and could not be freed")
                    onStatus.invoke(VpnStatus.PORT_BUSY)
                    isRunning = false
                    return@Thread
                }
            } catch (e: Exception) {
                onLog.invoke("Port check error: ${e.message}")
                isRunning = false
                return@Thread
            }
            try {
                val processBuilder = ProcessBuilder(
                    relayBin.absolutePath,
                    "--mode", relayMode,
                    "--ws-port", "${Ports.PION_SIGNALING}",
                    "--socks-host", Prefs.socksHost,
                    "--socks-port", "${Prefs.socksPort}",
                    "--socks-user", SocksAuth.user,
                    "--socks-pass", SocksAuth.pass
                )
                processBuilder.redirectErrorStream(true)
                val proc = processBuilder.start()
                synchronized(this) {
                    process = proc
                    stdinWriter = BufferedWriter(OutputStreamWriter(proc.outputStream))
                    pendingCommands.forEach { writeStdin(it) }
                    pendingCommands.clear()
                }
                onLog.invoke("Headless relay started (signaling :${Ports.PION_SIGNALING}, SOCKS5 ${SocksAuth.user}:${SocksAuth.pass}@${Prefs.socksHost}:${Prefs.socksPort})")

                proc.inputStream.bufferedReader().forEachLine { line ->
                    if (line.startsWith("RESOLVE:")) {
                        val hostname = line.removePrefix("RESOLVE:")
                        try {
                            val all = InetAddress.getAllByName(hostname)
                            val address = all.firstOrNull { it is Inet4Address } ?: all.first()
                            val resolvedIP = address.hostAddress ?: ""
                            Log.d("RELAY", "Resolved $hostname -> $resolvedIP")
                            writeStdin(resolvedIP)
                        } catch (e: Exception) {
                            Log.e("RELAY", "DNS resolve failed for $hostname", e)
                            writeStdin("")
                        }
                    } else if (line.startsWith("STATUS:")) {
                        val status = line.removePrefix("STATUS:")
                        Log.d("RELAY", "status: $status")
                        when {
                            status == "READY" -> onStatus.invoke(VpnStatus.STARTING)
                            status == "CONNECTING" -> onStatus.invoke(VpnStatus.CONNECTING)
                            status == "RECONNECTING" -> onStatus.invoke(VpnStatus.CONNECTING)
                            status == "TUNNEL_CONNECTED" -> onStatus.invoke(VpnStatus.TUNNEL_ACTIVE)
                            status == "TUNNEL_LOST" -> onStatus.invoke(VpnStatus.TUNNEL_LOST)
                            status.startsWith("CAPTCHA:") -> {
                                val captchaUrl = status.removePrefix("CAPTCHA:")
                                onStatus.invoke(VpnStatus.ACTION_REQUIRED_CAPTCHA)
                                onCaptchaUrl?.invoke(captchaUrl)
                            }
                            status.startsWith("ERROR:") -> {
                                onLog.invoke("Headless relay error: $status")
                                onStatus.invoke(VpnStatus.CALL_FAILED)
                            }
                        }
                    } else {
                        Log.d("RELAY", line)
                        onLog.invoke(line)
                    }
                }
                proc.waitFor()
                Log.d("RELAY", "Headless relay exited: ${proc.exitValue()}")
                if (isRunning) {
                    onStatus.invoke(VpnStatus.CALL_FAILED)
                    isRunning = false
                }
            } catch (e: Exception) {
                if (isRunning) {
                    Log.e("RELAY", "Headless relay error", e)
                    onLog.invoke("Relay error: ${e.message}")
                    onStatus.invoke(VpnStatus.CALL_FAILED)
                    isRunning = false
                }
            }
        }.also { it.start() }
    }

    fun sendJoinParams(joinJson: String) {
        writeStdin("JOIN:$joinJson")
    }

    fun sendAuth(joinLink: String, displayName: String, tunnelMode: String) {
        val json = JSONObject().apply {
            put("joinLink", joinLink)
            put("displayName", displayName)
            put("tunnelMode", tunnelMode)
            put("vp8Fps", Prefs.activeVp8Fps)
            put("vp8Batch", Prefs.activeVp8Batch)
            put("dualTrack", Prefs.activeDualTrack)
            put("reliable", Prefs.activeReliable)
        }
        writeStdin("AUTH:$json")
    }

    @Synchronized
    fun stop() {
        isRunning = false
        try { stdinWriter?.close() } catch (_: Exception) {}
        stdinWriter = null
        val activeProcess = process
        process = null
        activeProcess?.destroy()
        if (activeProcess != null) {
            try {
                if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
                    if (!activeProcess.waitFor(1500, TimeUnit.MILLISECONDS)) {
                        activeProcess.destroyForcibly()
                        activeProcess.waitFor(500, TimeUnit.MILLISECONDS)
                    }
                } else {
                    activeProcess.waitFor()
                }
            } catch (_: Exception) {
            }
        }
        val activeThread = thread
        activeThread?.interrupt()
        if (activeThread != null && activeThread !== Thread.currentThread()) {
            try {
                activeThread.join(500)
            } catch (_: Exception) {
            }
        }
        thread = null
    }

    @Synchronized
    private fun writeStdin(line: String) {
        if (stdinWriter == null) {
            pendingCommands.add(line)
            return
        }
        try {
            stdinWriter?.write(line)
            stdinWriter?.newLine()
            stdinWriter?.flush()
        } catch (e: Exception) {
            Log.e("RELAY", "writeStdin error: ${e.message}")
        }
    }
}
