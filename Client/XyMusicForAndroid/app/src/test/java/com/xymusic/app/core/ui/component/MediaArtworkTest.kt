package com.xymusic.app.core.ui.component

import com.google.common.truth.Truth.assertThat
import org.junit.Test

class MediaArtworkTest {
    @Test
    fun stableCacheKeyIgnoresMissingValuesAndPreservesContentIdentity() {
        assertThat(stableArtworkCacheKey(null)).isNull()
        assertThat(stableArtworkCacheKey("   ")).isNull()
        assertThat(stableArtworkCacheKey("artwork:asset-1:generation-2"))
            .isEqualTo("artwork:asset-1:generation-2")
    }
}
