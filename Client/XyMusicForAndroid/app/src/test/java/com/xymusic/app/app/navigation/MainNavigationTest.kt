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

    @Test
    fun routeContentLayoutMatchesDestinationType() {
        assertThat(mainNavigationContentLayout(MainDestination.Home.route))
            .isEqualTo(MainNavigationContentLayout.Primary)
        assertThat(mainNavigationContentLayout(MainDestination.Mine.route))
            .isEqualTo(MainNavigationContentLayout.Primary)
        assertThat(mainNavigationContentLayout(PlayerDestination.NowPlaying.route))
            .isEqualTo(MainNavigationContentLayout.FullScreen)
        assertThat(mainNavigationContentLayout(PlaylistDestination.Detail.route))
            .isEqualTo(MainNavigationContentLayout.EdgeToEdge)
        assertThat(mainNavigationContentLayout(MainSecondaryDestination.Search.route))
            .isEqualTo(MainNavigationContentLayout.Secondary)
        assertThat(mainNavigationContentLayout(CatalogDestination.AlbumDetail.route))
            .isEqualTo(MainNavigationContentLayout.Secondary)
        assertThat(mainNavigationContentLayout(null))
            .isEqualTo(MainNavigationContentLayout.Secondary)
    }

    @Test
    fun chromeTargetsCurrentRouteAndRetainsTheLastMainSelection() {
        val config =
            MainNavigationLayoutConfig(
                useNavigationRail = false,
                compactPlayerBar = false,
                hasPlayerItem = true,
            )

        assertThat(
            mainNavigationChromeState(
                config = config,
                currentRoute = PlayerDestination.NowPlaying.route,
                lastSelectedMainDestination = MainDestination.Home,
            ),
        ).isEqualTo(
            MainNavigationChromeState(
                showMainNavigation = false,
                showMiniPlayer = false,
                selectedMainDestination = MainDestination.Home,
                isPlayerDestination = true,
            ),
        )
        assertThat(
            mainNavigationChromeState(
                config = config,
                currentRoute = PlaylistDestination.Detail.route,
                lastSelectedMainDestination = MainDestination.Home,
            ),
        ).isEqualTo(
            MainNavigationChromeState(
                showMainNavigation = false,
                showMiniPlayer = true,
                selectedMainDestination = MainDestination.Home,
                isPlayerDestination = false,
            ),
        )
        assertThat(
            mainNavigationChromeState(
                config = config,
                currentRoute = MainDestination.Mine.route,
                lastSelectedMainDestination = MainDestination.Home,
            ),
        ).isEqualTo(
            MainNavigationChromeState(
                showMainNavigation = true,
                showMiniPlayer = true,
                selectedMainDestination = MainDestination.Mine,
                isPlayerDestination = false,
            ),
        )
    }
}
