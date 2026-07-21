package com.xymusic.app.core.data.media.remote

import com.google.common.truth.Truth.assertThat
import com.xymusic.app.data.network.auth.model.ArtworkDto as AuthArtworkDto
import kotlinx.serialization.json.Json
import org.junit.Test

class ArtworkDtoTest {
    @Test
    fun catalogArtworkAcceptsStableResourceWithoutExpiry() {
        val artwork = Json.decodeFromString<ArtworkDto>(STABLE_ARTWORK_JSON)

        assertThat(artwork.url).isEqualTo(STABLE_ARTWORK_URL)
        assertThat(artwork.expiresAt).isNull()
    }

    @Test
    fun authenticatedUserArtworkAcceptsStableResourceWithoutExpiry() {
        val artwork = Json.decodeFromString<AuthArtworkDto>(STABLE_ARTWORK_JSON)

        assertThat(artwork.url).isEqualTo(STABLE_ARTWORK_URL)
        assertThat(artwork.expiresAt).isNull()
    }

    private companion object {
        const val STABLE_ARTWORK_URL =
            "/api/v1/oss/artwork/00000000-0000-4000-8000-000000000001/cover-version"
        const val STABLE_ARTWORK_JSON =
            """{"assetId":"00000000-0000-4000-8000-000000000001","url":"$STABLE_ARTWORK_URL","cacheKey":"asset:cover-version","mimeType":"image/webp"}"""
    }
}
