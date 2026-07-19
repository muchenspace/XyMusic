package com.xymusic.app.app.navigation

import com.google.common.truth.Truth.assertThat
import org.junit.Test

class MainNavigationTest {
    @Test
    fun bottomBarIsVisibleOnlyForPrimaryDestinations() {
        assertThat(shouldShowMainBottomBar(MainDestination.Home.route)).isTrue()
        assertThat(shouldShowMainBottomBar(MainDestination.Mine.route)).isTrue()

        MainSecondaryDestination.screens.forEach { destination ->
            assertThat(shouldShowMainBottomBar(destination.route)).isFalse()
        }
        assertThat(shouldShowMainBottomBar("main/library?libraryTab=History")).isFalse()
        assertThat(shouldShowMainBottomBar(PlayerDestination.NowPlaying.route)).isFalse()
        assertThat(shouldShowMainBottomBar(PlaylistDestination.Detail.route)).isFalse()
        assertThat(shouldShowMainBottomBar(CatalogDestination.AlbumDetail.route)).isFalse()
        assertThat(shouldShowMainBottomBar(CatalogDestination.ArtistDetail.route)).isFalse()
        assertThat(shouldShowMainBottomBar(null)).isFalse()
    }
}
