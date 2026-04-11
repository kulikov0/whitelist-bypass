package bypass.whitelist.tunnel

import android.util.Log
import bypass.whitelist.util.Ports
import mobile.LogCallback
import mobile.Mobile
import java.io.BufferedWriter
import java.io.File
import java.io.OutputStreamWriter
import java.net.Inet4Address
import java.net.InetAddress

class RelayController(
    private val nativeLibDir: String,
    private val onLog: (String) -> Unit,
    private val onStatus: (VpnStatus) -> Unit,
) {
    private var dcThread: Thread? = null
    private var pionThread: Thread? = null
    private var pionProcess: Process? = null

    @Volatile
    var isRunning = false
        private set

    @Synchronized
    fun start(mode: TunnelMode, platform: CallPlatform) {
        stop()
        isRunning = true
        if (mode.isPion) startPion(mode, platform) else startDc()
    }

    @Synchronized
    fun stop() {
        isRunning = false

        pionProcess?.let {
            it.destroy()
            it.waitFor()
        }
        pionProcess = null
        pionThread?.interrupt()
        pionThread = null

        Mobile.stopJoiner()
        dcThread?.interrupt()
        dcThread = null
    }

    private fun startDc() {
        val cb = LogCallback { msg ->
            onLog(msg)
            if (msg.contains("browser connected")) onStatus(VpnStatus.TUNNEL_ACTIVE)
            else if (msg.contains("ws read error")) onStatus(VpnStatus.TUNNEL_LOST)
        }
        dcThread = Thread {
            try {
                Mobile.startJoiner(Ports.DC_WS, Ports.SOCKS, cb)
            } catch (e: Exception) {
                if (isRunning) onLog("Relay error: ${e.message}")
            }
        }.also { it.start() }
        onLog("Relay started DC mode (SOCKS5 :${Ports.SOCKS}, WS :${Ports.DC_WS})")
    }

    private fun startPion(mode: TunnelMode, platform: CallPlatform) {
        val relayBin = File(nativeLibDir, "librelay.so")
        if (!relayBin.exists()) {
            onLog("Pion relay binary not found")
            return
        }
        val relayMode = mode.relayMode(platform)
        pionThread = Thread {
            try {
                val pb = ProcessBuilder(
                    relayBin.absolutePath,
                    "--mode", relayMode,
                    "--ws-port", "${Ports.PION_SIGNALING}",
                    "--socks-port", "${Ports.SOCKS}"
                )
                pb.redirectErrorStream(true)
                val proc = pb.start()
                synchronized(this) { pionProcess = proc }
                onLog("Pion relay started mode=$relayMode (signaling :${Ports.PION_SIGNALING}, SOCKS5 :${Ports.SOCKS})")
                val stdinWriter = BufferedWriter(OutputStreamWriter(proc.outputStream))
                proc.inputStream.bufferedReader().forEachLine { line ->
                    if (line.startsWith("RESOLVE:")) {
                        val hostname = line.removePrefix("RESOLVE:")
                        try {
                            val all = InetAddress.getAllByName(hostname)
                            val address = all.firstOrNull { it is Inet4Address } ?: all.first()
                            val resolvedIP = address.hostAddress ?: ""
                            Log.d("RELAY", "Resolved $hostname -> $resolvedIP")
                            stdinWriter.write(resolvedIP)
                            stdinWriter.newLine()
                            stdinWriter.flush()
                        } catch (e: Exception) {
                            Log.e("RELAY", "DNS resolve failed for $hostname", e)
                            stdinWriter.write("")
                            stdinWriter.newLine()
                            stdinWriter.flush()
                        }
                    } else {
                        Log.d("RELAY", line)
                        onLog(line)
                        if (line.contains("CONNECTED")) onStatus(VpnStatus.TUNNEL_ACTIVE)
                        else if (line.contains("session cleaned up")) onStatus(VpnStatus.TUNNEL_LOST)
                    }
                }
                onLog("Pion relay exited: ${proc.exitValue()}")
            } catch (e: Exception) {
                if (isRunning) {
                    Log.e("RELAY", "Pion relay error", e)
                    onLog("Pion relay error: ${e.message}")
                }
            }
        }.also { it.start() }
    }
}
