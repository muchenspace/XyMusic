package com.xymusic.app

import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import com.google.common.truth.Truth.assertThat
import org.junit.Test
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class ApplicationContextTest {
    @Test
    fun applicationPackageIsCorrect() {
        val context = InstrumentationRegistry.getInstrumentation().targetContext
        assertThat(context.packageName).isEqualTo("com.xymusic.app.debug")
    }
}
