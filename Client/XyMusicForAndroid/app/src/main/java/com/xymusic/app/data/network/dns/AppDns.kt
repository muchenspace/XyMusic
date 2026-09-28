package com.xymusic.app.data.network.dns

import com.xymusic.app.domain.settings.AppSettingsRepository
import com.xymusic.app.domain.settings.DnsSettings
import java.net.Inet6Address
import java.net.InetAddress
import java.net.UnknownHostException
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.TimeUnit
import javax.inject.Inject
import javax.inject.Singleton
import kotlinx.coroutines.CoroutineDispatcher
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.launch
import okhttp3.Dns
import okhttp3.HttpUrl.Companion.toHttpUrlOrNull
import okhttp3.OkHttpClient
import okhttp3.dnsoverhttps.DnsOverHttps

@Singleton
class AppDns @Inject constructor(
    private val appSettingsRepository: AppSettingsRepository,
    private val ioDispatcher: CoroutineDispatcher = Dispatchers.IO,
    private val systemDns: Dns = Dns.SYSTEM,
) : Dns {
    private val scope = CoroutineScope(SupervisorJob() + ioDispatcher)

    @Volatile
    private var currentSettings: DnsSettings = DnsSettings()

    private val cache = ConcurrentHashMap<String, CachedDnsRecord>()

    @Volatile
    private var cachedDohClient: Pair<String, DnsOverHttps>? = null

    init {
        scope.launch {
            appSettingsRepository.settings.collect { appSettings ->
                updateSettings(appSettings.dnsSettings)
            }
        }
    }

    fun updateSettings(settings: DnsSettings) {
        currentSettings = settings
        if (settings.disableDnsCache) {
            clearCache()
        }
    }

    fun clearCache() {
        cache.clear()
    }

    override fun lookup(hostname: String): List<InetAddress> {
        if (hostname.isBlank()) {
            throw UnknownHostException("Hostname cannot be blank")
        }

        // If hostname is already a numeric IP, return it directly without network queries
        if (isIpAddress(hostname)) {
            return listOf(InetAddress.getByName(hostname))
        }

        val settings = currentSettings

        // Check cache if not disabled
        if (!settings.disableDnsCache) {
            val cached = cache[hostname]
            if (cached != null && System.currentTimeMillis() < cached.expiresAt) {
                return cached.addresses
            }
        }

        val resolved = if (!settings.customDnsEnabled) {
            systemDns.lookup(hostname)
        } else {
            var addresses: List<InetAddress>? = null

            // 1. Try DoH if enabled
            if (settings.dohEnabled && settings.dohUrl.isNotBlank()) {
                addresses = tryResolveDoh(hostname, settings)
            }

            // 2. Fall back to custom UDP DNS
            if (addresses.isNullOrEmpty() && settings.dnsServer.isNotBlank()) {
                addresses = tryResolveUdp(hostname, settings.dnsServer)
            }

            // 3. Fall back to system DNS
            if (addresses.isNullOrEmpty()) {
                addresses = runCatching { systemDns.lookup(hostname) }.getOrNull()
            }

            addresses ?: throw UnknownHostException("Unable to resolve host: $hostname")
        }

        if (resolved.isEmpty()) {
            throw UnknownHostException("Unable to resolve host: $hostname")
        }

        if (!settings.disableDnsCache) {
            cache[hostname] = CachedDnsRecord(
                addresses = resolved,
                expiresAt = System.currentTimeMillis() + CACHE_TTL_MS,
            )
        }

        return resolved
    }

    private fun tryResolveDoh(hostname: String, settings: DnsSettings): List<InetAddress>? {
        val dohHost = settings.dohUrl.toHttpUrlOrNull()?.host
        // Prevent recursive lookup for the DoH server itself
        if (dohHost != null && hostname.equals(dohHost, ignoreCase = true)) {
            return null
        }

        val doh = getOrCreateDoh(settings) ?: return null
        return runCatching { doh.lookup(hostname) }.getOrNull()
    }

    private fun getOrCreateDoh(settings: DnsSettings): DnsOverHttps? {
        val url = settings.dohUrl.trim()
        val httpUrl = url.toHttpUrlOrNull() ?: return null
        val existing = cachedDohClient
        if (existing != null && existing.first == url) {
            return existing.second
        }

        val bootstrapIps = mutableListOf<InetAddress>()
        if (httpUrl.host.contains("alidns")) {
            DnsSettings.ALIYUN_DNS_SERVERS.forEach { ip ->
                runCatching { InetAddress.getByName(ip) }.getOrNull()?.let { bootstrapIps.add(it) }
            }
        }
        val customIp = settings.dnsServer.trim()
        if (isIpAddress(customIp)) {
            runCatching { InetAddress.getByName(customIp) }.getOrNull()?.let {
                if (!bootstrapIps.contains(it)) bootstrapIps.add(it)
            }
        }

        val bootstrapClient = OkHttpClient.Builder()
            .dns(systemDns)
            .connectTimeout(5, TimeUnit.SECONDS)
            .readTimeout(5, TimeUnit.SECONDS)
            .build()

        val builder = DnsOverHttps.Builder()
            .client(bootstrapClient)
            .url(httpUrl)
            .includeIPv6(true)

        if (bootstrapIps.isNotEmpty()) {
            builder.bootstrapDnsHosts(bootstrapIps)
        }

        val doh = runCatching { builder.build() }.getOrNull() ?: return null
        cachedDohClient = Pair(url, doh)
        return doh
    }

    private fun tryResolveUdp(hostname: String, dnsServers: String): List<InetAddress>? {
        val servers = dnsServers.split(',', ';', ' ')
            .map { it.trim() }
            .filter { isIpAddress(it) }

        for (serverIp in servers) {
            try {
                val result = UdpDnsResolver.resolve(hostname, serverIp)
                if (result.isNotEmpty()) {
                    return result
                }
            } catch (_: Exception) {
                // Try next server on failure
            }
        }
        return null
    }

    companion object {
        const val CACHE_TTL_MS = 5 * 60 * 1_000L // 5 minutes

        fun isIpAddress(ip: String): Boolean {
            val trimmed = ip.trim()
            val parts = trimmed.split('.')
            if (parts.size == 4 && parts.all { part ->
                part.toIntOrNull()?.let { it in 0..255 } == true
            }) {
                return true
            }
            if (trimmed.contains(':')) {
                return runCatching { InetAddress.getByName(trimmed) is Inet6Address }.getOrDefault(false)
            }
            return false
        }
    }

    private data class CachedDnsRecord(
        val addresses: List<InetAddress>,
        val expiresAt: Long,
    )
}
