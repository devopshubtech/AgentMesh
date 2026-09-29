package io.agentmesh.control.data

import android.net.Uri
import kotlinx.serialization.Serializable

/**
 * A saved remote server: the AgentMesh address plus the connect key from a
 * QR code / link. The key itself is stored encrypted (see [ProfileStore]).
 */
@Serializable
data class ConnectProfile(
    val id: String,
    val name: String,
    val serverUrl: String,
    val keyEnc: String,
    val keyHash: String,
    val rendezvousUrl: String = "",
    val deviceName: String = "",
    val lastConnectedAt: Long = 0,
)

/** What a connect link carries. */
data class ConnectLink(val serverUrl: String, val key: String, val rendezvousUrl: String)

class ApiException(val status: Int, val code: String, override val message: String) : Exception(message)

object Links {
    /**
     * Accepts every form a connect link can arrive in:
     *   https://<server>/join.html#k=<key>&r=<rendezvous>   (QR code / shared link)
     *   agentmesh://join?u=<server>&k=<key>&r=<rendezvous>   (deep link from the join page)
     */
    fun parse(raw: String): ConnectLink? {
        val s = raw.trim()
        if (s.isEmpty()) return null
        val uri = runCatching { Uri.parse(s) }.getOrNull() ?: return null
        return when (uri.scheme?.lowercase()) {
            "agentmesh" -> {
                val u = uri.getQueryParameter("u")?.trimEnd('/') ?: return null
                val k = uri.getQueryParameter("k") ?: return null
                if (!u.startsWith("https://")) null else ConnectLink(u, k, uri.getQueryParameter("r").orEmpty())
            }
            "https" -> {
                val params = Uri.parse("x://x?" + (uri.encodedFragment ?: ""))
                val k = params.getQueryParameter("k") ?: return null
                ConnectLink("https://" + uri.encodedAuthority, k, params.getQueryParameter("r").orEmpty())
            }
            else -> null
        }
    }

    /**
     * Normalizes a server address typed by hand (Edit dialog):
     *   203.0.113.7 -> https://203.0.113.7:13443, host:port -> https://host:port, https://x/y -> https://x
     */
    fun normalizeServer(input: String): String? {
        var s = input.trim().trimEnd('/')
        if (s.startsWith("http://", ignoreCase = true)) return null
        if (s.startsWith("https://", ignoreCase = true)) s = s.substring(8)
        val hostPort = s.substringBefore('/')
        if (hostPort.isBlank() || hostPort.contains(' ')) return null
        val host = hostPort.substringBefore(':')
        val hasPort = hostPort.contains(':')
        val isIp = host.split('.').let { p -> p.size == 4 && p.all { it.toIntOrNull() in 0..255 } }
        return when {
            hasPort -> "https://$hostPort"
            isIp -> "https://$host:13443"
            host.contains('.') || host == "localhost" -> "https://$host"
            else -> null
        }
    }
}
