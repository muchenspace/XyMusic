package com.xymusic.benchmark

import android.os.Build
import androidx.benchmark.macro.CompilationMode
import androidx.benchmark.macro.FrameTimingMetric
import androidx.benchmark.macro.StartupMode
import androidx.benchmark.macro.StartupTimingMetric
import androidx.benchmark.macro.junit4.BaselineProfileRule
import androidx.benchmark.macro.junit4.MacrobenchmarkRule
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assume.assumeTrue
import org.junit.BeforeClass
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class BaselineProfileGenerator {
    @get:Rule
    val rule = BaselineProfileRule()

    companion object {
        @JvmStatic
        @BeforeClass
        fun assumeProfileCollectionSupported() {
            assumeTrue(
                "Baseline profile collection requires API 28+ in this benchmark environment",
                Build.VERSION.SDK_INT >= Build.VERSION_CODES.P,
            )
        }
    }

    @Test
    fun generate() = rule.collect(
        packageName = PACKAGE_NAME,
        includeInStartupProfile = true,
    ) {
        pressHome()
        runBaselineProfileJourney()
    }
}

@RunWith(AndroidJUnit4::class)
class StartupBenchmarks {
    @get:Rule
    val rule = MacrobenchmarkRule()

    companion object {
        @JvmStatic
        @BeforeClass
        fun assumeMacrobenchmarkSupported() {
            val isLgAndroidTen =
                Build.VERSION.SDK_INT == Build.VERSION_CODES.Q &&
                    Build.MANUFACTURER.equals("LGE", ignoreCase = true)
            assumeTrue(
                "AndroidX Macrobenchmark 1.4.1 loses UiAutomation on LG Android 10 during shell setup",
                !isLgAndroidTen,
            )
        }
    }

    @Test
    fun coldStartupToInteractiveContent() = rule.measureRepeated(
        packageName = PACKAGE_NAME,
        metrics = listOf(StartupTimingMetric(), FrameTimingMetric()),
        compilationMode = CompilationMode.None(),
        iterations = 5,
        startupMode = StartupMode.COLD,
        setupBlock = { pressHome() },
    ) {
        startActivityAndWaitForInteractiveContent()
    }

    @Test
    fun warmStartupToInteractiveContent() = rule.measureRepeated(
        packageName = PACKAGE_NAME,
        metrics = listOf(StartupTimingMetric(), FrameTimingMetric()),
        compilationMode = CompilationMode.None(),
        iterations = 5,
        startupMode = StartupMode.WARM,
        setupBlock = { pressHome() },
    ) {
        startActivityAndWaitForInteractiveContent()
    }
}
