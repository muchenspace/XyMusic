package com.xymusic.app.data.network.auth

import java.util.UUID
import javax.inject.Inject

fun interface IdempotencyKeyGenerator {
    fun generate(): String
}

class UuidIdempotencyKeyGenerator
@Inject
constructor() : IdempotencyKeyGenerator {
    override fun generate(): String = UUID.randomUUID().toString()
}
