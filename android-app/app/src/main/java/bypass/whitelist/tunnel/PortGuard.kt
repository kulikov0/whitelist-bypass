package bypass.whitelist.tunnel

import android.os.Process
import android.util.Log
import java.io.File
import java.net.InetAddress
import java.net.ServerSocket

object PortGuard {

    private const val TAG = "PortGuard"

    fun ensurePortFree(port: Long): Boolean {
        if (tryBind(port)) return true
        Log.w(TAG, "Port $port is busy, killing occupying process")
        killByPort(port)
        for (attempt in 1..20) {
            Thread.sleep(100)
            if (tryBind(port)) {
                Log.i(TAG, "Port $port freed after kill (attempt $attempt)")
                return true
            }
        }
        Log.e(TAG, "Port $port still busy after kill attempt")
        return false
    }

    private fun tryBind(port: Long): Boolean {
        return try {
            ServerSocket(port.toInt(), 1, InetAddress.getByName("127.0.0.1")).use { true }
        } catch (e: Exception) {
            false
        }
    }

    private fun killByPort(port: Long) {
        try {
            val hexPort = String.format("%04X", port)
            val tcp = File("/proc/net/tcp")
            if (!tcp.canRead()) {
                Log.w(TAG, "Cannot read /proc/net/tcp")
                return
            }
            val inodes = mutableSetOf<String>()
            tcp.forEachLine { line ->
                val fields = line.trim().split("\\s+".toRegex())
                if (fields.size >= 10) {
                    val localPort = fields[1].substringAfter(":")
                    if (localPort.equals(hexPort, ignoreCase = true)) {
                        inodes.add(fields[9])
                    }
                }
            }
            if (inodes.isEmpty()) {
                Log.w(TAG, "No inode found for port $port")
                return
            }
            val myPid = Process.myPid()
            val procDir = File("/proc")
            for (entry in procDir.listFiles() ?: emptyArray()) {
                val pid = entry.name.toIntOrNull() ?: continue
                if (pid == myPid) continue
                val fdDir = File(entry, "fd")
                if (!fdDir.canRead()) continue
                for (fd in fdDir.listFiles() ?: emptyArray()) {
                    try {
                        val target = fd.canonicalPath
                        for (inode in inodes) {
                            if (target.contains("socket:[$inode]")) {
                                Log.w(TAG, "Killing PID $pid holding port $port (inode $inode)")
                                Process.killProcess(pid)
                                return
                            }
                        }
                    } catch (ignored: Exception) {
                    }
                }
            }
            Log.w(TAG, "Could not find PID for port $port via /proc, trying fuser")
            try {
                val fuser = Runtime.getRuntime().exec(arrayOf("fuser", "$port/tcp"))
                val output = fuser.inputStream.bufferedReader().readText().trim()
                fuser.waitFor()
                for (token in output.split("\\s+".toRegex())) {
                    val pid = token.toIntOrNull() ?: continue
                    if (pid != myPid) {
                        Log.w(TAG, "fuser: killing PID $pid for port $port")
                        Process.killProcess(pid)
                        return
                    }
                }
            } catch (e: Exception) {
                Log.w(TAG, "fuser fallback failed: ${e.message}")
            }
        } catch (e: Exception) {
            Log.e(TAG, "killByPort error: ${e.message}")
        }
    }
}
