package com.xymusic.app.feature.catalog.data

import androidx.room.withTransaction
import com.xymusic.app.core.database.XyMusicDatabase
import javax.inject.Inject
import javax.inject.Singleton

interface CatalogTransactionRunner {
    suspend fun <T> run(block: suspend () -> T): T
}

@Singleton
class RoomCatalogTransactionRunner
@Inject
constructor(private val database: XyMusicDatabase) :
    CatalogTransactionRunner {
    override suspend fun <T> run(block: suspend () -> T): T = database.withTransaction(block)
}
