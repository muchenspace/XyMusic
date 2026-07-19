package com.xymusic.app.core.database

import com.xymusic.app.core.database.dao.AccountDataDao
import com.xymusic.app.core.database.dao.AccountDataDeletion
import dagger.Lazy
import javax.inject.Inject
import javax.inject.Singleton

interface AccountDataCleaner {
    suspend fun clear(ownerUserId: String): AccountDataDeletion
}

fun interface OfflineAccountDataCleaner {
    suspend fun clear(ownerUserId: String): Int
}

@Singleton
class RoomAccountDataCleaner
@Inject
constructor(
    private val accountDataDao: AccountDataDao,
    private val offlineAccountDataCleaner: Lazy<OfflineAccountDataCleaner>,
) : AccountDataCleaner {
    override suspend fun clear(ownerUserId: String): AccountDataDeletion {
        val offlineTrackCount = offlineAccountDataCleaner.get().clear(ownerUserId)
        return accountDataDao.deletePrivateData(ownerUserId).copy(
            offlineTrackCount = offlineTrackCount,
        )
    }
}
