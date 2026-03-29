package bypass.whitelist.ui

import android.content.Intent
import android.widget.TextView
import androidx.appcompat.app.AppCompatActivity
import androidx.core.content.FileProvider
import bypass.whitelist.R
import bypass.whitelist.util.LogWriter

class LogViewController(
    private val activity: AppCompatActivity,
    private val logView: TextView,
    private val logWriter: LogWriter,
) {
    fun append(msg: String) {
        val (line, evicted) = logWriter.append(msg)
        activity.runOnUiThread {
            if (evicted) {
                logView.text = logWriter.displayText()
            } else {
                logView.append("$line\n")
            }
            val scrollAmount = logView.layout?.let {
                it.getLineTop(logView.lineCount) - logView.height
            } ?: 0
            if (scrollAmount > 0) logView.scrollTo(0, scrollAmount)
        }
    }

    fun reset() {
        logWriter.reset()
        logView.text = ""
        logView.scrollTo(0, 0)
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
