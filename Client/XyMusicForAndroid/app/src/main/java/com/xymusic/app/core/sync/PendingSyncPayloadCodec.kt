package com.xymusic.app.core.sync

import com.xymusic.app.core.database.entity.PendingSyncOperationEntity
import javax.inject.Inject
import kotlinx.serialization.DeserializationStrategy
import kotlinx.serialization.SerializationStrategy
import kotlinx.serialization.json.Json

class PendingSyncPayloadCodec
@Inject
constructor(private val json: Json) {
    fun <T> decode(operation: PendingSyncOperationEntity, deserializer: DeserializationStrategy<T>): T =
        json.decodeFromString(
            deserializer,
            requireNotNull(operation.requestPayloadJson) { "Pending payload is missing" },
        )

    fun <T> encode(serializer: SerializationStrategy<T>, payload: T): String = json.encodeToString(serializer, payload)
}
