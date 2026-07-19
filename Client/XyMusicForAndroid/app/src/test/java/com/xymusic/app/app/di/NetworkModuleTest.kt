package com.xymusic.app.app.di

import com.google.common.truth.Truth.assertThat
import com.xymusic.app.support.InMemoryServerConfigRepository
import org.junit.Test

class NetworkModuleTest {
    @Test
    fun networkLayerCanInitializeBeforeServerSetup() {
        val baseUrl =
            NetworkModule.provideApiBaseUrl(
                InMemoryServerConfigRepository(initialEndpoint = null),
            )

        assertThat(baseUrl.toString()).isEqualTo("https://localhost/")
    }
}
