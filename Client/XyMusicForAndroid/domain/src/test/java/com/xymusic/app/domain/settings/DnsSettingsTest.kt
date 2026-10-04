package com.xymusic.app.domain.settings

import com.google.common.truth.Truth.assertThat
import org.junit.Test

class DnsSettingsTest {
    @Test
    fun defaultSettingsHaveAliyunDnsAndDohEnabled() {
        val settings = DnsSettings()

        assertThat(settings.customDnsEnabled).isTrue()
        assertThat(settings.dnsServer).isEqualTo("223.5.5.5")
        assertThat(settings.dohEnabled).isTrue()
        assertThat(settings.dohUrl).isEqualTo("https://dns.alidns.com/dns-query")
        assertThat(settings.disableDnsCache).isFalse()
    }

    @Test
    fun appSettingsIncludesDefaultDnsSettings() {
        val appSettings = AppSettings()

        assertThat(appSettings.dnsSettings.customDnsEnabled).isTrue()
        assertThat(appSettings.dnsSettings.dnsServer).isEqualTo("223.5.5.5")
        assertThat(appSettings.dnsSettings.dohEnabled).isTrue()
        assertThat(appSettings.dnsSettings.dohUrl).isEqualTo("https://dns.alidns.com/dns-query")
        assertThat(appSettings.dnsSettings.disableDnsCache).isFalse()
    }

    @Test
    fun customSettingsPreserveValues() {
        val settings = DnsSettings(
            customDnsEnabled = false,
            dnsServer = "8.8.8.8",
            dohEnabled = false,
            dohUrl = "https://dns.google/dns-query",
            disableDnsCache = true,
        )

        assertThat(settings.customDnsEnabled).isFalse()
        assertThat(settings.dnsServer).isEqualTo("8.8.8.8")
        assertThat(settings.dohEnabled).isFalse()
        assertThat(settings.dohUrl).isEqualTo("https://dns.google/dns-query")
        assertThat(settings.disableDnsCache).isTrue()
    }

    @Test
    fun isIpAddressRecognizesIPv4AndIPv6() {
        assertThat(isIpAddress("223.5.5.5")).isTrue()
        assertThat(isIpAddress("127.0.0.1")).isTrue()
        assertThat(isIpAddress("::1")).isTrue()
        assertThat(isIpAddress("2400:3200::1")).isTrue()

        assertThat(isIpAddress("dns.alidns.com")).isFalse()
        assertThat(isIpAddress("example.com")).isFalse()
        assertThat(isIpAddress("999.999.999.999")).isFalse()
    }
}
