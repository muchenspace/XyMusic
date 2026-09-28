package com.xymusic.app.data.network.dns

import java.io.ByteArrayOutputStream
import java.io.DataOutputStream
import java.net.DatagramPacket
import java.net.DatagramSocket
import java.net.InetAddress
import java.nio.ByteBuffer

object UdpDnsResolver {
    private const val DNS_PORT = 53
    private const val DEFAULT_TIMEOUT_MS = 3_000

    fun resolve(
        hostname: String,
        serverIp: String,
        timeoutMs: Int = DEFAULT_TIMEOUT_MS,
    ): List<InetAddress> {
        val serverAddr = InetAddress.getByName(serverIp)
        val query = buildQuery(hostname)
        DatagramSocket().use { socket ->
            socket.soTimeout = timeoutMs
            val packet = DatagramPacket(query, query.size, serverAddr, DNS_PORT)
            socket.send(packet)

            val buffer = ByteArray(512)
            val response = DatagramPacket(buffer, buffer.size)
            socket.receive(response)

            return parseResponse(buffer, response.length, hostname)
        }
    }

    internal fun buildQuery(hostname: String, id: Short = (1..32767).random().toShort()): ByteArray {
        val baos = ByteArrayOutputStream()
        val dos = DataOutputStream(baos)

        // 12-byte DNS header
        dos.writeShort(id.toInt())
        dos.writeShort(0x0100) // Standard query with recursion desired (RD=1)
        dos.writeShort(1)      // QDCOUNT: 1 question
        dos.writeShort(0)      // ANCOUNT: 0
        dos.writeShort(0)      // NSCOUNT: 0
        dos.writeShort(0)      // ARCOUNT: 0

        // Question: QNAME
        for (label in hostname.split('.')) {
            val bytes = label.toByteArray(Charsets.US_ASCII)
            dos.writeByte(bytes.size)
            dos.write(bytes)
        }
        dos.writeByte(0) // Root null label

        dos.writeShort(1) // QTYPE: A (IPv4)
        dos.writeShort(1) // QCLASS: IN (Internet)

        dos.flush()
        return baos.toByteArray()
    }

    internal fun parseResponse(buffer: ByteArray, length: Int, hostname: String): List<InetAddress> {
        if (length < 12) return emptyList()
        val byteBuffer = ByteBuffer.wrap(buffer, 0, length)
        byteBuffer.short // id
        val flags = byteBuffer.short.toInt() and 0xFFFF
        val rcode = flags and 0x000F
        if (rcode != 0) return emptyList()

        val qdCount = byteBuffer.short.toInt() and 0xFFFF
        val anCount = byteBuffer.short.toInt() and 0xFFFF
        byteBuffer.short // nsCount
        byteBuffer.short // arCount

        // Skip Question section
        for (i in 0 until qdCount) {
            skipName(byteBuffer)
            if (byteBuffer.remaining() < 4) return emptyList()
            byteBuffer.short // qtype
            byteBuffer.short // qclass
        }

        val addresses = mutableListOf<InetAddress>()
        // Parse Answer section
        for (i in 0 until anCount) {
            if (!byteBuffer.hasRemaining()) break
            skipName(byteBuffer)
            if (byteBuffer.remaining() < 10) break
            val type = byteBuffer.short.toInt() and 0xFFFF
            val clazz = byteBuffer.short.toInt() and 0xFFFF
            byteBuffer.int // ttl
            val rdLength = byteBuffer.short.toInt() and 0xFFFF

            if (byteBuffer.remaining() < rdLength) break

            if (type == 1 && clazz == 1 && rdLength == 4) {
                // Type A (IPv4)
                val ipBytes = ByteArray(4)
                byteBuffer.get(ipBytes)
                runCatching {
                    addresses.add(InetAddress.getByAddress(hostname, ipBytes))
                }
            } else {
                byteBuffer.position(byteBuffer.position() + rdLength)
            }
        }

        return addresses
    }

    private fun skipName(buffer: ByteBuffer) {
        while (buffer.hasRemaining()) {
            val len = buffer.get().toInt() and 0xFF
            if (len == 0) break
            if ((len and 0xC0) == 0xC0) {
                // Compression offset (2 bytes)
                if (buffer.hasRemaining()) buffer.get()
                break
            } else {
                val target = buffer.position() + len
                if (target <= buffer.limit()) {
                    buffer.position(target)
                } else {
                    buffer.position(buffer.limit())
                    break
                }
            }
        }
    }
}
