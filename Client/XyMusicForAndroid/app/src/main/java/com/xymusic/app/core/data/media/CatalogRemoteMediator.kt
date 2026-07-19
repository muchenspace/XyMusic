package com.xymusic.app.core.data.media

import androidx.paging.ExperimentalPagingApi
import androidx.paging.LoadType
import androidx.paging.PagingState
import androidx.paging.RemoteMediator
import androidx.room.withTransaction
import com.xymusic.app.core.data.media.remote.RemotePage
import com.xymusic.app.core.database.XyMusicDatabase
import com.xymusic.app.core.database.dao.CatalogRemoteKeyDao
import com.xymusic.app.core.database.entity.CatalogRemoteKeyEntity
import com.xymusic.app.core.database.model.CatalogItemType
import com.xymusic.app.core.network.ServerRuntimeCoordinator
import java.time.Clock
import java.util.concurrent.CancellationException

@OptIn(ExperimentalPagingApi::class)
internal class CatalogRemoteMediator<Value : Any, RemoteItem : Any>(
    private val database: XyMusicDatabase,
    private val remoteKeyDao: CatalogRemoteKeyDao,
    private val collectionKey: String,
    private val itemType: CatalogItemType,
    private val clock: Clock,
    private val serverRuntimeCoordinator: ServerRuntimeCoordinator,
    private val itemId: (RemoteItem) -> String,
    private val loadPage: suspend (cursor: String?, limit: Int) -> RemotePage<RemoteItem>,
    private val mergeItems: suspend (items: List<RemoteItem>, cachedAtEpochMs: Long) -> Unit,
) : RemoteMediator<Int, Value>() {
    override suspend fun load(loadType: LoadType, state: PagingState<Int, Value>): MediatorResult {
        if (loadType == LoadType.PREPEND) return MediatorResult.Success(endOfPaginationReached = true)

        return try {
            val generation = serverRuntimeCoordinator.captureGeneration()
            val boundary =
                when (loadType) {
                    LoadType.REFRESH -> PageBoundary(cursor = null, firstPosition = 0L)
                    LoadType.APPEND ->
                        appendBoundary() ?: return MediatorResult.Success(
                            endOfPaginationReached = true,
                        )
                    LoadType.PREPEND -> error("PREPEND handled above")
                }
            val limit =
                when (loadType) {
                    LoadType.REFRESH -> state.config.initialLoadSize
                    else -> state.config.pageSize
                }.coerceIn(1, MAX_PAGE_SIZE)
            val page = loadPage(boundary.cursor, limit)
            validateCatalogPage(page, boundary.cursor, itemId)
            val itemIds = page.items.map(itemId)
            val refreshedAt = clock.millis()
            val keys =
                itemIds.mapIndexed { index, id ->
                    CatalogRemoteKeyEntity(
                        collectionKey = collectionKey,
                        itemType = itemType,
                        itemId = id,
                        position = boundary.firstPosition + index,
                        previousCursor = boundary.cursor,
                        nextCursor = page.nextCursor,
                        refreshedAtEpochMs = refreshedAt,
                    )
                }

            database.withTransaction {
                serverRuntimeCoordinator.requireCurrent(generation)
                mergeItems(page.items, refreshedAt)
                when (loadType) {
                    LoadType.REFRESH -> remoteKeyDao.replace(collectionKey, itemType, keys)
                    LoadType.APPEND ->
                        if (keys.isNotEmpty()) {
                            remoteKeyDao.append(collectionKey, itemType, keys)
                        } else {
                            remoteKeyDao.markEndOfPagination(
                                collectionKey = collectionKey,
                                itemType = itemType,
                                expectedCursor = requireNotNull(boundary.cursor),
                                expectedLastPosition = boundary.firstPosition - 1L,
                                refreshedAtEpochMs = refreshedAt,
                            )
                        }
                    LoadType.PREPEND -> error("PREPEND handled above")
                }
            }

            MediatorResult.Success(endOfPaginationReached = page.nextCursor == null)
        } catch (failure: CancellationException) {
            throw failure
        } catch (failure: Exception) {
            MediatorResult.Error(failure)
        }
    }

    private suspend fun appendBoundary(): PageBoundary? {
        val lastKey = remoteKeyDao.lastKey(collectionKey, itemType) ?: return null
        val nextCursor = lastKey.nextCursor ?: return null
        return PageBoundary(cursor = nextCursor, firstPosition = lastKey.position + 1L)
    }

    private data class PageBoundary(val cursor: String?, val firstPosition: Long)

    private companion object {
        const val MAX_PAGE_SIZE = 100
    }
}
