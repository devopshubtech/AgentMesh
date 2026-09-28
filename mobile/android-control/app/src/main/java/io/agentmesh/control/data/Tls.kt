package io.agentmesh.control.data

import java.io.ByteArrayInputStream
import java.security.KeyStore
import java.security.MessageDigest
import java.security.cert.CertificateException
import java.security.cert.CertificateFactory
import java.security.cert.X509Certificate
import java.util.concurrent.TimeUnit
import javax.net.ssl.SSLContext
import javax.net.ssl.TrustManagerFactory
import javax.net.ssl.X509TrustManager
import okhttp3.OkHttpClient
import okhttp3.Request

/** TLS helpers: trust the system CAs plus an optional private (e.g. dev) CA. */
object Tls {
    fun parseCert(pem: String): X509Certificate =
        CertificateFactory.getInstance("X.509").generateCertificate(ByteArrayInputStream(pem.toByteArray())) as X509Certificate

    fun fingerprint(cert: X509Certificate): String =
        MessageDigest.getInstance("SHA-256").digest(cert.encoded).joinToString(":") { "%02X".format(it) }

    private fun systemTrust(): X509TrustManager {
        val tmf = TrustManagerFactory.getInstance(TrustManagerFactory.getDefaultAlgorithm())
        tmf.init(null as KeyStore?)
        return tmf.trustManagers.filterIsInstance<X509TrustManager>().first()
    }

    private fun customTrust(ca: X509Certificate): X509TrustManager {
        val ks = KeyStore.getInstance(KeyStore.getDefaultType()).apply {
            load(null)
            setCertificateEntry("agentmesh-ca", ca)
        }
        val tmf = TrustManagerFactory.getInstance(TrustManagerFactory.getDefaultAlgorithm())
        tmf.init(ks)
        return tmf.trustManagers.filterIsInstance<X509TrustManager>().first()
    }

    /** Accepts a chain if either the system store or the private CA trusts it. */
    fun trustManager(caPem: String): X509TrustManager {
        val system = systemTrust()
        if (caPem.isBlank()) return system
        val custom = customTrust(parseCert(caPem))
        return object : X509TrustManager {
            override fun checkClientTrusted(chain: Array<X509Certificate>, authType: String) =
                system.checkClientTrusted(chain, authType)

            override fun checkServerTrusted(chain: Array<X509Certificate>, authType: String) {
                try {
                    custom.checkServerTrusted(chain, authType)
                } catch (e: CertificateException) {
                    system.checkServerTrusted(chain, authType)
                }
            }

            override fun getAcceptedIssuers(): Array<X509Certificate> = system.acceptedIssuers + custom.acceptedIssuers
        }
    }

    fun client(caPem: String): OkHttpClient {
        val tm = trustManager(caPem)
        val ctx = SSLContext.getInstance("TLS").apply { init(null, arrayOf(tm), null) }
        return OkHttpClient.Builder()
            .sslSocketFactory(ctx.socketFactory, tm)
            .connectTimeout(15, TimeUnit.SECONDS)
            .readTimeout(30, TimeUnit.SECONDS)
            .build()
    }

    /**
     * Trust-on-first-use: downloads the server's published CA without
     * verification, then proves the server's certificate chains to it. The
     * caller must show [fingerprint] to the user for confirmation before
     * saving the CA.
     */
    fun fetchServerCa(serverUrl: String): X509Certificate {
        val trustAll = object : X509TrustManager {
            override fun checkClientTrusted(chain: Array<X509Certificate>, authType: String) {}
            override fun checkServerTrusted(chain: Array<X509Certificate>, authType: String) {}
            override fun getAcceptedIssuers(): Array<X509Certificate> = emptyArray()
        }
        val ctx = SSLContext.getInstance("TLS").apply { init(null, arrayOf(trustAll), null) }
        val insecure = OkHttpClient.Builder()
            .sslSocketFactory(ctx.socketFactory, trustAll)
            .hostnameVerifier { _, _ -> true }
            .connectTimeout(10, TimeUnit.SECONDS)
            .build()
        val pem = insecure.newCall(Request.Builder().url("$serverUrl/agentmesh-ca.pem").build()).execute().use { r ->
            if (!r.isSuccessful) error("server does not publish a CA certificate (HTTP ${r.code})")
            r.body!!.string()
        }
        val ca = parseCert(pem)
        // The live server certificate must chain to this CA (with hostname check).
        client(pem).newCall(Request.Builder().url("$serverUrl/agentmesh-ca.pem").build()).execute().close()
        return ca
    }

    fun toPem(cert: X509Certificate): String =
        "-----BEGIN CERTIFICATE-----\n" +
            android.util.Base64.encodeToString(cert.encoded, android.util.Base64.DEFAULT) +
            "-----END CERTIFICATE-----\n"
}
