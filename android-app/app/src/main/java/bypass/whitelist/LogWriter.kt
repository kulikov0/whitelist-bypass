package bypass.whitelist

import java.io.File
import java.io.FileWriter
import java.text.SimpleDateFormat
import java.util.ArrayDeque
import java.util.Locale

class LogWriter(cacheDir: File, private val maxDisplayLines: Int = 100) {

    val file: File get() = logFile

    private val logFile = File(cacheDir, "tunnel.log")
    private var writer: FileWriter? = null
    private val recentLines = ArrayDeque<String>(maxDisplayLines)
    private val timeFmt = SimpleDateFormat("HH:mm:ss.SSS", Locale.US)

    @Synchronized
    fun reset() {
        writer?.close()
        writer = FileWriter(logFile, false)
        recentLines.clear()
    }

    @Synchronized
    fun append(msg: String): AppendResult {
        val line = "${timeFmt.format(System.currentTimeMillis())} ${msg.replace("[HOOK] ", "")}"
        writer?.appendLine(line)
        writer?.flush()
        val evicted = recentLines.size >= maxDisplayLines
        if (evicted) recentLines.pollFirst()
        recentLines.addLast(line)
        return AppendResult(line, evicted)
    }

    @Synchronized
    fun displayText(): String {
        return recentLines.joinToString("\n")
    }

    @Synchronized
    fun close() {
        writer?.close()
        writer = null
    }

    data class AppendResult(val line: String, val evicted: Boolean)
}