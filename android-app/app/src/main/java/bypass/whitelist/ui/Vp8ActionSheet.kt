package bypass.whitelist.ui

import android.os.Bundle
import android.view.LayoutInflater
import android.view.View
import android.view.ViewGroup
import android.widget.EditText
import androidx.fragment.app.FragmentManager
import bypass.whitelist.R
import com.google.android.material.bottomsheet.BottomSheetDialogFragment
import com.google.android.material.button.MaterialButton
import com.google.android.material.materialswitch.MaterialSwitch

class Vp8ActionSheet : BottomSheetDialogFragment() {

    private var initialFps: Int = 0
    private var initialBatch: Int = 0
    private var initialDualTrack: Boolean = false
    private var initialReliable: Boolean = false
    private var onSaved: ((fps: Int, batch: Int, dualTrack: Boolean, reliable: Boolean) -> Unit)? = null

    override fun onCreateView(
        inflater: LayoutInflater,
        container: ViewGroup?,
        savedInstanceState: Bundle?,
    ): View = inflater.inflate(R.layout.sheet_action_vp8, container, false)

    override fun onViewCreated(view: View, savedInstanceState: Bundle?) {
        val fps = view.findViewById<EditText>(R.id.vp8FpsInput)
        val batch = view.findViewById<EditText>(R.id.vp8BatchInput)
        val dualSwitch = view.findViewById<MaterialSwitch>(R.id.vp8DualTrackSwitch)
        val reliableSwitch = view.findViewById<MaterialSwitch>(R.id.vp8ReliableSwitch)
        fps.setText(initialFps.toString())
        batch.setText(initialBatch.toString())
        dualSwitch.isChecked = initialDualTrack
        reliableSwitch.isChecked = initialReliable

        view.findViewById<MaterialButton>(R.id.vp8CancelButton).setOnClickListener { dismiss() }
        view.findViewById<MaterialButton>(R.id.vp8SaveButton).setOnClickListener {
            val newFps = fps.text.toString().toIntOrNull()?.takeIf { it in 1..240 } ?: initialFps
            val newBatch = batch.text.toString().toIntOrNull()?.takeIf { it in 1..256 } ?: initialBatch
            onSaved?.invoke(newFps, newBatch, dualSwitch.isChecked, reliableSwitch.isChecked)
            dismiss()
        }
    }

    companion object {
        fun show(
            manager: FragmentManager,
            fps: Int,
            batch: Int,
            dualTrack: Boolean,
            reliable: Boolean,
            onSaved: (fps: Int, batch: Int, dualTrack: Boolean, reliable: Boolean) -> Unit,
        ) {
            Vp8ActionSheet().apply {
                this.initialFps = fps
                this.initialBatch = batch
                this.initialDualTrack = dualTrack
                this.initialReliable = reliable
                this.onSaved = onSaved
            }.show(manager, "Vp8ActionSheet")
        }
    }
}
