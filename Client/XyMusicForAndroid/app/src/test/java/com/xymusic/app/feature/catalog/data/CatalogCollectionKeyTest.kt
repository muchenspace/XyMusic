package com.xymusic.app.feature.catalog.data

import com.google.common.truth.Truth.assertThat
import com.xymusic.app.feature.catalog.domain.model.AlbumQuery
import com.xymusic.app.feature.catalog.domain.model.ArtistQuery
import com.xymusic.app.feature.catalog.domain.model.TrackQuery
import com.xymusic.app.feature.catalog.domain.model.TrackSort
import org.junit.Test

class CatalogCollectionKeyTest {
    @Test
    fun everyFilterAndSortContributesToTrackCollectionIdentity() {
        val base = TrackQuery().collectionKey()
        val byArtist = TrackQuery(artistId = ARTIST_ID).collectionKey()
        val byAlbum =
            TrackQuery(albumId = ALBUM_ID, sort = TrackSort.ALBUM_ORDER_ASC)
                .collectionKey()

        assertThat(setOf(base, byArtist, byAlbum)).hasSize(3)
    }

    @Test(expected = IllegalArgumentException::class)
    fun albumOrderRequiresAlbumFilter() {
        TrackQuery(sort = TrackSort.ALBUM_ORDER_ASC)
    }

    @Test
    fun collectionFamiliesCannotCollide() {
        assertThat(TrackQuery().collectionKey()).isNotEqualTo(ArtistQuery().collectionKey())
        assertThat(ArtistQuery().collectionKey()).isNotEqualTo(AlbumQuery().collectionKey())
    }

    private companion object {
        const val ARTIST_ID = "11111111-1111-4111-8111-111111111111"
        const val ALBUM_ID = "22222222-2222-4222-8222-222222222222"
    }
}
