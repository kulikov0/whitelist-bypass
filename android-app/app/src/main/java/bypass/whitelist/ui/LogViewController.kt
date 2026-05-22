package bypass.whitelist.ui

import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import android.content.Intent
import android.text.method.ScrollingMovementMethod
import android.widget.TextView
import android.widget.Toast
import androidx.appcompat.app.AppCompatActivity
import androidx.core.content.FileProvider
import bypass.whitelist.R
import bypass.whitelist.util.LogWriter

class LogViewController(
    private val activity: AppCompatActivity,
    private val logView: TextView,
    private val logWriter: LogWriter,
) {
    init {
        logView.movementMethod = ScrollingMovementMethod.getInstance()
    }

    private val pendingLogs = mutableListOf<String>()
    private var isUpdateScheduled = false
    private val handler = android.os.Handler(android.os.Looper.getMainLooper())

    fun append(msg: String) {
        val (line, evicted) = logWriter.append(msg)
        
        synchronized(pendingLogs) {
            if (evicted) {
                pendingLogs.clear()
                pendingLogs.add(logWriter.displayText())
            } else {
                pendingLogs.add("$line\n")
            }
            
            if (!isUpdateScheduled) {
                isUpdateScheduled = true
                handler.postDelayed({ flushLogs() }, 200)
            }
        }
    }

    private fun flushLogs() {
        val textToAppend: String
        val isEvicted: Boolean
        
        synchronized(pendingLogs) {
            if (pendingLogs.isEmpty()) return
            isEvicted = pendingLogs.size == 1 && pendingLogs[0].length > 1000 // Heuristic for evicted
            
            if (isEvicted) {
                textToAppend = pendingLogs[0]
            } else {
                val sb = StringBuilder()
                for (s in pendingLogs) {
                    sb.append(s)
                }
                textToAppend = sb.toString()
            }
            pendingLogs.clear()
            isUpdateScheduled = false
        }
        
        if (isEvicted) {
            logView.text = textToAppend
        } else {
            logView.append(textToAppend)
        }
        
        val scrollAmount = logView.layout?.let {
            it.getLineTop(logView.lineCount) - logView.height
        } ?: 0
        if (scrollAmount > 0) logView.scrollTo(0, scrollAmount)
    }

    fun reset() {
        logWriter.reset()
        logView.text = ""
        logView.scrollTo(0, 0)
    }

    fun copyLogs() {
        val contents = if (logWriter.file.exists()) logWriter.file.readText() else logWriter.displayText()
        val clipboard = activity.getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager
        clipboard.setPrimaryClip(ClipData.newPlainText("relay.log", contents))
        Toast.makeText(activity, R.string.copy_logs_toast, Toast.LENGTH_SHORT).show()
    }

    fun shareLogs() {
        val uri = FileProvider.getUriForFile(
            activity, "${activity.packageName}.fileprovider", logWriter.file
        )
        val share = Intent(Intent.ACTION_SEND).apply {
            type = "text/plain"
            putExtra(Intent.EXTRA_STREAM, uri)
            addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
        }
        activity.startActivity(Intent.createChooser(share, activity.getString(R.string.share_logs)))
    }

    fun close() {
        logWriter.close()
    }
}
