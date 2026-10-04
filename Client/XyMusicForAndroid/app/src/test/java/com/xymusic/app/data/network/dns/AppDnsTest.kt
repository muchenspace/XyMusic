package com.xymusic.app.data.network.dns

import com.google.common.truth.Truth.assertThat
import com.xymusic.app.domain.settings.AppSettings
import com.xymusic.app.domain.settings.AppSettingsRepository
import com.xymusic.app.domain.settings.DnsSettings
import java.net.InetAddress
import java.net.UnknownHostException
import java.util.concurrent.atomic.AtomicInteger
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import okhttp3.Dns
import org.junit.Assert.assertThrows
import org.junit.Test

@OptIn(ExperimentalCoroutinesApi::class)
class AppDnsTest {
    private val testDispatcher = UnconfinedTestDispatcher()

    @Test
    fun ipLiteralsAreReturnedDirectlyWithoutDnsQuery() {
        val appDns = createAppDns(FakeAppSettingsRepository())

        val result = appDns.lookup("192.168.1.1")

        assertThat(result).hasSize(1)
        assertThat(result[0].hostAddress).isEqualTo("192.168.1.1")
    }

    @Test
    fun blankHostnameThrowsUnknownHostException() {
        val appDns = createAppDns(FakeAppSettingsRepository())

        assertThrows(UnknownHostException::class.java) {
            appDns.lookup("   ")
        }
    }

    @Test
    fun customDnsDisabledDelegatesDirectlyToSystemDns() {
        val systemCallCount = AtomicInteger(0)
        val mockSystemDns = Dns { hostname ->
            systemCallCount.incrementAndGet()
            listOf(InetAddress.getByName("10.0.0.1"))
        }

        val repository = FakeAppSettingsRepository(
            AppSettings(
                dnsSettings = DnsSettings(customDnsEnabled = false),
            ),
        )
        val appDns = createAppDns(repository, systemDns = mockSystemDns)

        val result = appDns.lookup("music.local")

        assertThat(result).hasSize(1)
        assertThat(result[0].hostAddress).isEqualTo("10.0.0.1")
        assertThat(systemCallCount.get()).isEqualTo(1)
    }

    @Test
    fun dnsCacheCachesSuccessfulLookupWhenNotDisabled() {
        val systemCallCount = AtomicInteger(0)
        val mockSystemDns = Dns {
            systemCallCount.incrementAndGet()
            listOf(InetAddress.getByName("10.0.0.2"))
        }

        val repository = FakeAppSettingsRepository(
            AppSettings(
                dnsSettings = DnsSettings(
                    customDnsEnabled = false,
                    disableDnsCache = false,
                ),
            ),
        )
        val appDns = createAppDns(repository, systemDns = mockSystemDns)

        // First lookup hits DNS
        val result1 = appDns.lookup("cache.test")
        // Second lookup hits cache
        val result2 = appDns.lookup("cache.test")

        assertThat(result1).isEqualTo(result2)
        assertThat(systemCallCount.get()).isEqualTo(1)
    }

    @Test
    fun disableDnsCacheBypassesCacheOnEveryLookup() {
        val systemCallCount = AtomicInteger(0)
        val mockSystemDns = Dns {
            systemCallCount.incrementAndGet()
            listOf(InetAddress.getByName("10.0.0.3"))
        }

        val repository = FakeAppSettingsRepository(
            AppSettings(
                dnsSettings = DnsSettings(
                    customDnsEnabled = false,
                    disableDnsCache = true,
                ),
            ),
        )
        val appDns = createAppDns(repository, systemDns = mockSystemDns)

        appDns.lookup("nocache.test")
        appDns.lookup("nocache.test")

        assertThat(systemCallCount.get()).isEqualTo(2)
    }

    private fun createAppDns(
        repository: AppSettingsRepository,
        systemDns: Dns = Dns.SYSTEM,
    ): AppDns = AppDns(
        appSettingsRepository = repository,
        ioDispatcher = testDispatcher,
        systemDns = systemDns,
    )

    private class FakeAppSettingsRepository(initial: AppSettings = AppSettings()) : AppSettingsRepository {
        private val state = MutableStateFlow(initial)
        override val settings: Flow<AppSettings> = state

        override suspend fun update(settings: AppSettings) {
            state.value = settings
        }

        override suspend fun mutate(transform: (AppSettings) -> AppSettings) {
            state.value = transform(state.value)
        }

        override suspend fun reset() {
            state.value = AppSettings()
        }
    }
}
