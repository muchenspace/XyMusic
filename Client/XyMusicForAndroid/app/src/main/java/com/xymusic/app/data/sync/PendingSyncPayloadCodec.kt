package com.xymusic.app.data.sync

import com.xymusic.app.core.database.entity.PendingSyncOperationEntity
import kotlinx.serialization.DeserializationStrategy
import kotlinx.serialization.SerializationStrategy
import kotlinx.serialization.json.Json

internal class PendingSyncPayloadCodec(private val json: Json) {
    fun <T> decode(operation: PendingSyncOperationEntity, deserializer: DeserializationStrategy<T>): T =
        json.decodeFromString(
            deserializer,
            requireNotNull(operation.requestPayloadJson) { "Pending payload is missing" },
        )

    fun <T> encode(serializer: SerializationStrategy<T>, payload: T): String = json.encodeToString(serializer, payload)
}
