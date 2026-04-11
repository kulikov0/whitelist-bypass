package bypass.whitelist.tunnel

import android.util.Log
import bypass.whitelist.util.Ports
import java.io.BufferedWriter
import java.io.File
import java.io.OutputStreamWriter
import java.net.Inet4Address
import java.net.InetAddress

class HeadlessRelayController(
    private val nativeLibDir: String,
    private val relayMode: String = "vk-headless-joiner",
    private val onLog: (String) -> Unit,
    private val onStatus: (VpnStatus) -> Unit,
) {
    private var process: Process? = null
    private var thread: Thread? = null
    private var stdinWriter: BufferedWriter? = null

    @Volatile
    var isRunning = false
        private set

    fun start() {
        stop()
        isRunning = true

        val relayBin = File(nativeLibDir, "librelay.so")
        if (!relayBin.exists()) {
            onLog("Relay binary not found")
            return
        }

        thread = Thread {
            try {
                val processBuilder = ProcessBuilder(
                    relayBin.absolutePath,
                    "--mode", relayMode,
                    "--ws-port", "${Ports.PION_SIGNALING}",
                    "--socks-port", "${Ports.SOCKS}"
                )
                processBuilder.redirectErrorStream(true)
                val proc = processBuilder.start()
                synchronized(this) {
                    process = proc
                    stdinWriter = BufferedWriter(OutputStreamWriter(proc.outputStream))
                }
                onLog("Headless relay started (signaling :${Ports.PION_SIGNALING}, SOCKS5 :${Ports.SOCKS})")

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
                            status == "READY" -> onStatus(VpnStatus.STARTING)
                            status == "CONNECTING" -> onStatus(VpnStatus.CONNECTING)
                            status == "TUNNEL_CONNECTED" -> onStatus(VpnStatus.TUNNEL_ACTIVE)
                            status == "TUNNEL_LOST" -> onStatus(VpnStatus.TUNNEL_LOST)
                            status.startsWith("ERROR:") -> onStatus(VpnStatus.CALL_FAILED)
                        }
                    } else {
                        Log.d("RELAY", line)
                        onLog(line)
                    }
                }
                proc.waitFor()
                Log.d("RELAY", "Headless relay exited: ${proc.exitValue()}")
            } catch (e: Exception) {
                if (isRunning) {
                    Log.e("RELAY", "Headless relay error", e)
                    onLog("Relay error: ${e.message}")
                }
            }
        }.also { it.start() }
    }

    fun sendJoinParams(joinJson: String) {
        writeStdin("JOIN:$joinJson")
    }

    @Synchronized
    fun stop() {
        isRunning = false
        process?.let {
            it.destroy()
            it.waitFor()
        }
        process = null
        stdinWriter = null
        thread?.interrupt()
        thread = null
    }

    @Synchronized
    private fun writeStdin(line: String) {
        try {
            stdinWriter?.write(line)
            stdinWriter?.newLine()
            stdinWriter?.flush()
        } catch (e: Exception) {
            Log.e("RELAY", "writeStdin error: ${e.message}")
        }
    }
}
