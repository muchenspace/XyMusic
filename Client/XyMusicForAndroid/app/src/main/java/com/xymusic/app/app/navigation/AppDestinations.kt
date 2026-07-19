package com.xymusic.app.app.navigation

import androidx.annotation.StringRes
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.AccountCircle
import androidx.compose.material.icons.filled.Home
import androidx.compose.material.icons.outlined.AccountCircle
import androidx.compose.material.icons.outlined.Home
import androidx.compose.ui.graphics.vector.ImageVector
import com.xymusic.app.R
import com.xymusic.app.feature.catalog.presentation.CatalogRouteArgs
import com.xymusic.app.feature.playlist.presentation.PlaylistRouteArgs

sealed class AuthDestination(val route: String) {
    data object Graph : AuthDestination("auth/graph")

    data object Entry : AuthDestination("auth/entry")

    data object SignIn : AuthDestination("auth/sign-in")

    data object Register : AuthDestination("auth/register")

    companion object {
        val screens: List<AuthDestination> =
            listOf(
                Entry,
                SignIn,
                Register,
            )
    }
}

sealed class CatalogDestination(val route: String, val argumentName: String) {
    data object AlbumDetail : CatalogDestination(
        route = "catalog/album/{${CatalogRouteArgs.AlbumId}}",
        argumentName = CatalogRouteArgs.AlbumId,
    ) {
        fun createRoute(albumId: String): String = createDetailRoute("catalog/album", albumId)
    }

    data object ArtistDetail : CatalogDestination(
        route = "catalog/artist/{${CatalogRouteArgs.ArtistId}}",
        argumentName = CatalogRouteArgs.ArtistId,
    ) {
        fun createRoute(artistId: String): String = createDetailRoute("catalog/artist", artistId)
    }

    companion object {
        val screens: List<CatalogDestination> = listOf(AlbumDetail, ArtistDetail)

        private fun createDetailRoute(prefix: String, id: String): String {
            require(id.isNotBlank()) { "Catalog destination ID cannot be blank" }
            require(
                id.all { character ->
                    character.isLetterOrDigit() || character == '-' || character == '_'
                },
            ) { "Catalog destination ID contains unsupported characters" }
            return "$prefix/$id"
        }
    }
}

sealed class PlayerDestination(val route: String) {
    data object NowPlaying : PlayerDestination("player/now-playing")
}

sealed class PlaylistDestination(val route: String) {
    data object Detail : PlaylistDestination(
        "playlist/{${PlaylistRouteArgs.PlaylistId}}",
    ) {
        fun createRoute(playlistId: String): String {
            require(playlistId.isNotBlank()) { "Playlist ID cannot be blank" }
            require(playlistId.all { it.isLetterOrDigit() || it == '-' || it == '_' }) {
                "Playlist ID contains unsupported characters"
            }
            return "playlist/$playlistId"
        }
    }
}

enum class MainDestination(
    val route: String,
    @StringRes val labelRes: Int,
    val selectedIcon: ImageVector,
    val unselectedIcon: ImageVector,
) {
    Home(
        route = "main/home",
        labelRes = R.string.navigation_home,
        selectedIcon = Icons.Filled.Home,
        unselectedIcon = Icons.Outlined.Home,
    ),
    Mine(
        route = "main/mine",
        labelRes = R.string.navigation_mine,
        selectedIcon = Icons.Filled.AccountCircle,
        unselectedIcon = Icons.Outlined.AccountCircle,
    ),
    ;

    companion object {
        fun fromRoute(route: String?): MainDestination? = entries.firstOrNull { destination ->
            route?.substringBefore('?') == destination.route
        }
    }
}

sealed class MainSecondaryDestination(val route: String) {
    data object Library : MainSecondaryDestination("main/library")

    data object Search : MainSecondaryDestination("main/search")

    data object Settings : MainSecondaryDestination("main/settings")

    companion object {
        val screens: List<MainSecondaryDestination> = listOf(Library, Search, Settings)
    }
}
