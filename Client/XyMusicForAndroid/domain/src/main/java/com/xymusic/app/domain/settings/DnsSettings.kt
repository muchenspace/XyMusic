package com.xymusic.app.domain.settings

import java.net.Inet6Address
import java.net.InetAddress

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

/**
 * Returns whether [ip] is a literal IPv4 or IPv6 address.
 *
 * Lives in the domain so presentation can validate DNS input without depending
 * on the data-layer DNS client.
 */
fun isIpAddress(ip: String): Boolean {
    val trimmed = ip.trim()
    val parts = trimmed.split('.')
    if (parts.size == 4 &&
        parts.all { part ->
            part.toIntOrNull()?.let { it in 0..255 } == true
        }
    ) {
        return true
    }
    if (trimmed.contains(':')) {
        return runCatching { InetAddress.getByName(trimmed) is Inet6Address }.getOrDefault(false)
    }
    return false
}
