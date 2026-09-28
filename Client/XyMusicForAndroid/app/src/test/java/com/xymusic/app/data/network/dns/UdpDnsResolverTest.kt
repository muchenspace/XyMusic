package com.xymusic.app.data.network.dns

import com.google.common.truth.Truth.assertThat
import java.io.ByteArrayOutputStream
import java.io.DataOutputStream
import java.net.InetAddress
import org.junit.Test

class UdpDnsResolverTest {
    @Test
    fun buildQueryConstructsValidDnsPacket() {
        val query = UdpDnsResolver.buildQuery("example.com", id = 0x1234.toShort())

        // Minimum 12-byte header + 13-byte question = 25 bytes
        assertThat(query.size).isEqualTo(29)

        // ID
        assertThat(query[0]).isEqualTo(0x12.toByte())
        assertThat(query[1]).isEqualTo(0x34.toByte())

        // Flags: 0x0100 (RD=1)
        assertThat(query[2]).isEqualTo(0x01.toByte())
        assertThat(query[3]).isEqualTo(0x00.toByte())

        // QDCOUNT: 1
        assertThat(query[4]).isEqualTo(0x00.toByte())
        assertThat(query[5]).isEqualTo(0x01.toByte())

        // Label: 7 "example"
        assertThat(query[12]).isEqualTo(7.toByte())
        val exampleStr = String(query, 13, 7, Charsets.US_ASCII)
        assertThat(exampleStr).isEqualTo("example")

        // Label: 3 "com"
        assertThat(query[20]).isEqualTo(3.toByte())
        val comStr = String(query, 21, 3, Charsets.US_ASCII)
        assertThat(comStr).isEqualTo("com")

        // Null terminator
        assertThat(query[24]).isEqualTo(0.toByte())

        // QTYPE: 1 (A)
        assertThat(query[25]).isEqualTo(0.toByte())
        assertThat(query[26]).isEqualTo(1.toByte())

        // QCLASS: 1 (IN)
        assertThat(query[27]).isEqualTo(0.toByte())
        assertThat(query[28]).isEqualTo(1.toByte())
    }

    @Test
    fun parseResponseExtractsIPv4Addresses() {
        val baos = ByteArrayOutputStream()
        val dos = DataOutputStream(baos)

        // Header: 12 bytes
        dos.writeShort(0x1234)
        dos.writeShort(0x8180) // Standard query response, No error
        dos.writeShort(1)      // QDCOUNT: 1
        dos.writeShort(1)      // ANCOUNT: 1
        dos.writeShort(0)      // NSCOUNT: 0
        dos.writeShort(0)      // ARCOUNT: 0

        // Question: example.com
        dos.writeByte(7)
        dos.writeBytes("example")
        dos.writeByte(3)
        dos.writeBytes("com")
        dos.writeByte(0)
        dos.writeShort(1) // QTYPE: A
        dos.writeShort(1) // QCLASS: IN

        // Answer: pointer 0xC00C to question name
        dos.writeShort(0xC00C)
        dos.writeShort(1)  // TYPE: A
        dos.writeShort(1)  // CLASS: IN
        dos.writeInt(300)  // TTL
        dos.writeShort(4)  // RDLENGTH: 4
        dos.writeByte(93)
        dos.writeByte(184)
        dos.writeByte(216)
        dos.writeByte(34)  // 93.184.216.34

        dos.flush()
        val responseBytes = baos.toByteArray()

        val addresses = UdpDnsResolver.parseResponse(responseBytes, responseBytes.size, "example.com")
        assertThat(addresses).hasSize(1)
        assertThat(addresses[0]).isEqualTo(InetAddress.getByAddress("example.com", byteArrayOf(93.toByte(), 184.toByte(), 216.toByte(), 34.toByte())))
    }

    @Test
    fun parseResponseReturnsEmptyOnErrorRcode() {
        val baos = ByteArrayOutputStream()
        val dos = DataOutputStream(baos)

        // Header: 12 bytes with RCODE = 3 (NXDOMAIN)
        dos.writeShort(0x1234)
        dos.writeShort(0x8183)
        dos.writeShort(1)
        dos.writeShort(0)
        dos.writeShort(0)
        dos.writeShort(0)
        dos.flush()

        val bytes = baos.toByteArray()
        val addresses = UdpDnsResolver.parseResponse(bytes, bytes.size, "nonexistent.example")
        assertThat(addresses).isEmpty()
    }

    @Test
    fun parseResponseReturnsEmptyOnTruncatedBuffer() {
        val addresses = UdpDnsResolver.parseResponse(ByteArray(5), 5, "example.com")
        assertThat(addresses).isEmpty()
    }
}
