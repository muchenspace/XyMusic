package com.xymusic.app.feature.search.data

import androidx.paging.ExperimentalPagingApi
import androidx.paging.LoadType
import androidx.paging.PagingState
import androidx.paging.RemoteMediator
import com.xymusic.app.core.data.media.CatalogRemoteMediator
import com.xymusic.app.core.data.media.remote.RemotePage
import com.xymusic.app.core.database.XyMusicDatabase
import com.xymusic.app.core.database.dao.CatalogRemoteKeyDao
import com.xymusic.app.core.database.model.CatalogItemType
import com.xymusic.app.core.network.ServerRuntimeCoordinator
import java.time.Clock

@OptIn(ExperimentalPagingApi::class)
internal class SearchRemoteMediator<Value : Any, RemoteItem : Any>(
    database: XyMusicDatabase,
    remoteKeyDao: CatalogRemoteKeyDao,
    collectionKey: String,
    itemType: CatalogItemType,
    clock: Clock,
    serverRuntimeCoordinator: ServerRuntimeCoordinator,
    itemId: (RemoteItem) -> String,
    loadPage: suspend (cursor: String?) -> RemotePage<RemoteItem>,
    mergeItems: suspend (items: List<RemoteItem>, cachedAtEpochMs: Long) -> Unit,
) : RemoteMediator<Int, Value>() {
    private val delegate =
        CatalogRemoteMediator<Value, RemoteItem>(
            database = database,
            remoteKeyDao = remoteKeyDao,
            collectionKey = collectionKey,
            itemType = itemType,
            clock = clock,
            serverRuntimeCoordinator = serverRuntimeCoordinator,
            itemId = itemId,
            loadPage = { cursor, _ -> loadPage(cursor) },
            mergeItems = mergeItems,
        )

    override suspend fun load(loadType: LoadType, state: PagingState<Int, Value>): MediatorResult =
        delegate.load(loadType, state)
}
