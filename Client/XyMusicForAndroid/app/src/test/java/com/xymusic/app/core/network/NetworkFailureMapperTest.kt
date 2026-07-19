package com.xymusic.app.core.network

import com.google.common.truth.Truth.assertThat
import java.io.EOFException
import java.io.IOException
import java.net.ConnectException
import java.net.NoRouteToHostException
import java.net.SocketTimeoutException
import java.net.UnknownHostException
import java.net.UnknownServiceException
import javax.net.ssl.SSLHandshakeException
import org.junit.Test

class NetworkFailureMapperTest {
    @Test
    fun classifiesSupportedTransportFailures() {
        val cases =
            listOf(
                ConnectException("Connection refused") to NetworkFailureReason.ConnectionRefused,
                UnknownHostException("missing.example") to NetworkFailureReason.HostUnresolved,
                SocketTimeoutException("timeout") to NetworkFailureReason.Timeout,
                SSLHandshakeException("certificate rejected") to NetworkFailureReason.SecureConnectionFailed,
                UnknownServiceException("CLEARTEXT communication not permitted") to
                    NetworkFailureReason.SecureConnectionFailed,
                NoRouteToHostException("No route to host") to NetworkFailureReason.NoRoute,
                EOFException("unexpected end of stream") to NetworkFailureReason.ConnectionLost,
                IOException("offline") to NetworkFailureReason.Unknown,
            )

        cases.forEach { (failure, expectedReason) ->
            assertThat(failure.toDomainNetworkError().reason).isEqualTo(expectedReason)
        }
    }

    @Test
    fun inspectsWrappedCauses() {
        val failure = IOException("request failed", UnknownHostException("missing.example"))

        assertThat(failure.toDomainNetworkError().reason).isEqualTo(NetworkFailureReason.HostUnresolved)
    }
}
