package com.xymusic.app.core.performance

import android.os.Handler
import android.os.HandlerThread
import android.view.FrameMetrics
import android.view.Window

class FrameJankMonitor(
    private val window: Window,
    private val frameBudgetNanos: Long,
    private val onSlowFrame: (durationNanos: Long) -> Unit,
) : Window.OnFrameMetricsAvailableListener {
    init {
        require(frameBudgetNanos > 0) { "Frame budget must be positive" }
    }

    @Volatile
    private var running = false
    private var callbackThread: HandlerThread? = null
    private var callbackHandler: Handler? = null

    @Synchronized
    fun start() {
        if (running) return

        val thread = HandlerThread("XyMusicFrameMetrics").also { it.start() }
        val handler = Handler(thread.looper)
        callbackThread = thread
        callbackHandler = handler
        running = true
        window.addOnFrameMetricsAvailableListener(this, handler)
    }

    @Synchronized
    fun stop() {
        if (!running) return
        running = false
        window.removeOnFrameMetricsAvailableListener(this)
        callbackHandler?.removeCallbacksAndMessages(null)
        callbackThread?.quitSafely()
        callbackHandler = null
        callbackThread = null
    }

    override fun onFrameMetricsAvailable(
        window: Window,
        frameMetrics: FrameMetrics,
        dropCountSinceLastInvocation: Int,
    ) {
        if (!running) return
        val duration = frameMetrics.getMetric(FrameMetrics.TOTAL_DURATION)
        if (isFrameOverBudget(duration, frameBudgetNanos)) onSlowFrame(duration)
    }
}

internal fun isFrameOverBudget(frameDurationNanos: Long, frameBudgetNanos: Long): Boolean =
    frameDurationNanos > frameBudgetNanos
