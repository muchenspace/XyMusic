package com.xymusic.app.core.network

import java.time.Clock
import java.time.Instant
import java.time.ZoneId
import java.util.concurrent.atomic.AtomicLong

class ServerSynchronizedClock private constructor(
    private val systemClock: Clock,
    private val offsetMillis: AtomicLong,
) : Clock() {
    constructor(systemClock: Clock = Clock.systemUTC()) : this(systemClock, AtomicLong())

    fun synchronize(serverEpochMillis: Long) {
        offsetMillis.set(Math.subtractExact(serverEpochMillis, systemClock.millis()))
    }

    override fun getZone(): ZoneId = systemClock.zone

    override fun withZone(zone: ZoneId): Clock = ServerSynchronizedClock(systemClock.withZone(zone), offsetMillis)

    override fun instant(): Instant = Instant.ofEpochMilli(millis())

    override fun millis(): Long = Math.addExact(systemClock.millis(), offsetMillis.get())
}
