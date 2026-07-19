package com.xymusic.app.feature.player.data.media

import android.app.Application
import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
import com.google.common.truth.Truth.assertThat
import com.xymusic.app.core.database.XyMusicDatabase
import com.xymusic.app.core.database.entity.OfflineTrackEntity
import dagger.Lazy
import kotlinx.coroutines.test.runTest
import org.junit.After
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34], application = Application::class)
class OfflineMediaStoreTest {
    private lateinit var database: XyMusicDatabase
    private lateinit var cache: FakeOfflineMediaCache
    private lateinit var store: OfflineMediaStore

    @Before
    fun setUp() {
        database =
            Room
                .inMemoryDatabaseBuilder(
                    ApplicationProvider.getApplicationContext(),
                    XyMusicDatabase::class.java,
                ).allowMainThreadQueries()
                .build()
        cache = FakeOfflineMediaCache()
        store = OfflineMediaStore(database.offlineTrackDao(), Lazy { cache })
    }

    @After
    fun tearDown() {
        database.close()
    }

    @Test
    fun removingOneOwnerKeepsSharedCachedMediaForTheOtherOwner() = runTest {
        val alice = offlineTrack("alice", "track", "shared-cache")
        val bob = alice.copy(ownerUserId = "bob")
        database.offlineTrackDao().upsert(alice)
        database.offlineTrackDao().upsert(bob)
        cache.cachedKeys += alice.cacheKey

        assertThat(store.remove("alice", alice.trackId)).isTrue()

        assertThat(cache.removedKeys).isEmpty()
        assertThat(database.offlineTrackDao().track("alice", alice.trackId)).isNull()
        assertThat(database.offlineTrackDao().track("bob", bob.trackId)).isEqualTo(bob)

        assertThat(store.remove("bob", bob.trackId)).isTrue()

        assertThat(cache.removedKeys).containsExactly(alice.cacheKey)
        assertThat(database.offlineTrackDao().track("bob", bob.trackId)).isNull()
    }

    @Test
    fun clearingAccountRemovesOnlyCacheKeysExclusiveToThatAccount() = runTest {
        val sharedAlice = offlineTrack("alice", "shared-track", "shared-cache")
        val sharedBob = sharedAlice.copy(ownerUserId = "bob")
        val aliceOnly = offlineTrack("alice", "alice-track", "alice-cache")
        database.offlineTrackDao().upsert(sharedAlice)
        database.offlineTrackDao().upsert(sharedBob)
        database.offlineTrackDao().upsert(aliceOnly)
        cache.cachedKeys += setOf(sharedAlice.cacheKey, aliceOnly.cacheKey)

        assertThat(store.clear("alice")).isEqualTo(2)

        assertThat(cache.removedKeys).containsExactly(aliceOnly.cacheKey)
        assertThat(database.offlineTrackDao().tracks("alice")).isEmpty()
        assertThat(database.offlineTrackDao().tracks("bob")).containsExactly(sharedBob)
        assertThat(store.playableTrack("bob", sharedBob.trackId)).isEqualTo(sharedBob)
    }

    @Test
    fun oneFailedDownloadDoesNotRemoveCacheUsedByAnotherActiveDownload() = runTest {
        cache.cachedKeys += "shared-cache"
        val failedClaim = store.createDownloadClaim("shared-cache")
        val activeClaim = store.createDownloadClaim("shared-cache")
        store.beginDownload(failedClaim)
        store.beginDownload(activeClaim)

        store.discardUncommitted(failedClaim)

        assertThat(cache.removedKeys).isEmpty()
        assertThat(cache.cachedKeys).contains("shared-cache")

        store.discardUncommitted(activeClaim)

        assertThat(cache.removedKeys).containsExactly("shared-cache")
    }

    @Test
    fun discardingTheSameClaimTwiceDoesNotReleaseAnotherActiveDownload() = runTest {
        cache.cachedKeys += "shared-cache"
        val firstClaim = store.createDownloadClaim("shared-cache")
        val secondClaim = store.createDownloadClaim("shared-cache")
        store.beginDownload(firstClaim)
        store.beginDownload(secondClaim)

        store.discardUncommitted(firstClaim)
        store.discardUncommitted(firstClaim)

        assertThat(cache.removedKeys).isEmpty()
        assertThat(cache.pinnedKeys).contains("shared-cache")

        store.discardUncommitted(secondClaim)

        assertThat(cache.removedKeys).containsExactly("shared-cache")
    }

    @Test
    fun committedDownloadPromotesItsPinAndPersistsTheOwner() = runTest {
        val track = offlineTrack("alice", "track", "cache")
        cache.cachedKeys += track.cacheKey
        val claim = store.createDownloadClaim(track.cacheKey)
        store.beginDownload(claim)

        assertThat(store.commit(track, claim)).isTrue()

        assertThat(cache.pinnedKeys).doesNotContain(track.cacheKey)
        assertThat(cache.persistentPinnedKeys).contains(track.cacheKey)
        assertThat(database.offlineTrackDao().track("alice", track.trackId)).isEqualTo(track)
    }

    @Test
    fun clearingOneOwnerDuringAnotherOwnersDownloadKeepsSharedMedia() = runTest {
        val alice = offlineTrack("alice", "track", "shared-cache")
        val bob = alice.copy(ownerUserId = "bob")
        database.offlineTrackDao().upsert(alice)
        cache.cachedKeys += alice.cacheKey
        val bobClaim = store.createDownloadClaim(bob.cacheKey)
        store.beginDownload(bobClaim)

        assertThat(store.clear("alice")).isEqualTo(1)
        assertThat(store.commit(bob, bobClaim)).isTrue()

        assertThat(cache.removedKeys).isEmpty()
        assertThat(cache.cachedKeys).contains(alice.cacheKey)
        assertThat(database.offlineTrackDao().track("alice", alice.trackId)).isNull()
        assertThat(database.offlineTrackDao().track("bob", bob.trackId)).isEqualTo(bob)
    }

    @Test
    fun clearingOwnerWithoutDownloadsDoesNotInitializeTheMediaCache() = runTest {
        var cacheRequested = false
        val lazyStore =
            OfflineMediaStore(
                database.offlineTrackDao(),
                Lazy {
                    cacheRequested = true
                    cache
                },
            )

        assertThat(lazyStore.clear("alice")).isEqualTo(0)

        assertThat(cacheRequested).isFalse()
    }

    private fun offlineTrack(ownerUserId: String, trackId: String, cacheKey: String) = OfflineTrackEntity(
        ownerUserId = ownerUserId,
        trackId = trackId,
        title = trackId,
        artistNamesJson = "[]",
        albumTitle = null,
        artworkUrl = null,
        artworkCacheKey = null,
        durationMs = 1_000,
        cacheKey = cacheKey,
        contentLength = 128,
        downloadedAtEpochMs = 1,
    )

    private class FakeOfflineMediaCache : OfflineMediaCache {
        val cachedKeys = mutableSetOf<String>()
        val removedKeys = mutableListOf<String>()
        val pinnedKeys = mutableSetOf<String>()
        val persistentPinnedKeys = mutableSetOf<String>()

        override fun pin(cacheKey: String) {
            pinnedKeys += cacheKey
        }

        override fun unpin(cacheKey: String) {
            pinnedKeys -= cacheKey
        }

        override fun promotePin(cacheKey: String) {
            persistentPinnedKeys += cacheKey
        }

        override fun isFullyCached(cacheKey: String, contentLength: Long): Boolean =
            contentLength > 0 && cacheKey in cachedKeys

        override suspend fun remove(cacheKey: String) {
            removedKeys += cacheKey
            cachedKeys -= cacheKey
            pinnedKeys -= cacheKey
            persistentPinnedKeys -= cacheKey
        }
    }
}
