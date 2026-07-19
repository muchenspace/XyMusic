package com.xymusic.app.data.network

import com.google.common.truth.Truth.assertThat
import com.xymusic.app.app.di.NetworkModule
import com.xymusic.app.core.data.media.remote.RandomCatalogRequestDto
import com.xymusic.app.core.network.ServerRuntimeCoordinator
import com.xymusic.app.core.security.AccessToken
import com.xymusic.app.core.security.RefreshToken
import com.xymusic.app.core.security.SessionTokens
import com.xymusic.app.feature.auth.data.remote.DeviceInfoDto
import com.xymusic.app.feature.auth.data.remote.LoginRequestDto
import com.xymusic.app.feature.auth.data.remote.PublicAuthApi
import com.xymusic.app.feature.auth.data.remote.RegisterRequestDto
import com.xymusic.app.feature.auth.data.remote.SessionAuthApi
import com.xymusic.app.feature.catalog.data.remote.CatalogApi
import com.xymusic.app.feature.library.data.remote.LibraryApi
import com.xymusic.app.feature.library.data.remote.RecordPlaybackRequestDto as LibraryRecordPlaybackRequestDto
import com.xymusic.app.feature.player.data.remote.PlaybackApi
import com.xymusic.app.feature.player.data.remote.PlaybackRequestDto
import com.xymusic.app.feature.player.data.remote.RecordPlaybackRequestDto as PlayerRecordPlaybackRequestDto
import com.xymusic.app.feature.playlist.data.remote.AddPlaylistTrackRequestDto
import com.xymusic.app.feature.playlist.data.remote.CreatePlaylistRequestDto
import com.xymusic.app.feature.playlist.data.remote.PlaylistApi
import com.xymusic.app.feature.playlist.data.remote.ReorderPlaylistRequestDto
import com.xymusic.app.feature.search.data.remote.SearchApi
import com.xymusic.app.feature.settings.data.remote.CompleteAvatarUploadRequestDto
import com.xymusic.app.feature.settings.data.remote.CreateAvatarUploadRequestDto
import com.xymusic.app.feature.settings.data.remote.ProfileApi
import com.xymusic.app.support.InMemoryServerConfigRepository
import com.xymusic.app.support.InMemoryTokenVault
import java.util.concurrent.TimeUnit
import kotlinx.coroutines.test.runTest
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.buildJsonArray
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import okhttp3.OkHttpClient
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import okhttp3.mockwebserver.RecordedRequest
import org.junit.After
import org.junit.Before
import org.junit.Test

class RetrofitApiContractTest {
    private lateinit var server: MockWebServer
    private lateinit var json: Json

    @Before
    fun setUp() {
        server = MockWebServer()
        server.start()
        json = NetworkModule.provideJson()
    }

    @After
    fun tearDown() {
        server.shutdown()
    }

    @Test
    fun publicRetrofitApiSurfaceIsExplicitlyLocked() {
        assertApiSurface(PublicAuthApi::class.java, "register", "login")
        assertApiSurface(SessionAuthApi::class.java, "logout")
        assertApiSurface(
            CatalogApi::class.java,
            "tracks",
            "randomTracks",
            "track",
            "artists",
            "artist",
            "albums",
            "randomAlbums",
            "album",
        )
        assertApiSurface(
            LibraryApi::class.java,
            "favorites",
            "addFavorite",
            "removeFavorite",
            "history",
            "recordPlayback",
        )
        assertApiSurface(PlaybackApi::class.java, "grant", "recordHistory")
        assertApiSurface(SearchApi::class.java, "search")
        assertApiSurface(
            PlaylistApi::class.java,
            "playlists",
            "create",
            "playlist",
            "update",
            "delete",
            "addTrack",
            "removeTrack",
            "reorder",
        )
        assertApiSurface(
            ProfileApi::class.java,
            "currentUser",
            "updateCurrentUser",
            "createAvatarUpload",
            "completeAvatarUpload",
            "logoutAllSessions",
        )
    }

    @Test
    fun authApiContracts() = runTest {
        val fixture = protectedFixture()
        val publicApi = NetworkModule.providePublicAuthApi(server.url("/"), publicClient(), json)
        val sessionApi = NetworkModule.provideSessionAuthApi(server.url("/"), fixture.client, json)

        val register =
            request {
                publicApi.register(RegisterRequestDto(username = "alice_01", password = "password-123"))
            }
        assertPublicRequest(register, method = "POST", path = "/api/v1/auth/register")
        assertJsonBody(
            register,
            buildJsonObject {
                put("username", "alice_01")
                put("password", "password-123")
            },
        )

        val login =
            request {
                publicApi.login(
                    LoginRequestDto(
                        username = "alice_01",
                        password = "password-123",
                        device =
                        DeviceInfoDto(
                            installationId = "installation-1",
                            name = "Contract phone",
                            platform = "ANDROID",
                            appVersion = "0.1.0",
                        ),
                    ),
                )
            }
        assertPublicRequest(login, method = "POST", path = "/api/v1/auth/login")
        assertJsonBody(
            login,
            buildJsonObject {
                put("username", "alice_01")
                put("password", "password-123")
                put(
                    "device",
                    buildJsonObject {
                        put("installationId", "installation-1")
                        put("name", "Contract phone")
                        put("platform", "ANDROID")
                        put("appVersion", "0.1.0")
                    },
                )
            },
        )

        val logout = request { sessionApi.logout(fixture.sessionContext) }
        assertProtectedRequest(logout, method = "POST", path = "/api/v1/auth/logout")
    }

    @Test
    fun catalogApiContracts() = runTest {
        val fixture = protectedFixture()
        val api = NetworkModule.provideCatalogApi(server.url("/"), fixture.client, json)

        val tracks =
            request {
                api.tracks(
                    cursor = "track-cursor",
                    limit = 25,
                    artistId = "artist-1",
                    albumId = "album-1",
                    sort = "title",
                )
            }
        assertProtectedRequest(tracks, method = "GET", path = "/api/v1/tracks")
        assertQuery(
            tracks,
            "cursor" to "track-cursor",
            "limit" to "25",
            "artistId" to "artist-1",
            "albumId" to "album-1",
            "sort" to "title",
        )

        val randomTracks = request { api.randomTracks(RandomCatalogRequestDto(limit = 9)) }
        assertProtectedRequest(randomTracks, method = "POST", path = "/api/v1/tracks/random")
        assertJsonBody(randomTracks, buildJsonObject { put("limit", 9) })

        val track = request { api.track("track-1") }
        assertProtectedRequest(track, method = "GET", path = "/api/v1/tracks/track-1")
        assertQuery(track, "lyricPage" to "1", "lyricPageSize" to "20")

        val artists = request { api.artists(cursor = "artist-cursor", limit = 20, sort = "name") }
        assertProtectedRequest(artists, method = "GET", path = "/api/v1/artists")
        assertQuery(artists, "cursor" to "artist-cursor", "limit" to "20", "sort" to "name")

        val artist = request { api.artist("artist-1") }
        assertProtectedRequest(artist, method = "GET", path = "/api/v1/artists/artist-1")

        val albums =
            request {
                api.albums(
                    cursor = "album-cursor",
                    limit = 15,
                    artistId = "artist-1",
                    sort = "releaseDate",
                )
            }
        assertProtectedRequest(albums, method = "GET", path = "/api/v1/albums")
        assertQuery(
            albums,
            "cursor" to "album-cursor",
            "limit" to "15",
            "artistId" to "artist-1",
            "sort" to "releaseDate",
        )

        val randomAlbums = request { api.randomAlbums(RandomCatalogRequestDto(limit = 7)) }
        assertProtectedRequest(randomAlbums, method = "POST", path = "/api/v1/albums/random")
        assertJsonBody(randomAlbums, buildJsonObject { put("limit", 7) })

        val album = request { api.album("album-1") }
        assertProtectedRequest(album, method = "GET", path = "/api/v1/albums/album-1")
    }

    @Test
    fun playbackAndSearchApiContracts() = runTest {
        val fixture = protectedFixture()
        val playbackApi = NetworkModule.providePlaybackApi(server.url("/"), fixture.client, json)
        val searchApi = NetworkModule.provideSearchApi(server.url("/"), fixture.client, json)

        val grant =
            request {
                playbackApi.grant(
                    trackId = "track-1",
                    request = PlaybackRequestDto(preferredQuality = "LOSSLESS", acceptedCodecs = listOf("flac", "aac")),
                )
            }
        assertProtectedRequest(grant, method = "POST", path = "/api/v1/tracks/track-1/playback")
        assertJsonBody(
            grant,
            buildJsonObject {
                put("preferredQuality", "LOSSLESS")
                put(
                    "acceptedCodecs",
                    buildJsonArray {
                        add(JsonPrimitive("flac"))
                        add(JsonPrimitive("aac"))
                    },
                )
            },
        )

        val history =
            request {
                playbackApi.recordHistory(
                    trackId = "track-1",
                    idempotencyKey = "playback-history-key",
                    request =
                    PlayerRecordPlaybackRequestDto(
                        playbackSessionId = "playback-session-1",
                        positionMs = 12_345,
                        occurredAt = "2026-07-17T00:00:00Z",
                        event = "PROGRESS",
                    ),
                )
            }
        assertProtectedRequest(
            history,
            method = "PUT",
            path = "/api/v1/library/history/track-1",
            idempotencyKey = "playback-history-key",
        )
        assertPlaybackBody(history)

        val search =
            request {
                searchApi.search(
                    query = "hello world",
                    scope = "ALL",
                    cursor = "search-cursor",
                    limit = 30,
                )
            }
        assertProtectedRequest(search, method = "GET", path = "/api/v1/search")
        assertQuery(
            search,
            "q" to "hello world",
            "scope" to "ALL",
            "cursor" to "search-cursor",
            "limit" to "30",
        )
    }

    @Test
    fun libraryApiContracts() = runTest {
        val fixture = protectedFixture()
        val api = NetworkModule.provideLibraryApi(server.url("/"), fixture.client, json)

        val favorites = request { api.favorites(cursor = "favorite-cursor", limit = 40, sort = "addedAt") }
        assertProtectedRequest(favorites, method = "GET", path = "/api/v1/library/favorites")
        assertQuery(favorites, "cursor" to "favorite-cursor", "limit" to "40", "sort" to "addedAt")

        val addFavorite = request { api.addFavorite("track-1") }
        assertProtectedRequest(addFavorite, method = "PUT", path = "/api/v1/library/favorites/track-1")

        val removeFavorite = request { api.removeFavorite("track-1") }
        assertProtectedRequest(removeFavorite, method = "DELETE", path = "/api/v1/library/favorites/track-1")

        val history = request { api.history(cursor = "history-cursor", limit = 50) }
        assertProtectedRequest(history, method = "GET", path = "/api/v1/library/history")
        assertQuery(history, "cursor" to "history-cursor", "limit" to "50")

        val record =
            request {
                api.recordPlayback(
                    trackId = "track-1",
                    idempotencyKey = "library-history-key",
                    request =
                    LibraryRecordPlaybackRequestDto(
                        playbackSessionId = "playback-session-1",
                        positionMs = 12_345,
                        occurredAt = "2026-07-17T00:00:00Z",
                        event = "PROGRESS",
                    ),
                )
            }
        assertProtectedRequest(
            record,
            method = "PUT",
            path = "/api/v1/library/history/track-1",
            idempotencyKey = "library-history-key",
        )
        assertPlaybackBody(record)
    }

    @Test
    fun playlistApiContracts() = runTest {
        val fixture = protectedFixture()
        val api = NetworkModule.providePlaylistApi(server.url("/"), fixture.client, json)

        val playlists = request { api.playlists(cursor = "playlist-cursor", limit = 25, sort = "updatedAt") }
        assertProtectedRequest(playlists, method = "GET", path = "/api/v1/playlists")
        assertQuery(playlists, "cursor" to "playlist-cursor", "limit" to "25", "sort" to "updatedAt")

        val create =
            request {
                api.create(
                    idempotencyKey = "playlist-create-key",
                    request = CreatePlaylistRequestDto("Road trip", "Driving", "PRIVATE"),
                )
            }
        assertProtectedRequest(
            create,
            method = "POST",
            path = "/api/v1/playlists",
            idempotencyKey = "playlist-create-key",
        )
        assertJsonBody(
            create,
            buildJsonObject {
                put("name", "Road trip")
                put("description", "Driving")
                put("visibility", "PRIVATE")
            },
        )

        val playlist = request { api.playlist("playlist-1", cursor = "entry-cursor", limit = 60) }
        assertProtectedRequest(playlist, method = "GET", path = "/api/v1/playlists/playlist-1")
        assertQuery(playlist, "cursor" to "entry-cursor", "limit" to "60")

        val updateBody =
            buildJsonObject {
                put("name", "Updated road trip")
                put("expectedVersion", 3)
            }
        val update = request { api.update("playlist-1", "playlist-update-key", updateBody) }
        assertProtectedRequest(
            update,
            method = "PATCH",
            path = "/api/v1/playlists/playlist-1",
            idempotencyKey = "playlist-update-key",
        )
        assertJsonBody(update, updateBody)

        val delete = request { api.delete("playlist-1", expectedVersion = 4, idempotencyKey = "playlist-delete-key") }
        assertProtectedRequest(
            delete,
            method = "DELETE",
            path = "/api/v1/playlists/playlist-1",
            idempotencyKey = "playlist-delete-key",
        )
        assertQuery(delete, "expectedVersion" to "4")

        val addTrack =
            request {
                api.addTrack(
                    playlistId = "playlist-1",
                    idempotencyKey = "playlist-add-key",
                    request =
                    AddPlaylistTrackRequestDto(
                        expectedVersion = 4,
                        trackId = "track-1",
                        insertAfterEntryId = "entry-1",
                    ),
                )
            }
        assertProtectedRequest(
            addTrack,
            method = "POST",
            path = "/api/v1/playlists/playlist-1/tracks",
            idempotencyKey = "playlist-add-key",
        )
        assertJsonBody(
            addTrack,
            buildJsonObject {
                put("expectedVersion", 4)
                put("trackId", "track-1")
                put("insertAfterEntryId", "entry-1")
            },
        )

        val removeTrack =
            request {
                api.removeTrack(
                    playlistId = "playlist-1",
                    entryId = "entry-1",
                    expectedVersion = 5,
                    idempotencyKey = "playlist-remove-key",
                )
            }
        assertProtectedRequest(
            removeTrack,
            method = "DELETE",
            path = "/api/v1/playlists/playlist-1/tracks/entry-1",
            idempotencyKey = "playlist-remove-key",
        )
        assertQuery(removeTrack, "expectedVersion" to "5")

        val reorder =
            request {
                api.reorder(
                    playlistId = "playlist-1",
                    idempotencyKey = "playlist-reorder-key",
                    request =
                    ReorderPlaylistRequestDto(
                        expectedVersion = 6,
                        orderedEntryIds = listOf("entry-2", "entry-1"),
                    ),
                )
            }
        assertProtectedRequest(
            reorder,
            method = "PATCH",
            path = "/api/v1/playlists/playlist-1/tracks/order",
            idempotencyKey = "playlist-reorder-key",
        )
        assertJsonBody(
            reorder,
            buildJsonObject {
                put("expectedVersion", 6)
                put(
                    "orderedEntryIds",
                    buildJsonArray {
                        add(JsonPrimitive("entry-2"))
                        add(JsonPrimitive("entry-1"))
                    },
                )
            },
        )
    }

    @Test
    fun profileApiContracts() = runTest {
        val fixture = protectedFixture()
        val api = NetworkModule.provideProfileApi(server.url("/"), fixture.client, json)

        val current = request { api.currentUser() }
        assertProtectedRequest(current, method = "GET", path = "/api/v1/users/me")

        val updateBody =
            buildJsonObject {
                put("displayName", "Contract user")
                put("bio", "API contract")
                put("expectedVersion", 2)
            }
        val update = request { api.updateCurrentUser("profile-update-key", updateBody) }
        assertProtectedRequest(
            update,
            method = "PATCH",
            path = "/api/v1/users/me",
            idempotencyKey = "profile-update-key",
        )
        assertJsonBody(update, updateBody)

        val createUpload =
            request {
                api.createAvatarUpload(
                    idempotencyKey = "avatar-create-key",
                    request =
                    CreateAvatarUploadRequestDto(
                        fileName = "avatar.jpg",
                        contentType = "image/jpeg",
                        sizeBytes = 12_345,
                        checksumSha256 = "checksum",
                    ),
                )
            }
        assertProtectedRequest(
            createUpload,
            method = "POST",
            path = "/api/v1/users/me/avatar/uploads",
            idempotencyKey = "avatar-create-key",
        )
        assertJsonBody(
            createUpload,
            buildJsonObject {
                put("fileName", "avatar.jpg")
                put("contentType", "image/jpeg")
                put("sizeBytes", 12_345)
                put("checksumSha256", "checksum")
            },
        )

        val completeUpload =
            request {
                api.completeAvatarUpload(
                    uploadId = "upload-1",
                    idempotencyKey = "avatar-complete-key",
                    request = CompleteAvatarUploadRequestDto(observedEtag = "etag-1"),
                )
            }
        assertProtectedRequest(
            completeUpload,
            method = "POST",
            path = "/api/v1/users/me/avatar/uploads/upload-1/complete",
            idempotencyKey = "avatar-complete-key",
        )
        assertJsonBody(completeUpload, buildJsonObject { put("observedEtag", "etag-1") })

        val logoutAll = request { api.logoutAllSessions() }
        assertProtectedRequest(logoutAll, method = "POST", path = "/api/v1/auth/logout-all")
    }

    private suspend fun <T> request(call: suspend () -> T): RecordedRequest {
        server.enqueue(MockResponse().setResponseCode(204))
        call()
        return checkNotNull(server.takeRequest(5, TimeUnit.SECONDS))
    }

    private fun assertPublicRequest(request: RecordedRequest, method: String, path: String) {
        assertThat(request.method).isEqualTo(method)
        assertThat(request.requestUrl?.encodedPath).isEqualTo(path)
        assertThat(request.getHeader(AUTHORIZATION_HEADER)).isNull()
        assertThat(request.getHeader(IDEMPOTENCY_KEY_HEADER)).isNull()
    }

    private fun assertProtectedRequest(
        request: RecordedRequest,
        method: String,
        path: String,
        idempotencyKey: String? = null,
    ) {
        assertThat(request.method).isEqualTo(method)
        assertThat(request.requestUrl?.encodedPath).isEqualTo(path)
        assertThat(request.getHeader(AUTHORIZATION_HEADER)).isEqualTo("Bearer $ACCESS_TOKEN")
        if (idempotencyKey == null) {
            assertThat(request.getHeader(IDEMPOTENCY_KEY_HEADER)).isNull()
        } else {
            assertThat(request.getHeader(IDEMPOTENCY_KEY_HEADER)).isEqualTo(idempotencyKey)
        }
    }

    private fun assertQuery(request: RecordedRequest, vararg expected: Pair<String, String>) {
        val url = checkNotNull(request.requestUrl)
        expected.forEach { (name, value) ->
            assertThat(url.queryParameter(name)).isEqualTo(value)
        }
        assertThat(url.queryParameterNames).containsExactlyElementsIn(expected.map(Pair<String, String>::first))
    }

    private fun assertJsonBody(request: RecordedRequest, expected: JsonElement) {
        assertThat(request.getHeader("Content-Type")).startsWith("application/json")
        assertThat(json.parseToJsonElement(request.body.readUtf8())).isEqualTo(expected)
    }

    private fun assertPlaybackBody(request: RecordedRequest) {
        assertJsonBody(
            request,
            buildJsonObject {
                put("playbackSessionId", "playback-session-1")
                put("positionMs", 12_345)
                put("occurredAt", "2026-07-17T00:00:00Z")
                put("event", "PROGRESS")
            },
        )
    }

    private fun assertApiSurface(api: Class<*>, vararg expectedMethodNames: String) {
        val actualMethodNames =
            api.declaredMethods
                .filterNot { method -> method.isSynthetic }
                .map { method -> method.name }

        assertThat(actualMethodNames).containsExactlyElementsIn(expectedMethodNames)
    }

    private fun publicClient(): OkHttpClient = OkHttpClient
        .Builder()
        .addInterceptor { chain ->
            chain.proceed(
                chain
                    .request()
                    .newBuilder()
                    .header(AUTHORIZATION_HEADER, "Bearer should-be-removed")
                    .build(),
            )
        }.addInterceptor(RemoveAuthorizationInterceptor())
        .build()

    private fun protectedFixture(): ProtectedFixture {
        val tokens =
            SessionTokens(
                userId = USER_ID,
                sessionId = SESSION_ID,
                accessToken = AccessToken.from(ACCESS_TOKEN),
                accessTokenExpiresAtEpochMillis = Long.MAX_VALUE,
                refreshToken = RefreshToken.from("refresh-token-contract-value"),
                refreshTokenExpiresAtEpochMillis = Long.MAX_VALUE,
            )
        val runtime = ServerRuntimeCoordinator()
        val repository = InMemoryServerConfigRepository.from(server.url("/"))
        val endpoint = checkNotNull(repository.currentEndpoint())
        val client =
            OkHttpClient
                .Builder()
                .addInterceptor(
                    BearerTokenInterceptor(
                        tokenVault = InMemoryTokenVault(tokens),
                        serverConfigRepository = repository,
                        serverRuntimeCoordinator = runtime,
                    ),
                ).build()
        return ProtectedFixture(
            client = client,
            sessionContext =
            SessionRequestContext(
                userId = USER_ID,
                sessionId = SESSION_ID,
                serverGeneration = runtime.captureGeneration(),
                serverEndpoint = endpoint,
                accessToken = tokens.accessToken,
            ),
        )
    }

    private data class ProtectedFixture(val client: OkHttpClient, val sessionContext: SessionRequestContext)

    private companion object {
        const val AUTHORIZATION_HEADER = "Authorization"
        const val IDEMPOTENCY_KEY_HEADER = "Idempotency-Key"
        const val ACCESS_TOKEN = "contract-access-token"
        const val USER_ID = "user-1"
        const val SESSION_ID = "session-1"
    }
}
