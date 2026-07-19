package com.xymusic.app

import com.google.common.truth.Truth.assertThat
import org.junit.Test

class ProjectConfigurationTest {
    @Test
    fun applicationIdIsConfigured() {
        val expectedApplicationId =
            if (BuildConfig.DEBUG) {
                "com.xymusic.app.debug"
            } else {
                "com.xymusic.app"
            }

        assertThat(BuildConfig.APPLICATION_ID).isEqualTo(expectedApplicationId)
    }
}
