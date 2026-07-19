package com.xymusic.app.core.performance

import android.view.Choreographer

class FrameJankMonitor(
    private val slowFrameThresholdNanos: Long,
    private val onSlowFrame: (durationNanos: Long) -> Unit,
) : Choreographer.FrameCallback {
    init {
        require(slowFrameThresholdNanos > 0) { "Slow frame threshold must be positive" }
    }

    private var running = false
    private var previousFrameNanos = 0L

    fun start() {
        if (running) return
        running = true
        previousFrameNanos = 0L
        Choreographer.getInstance().postFrameCallback(this)
    }

    fun stop() {
        running = false
        previousFrameNanos = 0L
        Choreographer.getInstance().removeFrameCallback(this)
    }

    override fun doFrame(frameTimeNanos: Long) {
        if (!running) return
        if (previousFrameNanos != 0L) {
            val duration = frameTimeNanos - previousFrameNanos
            if (duration >= slowFrameThresholdNanos) onSlowFrame(duration)
        }
        previousFrameNanos = frameTimeNanos
        Choreographer.getInstance().postFrameCallback(this)
    }
}
