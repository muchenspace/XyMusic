package com.xymusic.app.app.navigation

import com.google.common.truth.Truth.assertThat
import org.junit.Test

class AppDestinationsTest {
    @Test
    fun mainDestinationRoutesAreUnique() {
        val routes = MainDestination.entries.map(MainDestination::route)

        assertThat(routes).containsNoDuplicates()
        assertThat(routes)
            .containsExactly(
                "main/home",
                "main/mine",
            ).inOrder()
    }

    @Test
    fun routeLookupReturnsMatchingDestination() {
        assertThat(MainDestination.fromRoute(MainDestination.Mine.route))
            .isEqualTo(MainDestination.Mine)
        assertThat(MainDestination.fromRoute("unknown")).isNull()
        assertThat(MainDestination.fromRoute(null)).isNull()
    }

    @Test
    fun authenticationRoutesAreSeparateFromMainRoutes() {
        val authRoutes =
            AuthDestination.screens.map(AuthDestination::route).toSet() +
                AuthDestination.Graph.route
        val mainRoutes =
            MainDestination.entries.map(MainDestination::route).toSet() +
                MainSecondaryDestination.screens.map(MainSecondaryDestination::route)

        assertThat(authRoutes.intersect(mainRoutes)).isEmpty()
    }

    @Test
    fun secondaryMainRoutesRemainUniqueAndSeparateFromBottomDestinations() {
        val secondaryRoutes = MainSecondaryDestination.screens.map(MainSecondaryDestination::route)
        val mainRoutes = MainDestination.entries.map(MainDestination::route)

        assertThat(secondaryRoutes)
            .containsExactly(
                "main/library",
                "main/search",
                "main/settings",
            ).inOrder()
        assertThat(secondaryRoutes).containsNoDuplicates()
        assertThat(secondaryRoutes.intersect(mainRoutes.toSet())).isEmpty()
    }

    @Test
    fun routeLookupIgnoresArgumentsOnPrimaryDestinations() {
        assertThat(MainDestination.fromRoute("main/mine?source=profile"))
            .isEqualTo(MainDestination.Mine)
        assertThat(MainDestination.fromRoute("main/library?libraryTab=History")).isNull()
    }

    @Test
    fun authenticationRoutesAreUniqueAndContainNoPersonalDataArguments() {
        val routes = AuthDestination.screens.map(AuthDestination::route)

        assertThat(routes).hasSize(3)
        assertThat(routes).containsNoDuplicates()
        assertThat(routes).containsExactly(
            "auth/entry",
            "auth/sign-in",
            "auth/register",
        )
        routes.forEach { route ->
            assertThat(route).doesNotContain("?")
            assertThat(route).doesNotContain("{")
            assertThat(route).doesNotContain("}")
            assertThat(route).doesNotContain("@")
        }
    }

    @Test
    fun catalogDetailRoutesAreUniqueAndUseOnlyResourceIdArguments() {
        val routes = CatalogDestination.screens.map(CatalogDestination::route)

        assertThat(routes).containsExactly(
            "catalog/album/{albumId}",
            "catalog/artist/{artistId}",
        )
        assertThat(routes).containsNoDuplicates()
        assertThat(CatalogDestination.AlbumDetail.createRoute("album-1"))
            .isEqualTo("catalog/album/album-1")
        assertThat(CatalogDestination.ArtistDetail.createRoute("artist-1"))
            .isEqualTo("catalog/artist/artist-1")
    }

    @Test
    fun detailRoutesRejectBlankAndUnsafeResourceIds() {
        listOf(
            { CatalogDestination.AlbumDetail.createRoute("") },
            { CatalogDestination.AlbumDetail.createRoute("album/1") },
            { CatalogDestination.ArtistDetail.createRoute("artist?1") },
            { PlaylistDestination.Detail.createRoute("playlist 1") },
        ).forEach { createRoute ->
            val result = runCatching(createRoute)

            assertThat(result.exceptionOrNull()).isInstanceOf(IllegalArgumentException::class.java)
        }
    }
}
