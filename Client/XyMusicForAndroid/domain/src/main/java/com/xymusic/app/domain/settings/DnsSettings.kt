package com.xymusic.app.domain.settings

data class DnsSettings(
    val customDnsEnabled: Boolean = true,
    val dnsServer: String = DEFAULT_DNS_SERVER,
    val dohEnabled: Boolean = true,
    val dohUrl: String = DEFAULT_DOH_URL,
    val disableDnsCache: Boolean = false,
) {
    companion object {
        const val DEFAULT_DNS_SERVER = "223.5.5.5"
        const val DEFAULT_DOH_URL = "https://dns.alidns.com/dns-query"
        val ALIYUN_DNS_SERVERS = listOf("223.5.5.5", "223.6.6.6")
    }
}
