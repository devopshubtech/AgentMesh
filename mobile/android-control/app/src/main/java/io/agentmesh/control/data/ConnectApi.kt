package io.agentmesh.control.data

import java.io.IOException
import java.util.concurrent.TimeUnit
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlinx.serialization.json.put
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody

/** A granted exit-node session, ready for the tunnel engine. */
data class SessionGrant(val relayUrl: String, val ticket: String, val sessionId: String, val deviceName: String, val serverUrl: String)

/**
 * Opens sessions with a connect key (no login). If the saved address stops
 * answering (the free tunnel restarted with a new address), the current
 * address is looked up via the link's rendezvous URL and saved.
 */
class ConnectApi(private val store: ProfileStore) {
    private val http = OkHttpClient.Builder()
        .connectTimeout(15, TimeUnit.SECONDS)
        .readTimeout(30, TimeUnit.SECONDS)
        .build()
    private val json = Json { ignoreUnknownKeys = true }

    suspend fun open(p: ConnectProfile, clientLabel: String): SessionGrant = withContext(Dispatchers.IO) {
        try {
            openAt(p.serverUrl, p, clientLabel)
        } catch (e: Exception) {
            // A clear answer from the server (e.g. link revoked) is final.
            if (e is ApiException && e.status in 400..499) throw e
            val fresh = p.rendezvousUrl.takeIf { it.isNotBlank() }?.let { runCatching { resolve(it) }.getOrNull() }
            if (fresh == null || fresh == p.serverUrl) throw e
            val g = openAt(fresh, p, clientLabel)
            store.upsert(p.copy(serverUrl = fresh))
            g
        }
    }

    private fun openAt(server: String, p: ConnectProfile, clientLabel: String): SessionGrant {
        val body = buildJsonObject { put("client_label", clientLabel) }.toString()
            .toRequestBody("application/json".toMediaType())
        val req = Request.Builder().url("$server/v1/connect/session").post(body)
            .header("X-AgentMesh-Connect-Key", store.key(p))
            .header("User-Agent", "AgentMesh-Android")
            .build()
        http.newCall(req).execute().use { r ->
            val text = r.body?.string().orEmpty()
            if (!r.isSuccessful) {
                val err = runCatching { json.parseToJsonElement(text).jsonObject["error"]?.jsonObject }.getOrNull()
                val msg = err?.get("message")?.jsonPrimitive?.content
                if (msg == null && r.code >= 500) throw IOException("server unreachable (HTTP ${r.code})")
                throw ApiException(r.code, err?.get("code")?.jsonPrimitive?.content ?: "http_${r.code}", msg ?: "HTTP ${r.code}")
            }
            val o = json.parseToJsonElement(text).jsonObject
            return SessionGrant(
                relayUrl = "$server/v1/relay", // same address that just answered: works from any network
                ticket = o["ticket"]!!.jsonPrimitive.content,
                sessionId = o["session_id"]!!.jsonPrimitive.content,
                deviceName = o["device_name"]?.jsonPrimitive?.content.orEmpty(),
                serverUrl = server,
            )
        }
    }

    /** Reads {"url": ...} directly or from a GitHub gist API response. */
    private fun resolve(rendezvous: String): String? {
        val req = Request.Builder().url(rendezvous).header("Accept", "application/vnd.github+json").build()
        http.newCall(req).execute().use { r ->
            if (!r.isSuccessful) return null
            val root = json.parseToJsonElement(r.body?.string().orEmpty()).jsonObject
            val doc: JsonObject = root["files"]?.jsonObject?.get("agentmesh-endpoint.json")?.jsonObject?.get("content")
                ?.jsonPrimitive?.content?.let { json.parseToJsonElement(it).jsonObject } ?: root
            val url = doc["url"]?.jsonPrimitive?.content?.trimEnd('/') ?: return null
            return url.takeIf { it.startsWith("https://") }
        }
    }
}
