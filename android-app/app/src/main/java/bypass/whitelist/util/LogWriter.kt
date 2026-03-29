package bypass.whitelist.util

import java.io.File
import java.io.FileWriter
import java.text.SimpleDateFormat
import java.util.Date
import java.util.Locale

class LogWriter(cacheDir: File, private val maxDisplayLines: Int = 100) {

    private val logFile = File(cacheDir, "relay.log")
    private var writer: FileWriter? = null
    private val displayLines = ArrayDeque<String>()
    private val dateFormat = SimpleDateFormat("HH:mm:ss.SSS", Locale.US)

    val file: File get() = logFile

    @Synchronized
    fun reset() {
        writer?.close()
        writer = FileWriter(logFile, false)
        displayLines.clear()
    }

    @Synchronized
    fun append(msg: String): AppendResult {
        val ts = dateFormat.format(Date())
        val line = "$ts $msg"
        writer?.apply { write("$line\n"); flush() }
        displayLines.addLast(line)
        val evicted = displayLines.size > maxDisplayLines
        if (evicted) displayLines.removeFirst()
        return AppendResult(line, evicted)
    }

    @Synchronized
    fun displayText(): String = displayLines.joinToString("\n")

    @Synchronized
    fun close() {
        writer?.close()
        writer = null
    }

    data class AppendResult(val line: String, val evicted: Boolean)
}
