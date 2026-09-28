package io.agentmesh.control.data

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import kotlinx.serialization.KSerializer
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import java.util.UUID

/**
 * control-api client. The access token lives only in memory; the refresh
 * token is stored encrypted (see [SecureStore]) and rotated on every use.
 */
class Api(private val store: SecureStore) {
    private val json = Json { ignoreUnknownKeys = true; explicitNulls = false; encodeDefaults = true }
    private val jsonType = "application/json".toMediaType()
    private val refreshLock = Mutex()

    @Volatile private var http: OkHttpClient = Tls.client(store.caPem)
    @Volatile private var accessToken: String? = null

    val serverUrl get() = store.serverUrl
    val caPem get() = store.caPem

    /** Rebuilds the HTTP client after the server or CA changed. */
    fun reconfigure() {
        http = Tls.client(store.caPem)
        accessToken = null
    }

    private fun errorFrom(code: Int, body: String): ApiException {
        val detail = runCatching { json.decodeFromString(ApiErrorBody.serializer(), body).error }.getOrNull()
        return ApiException(code, detail?.code ?: "http_$code", detail?.message ?: "HTTP $code")
    }

    private fun <T> exec(req: Request, ser: KSerializer<T>?): T? {
        http.newCall(req).execute().use { r ->
            val body = r.body?.string().orEmpty()
            if (!r.isSuccessful) throw errorFrom(r.code, body)
            if (ser == null || body.isBlank()) return null
            return json.decodeFromString(ser, body)
        }
    }

    private fun request(method: String, path: String, body: String?, token: String?, extra: Map<String, String> = emptyMap()): Request {
        val b = Request.Builder().url(store.serverUrl + path)
            .method(method, body?.toRequestBody(jsonType) ?: if (method == "POST") "".toRequestBody(jsonType) else null)
            .header("User-Agent", "AgentMesh-Android")
        token?.let { b.header("Authorization", "Bearer $it") }
        extra.forEach { (k, v) -> b.header(k, v) }
        return b.build()
    }

    private suspend fun refresh(): Boolean = refreshLock.withLock {
        val rt = store.refreshToken ?: return false
        return try {
            val res = exec(request("POST", "/v1/auth/refresh", json.encodeToString(mapOf("refresh_token" to rt)), null),
                TokenResponse.serializer())!!
            accessToken = res.accessToken
            res.refreshToken?.let { store.refreshToken = it }
            true
        } catch (e: ApiException) {
            if (e.status == 401) store.refreshToken = null
            false
        }
    }

    /** Authenticated call with one transparent refresh on 401. */
    private suspend fun <T> call(method: String, path: String, body: String?, ser: KSerializer<T>?, extra: Map<String, String> = emptyMap()): T? =
        withContext(Dispatchers.IO) {
            if (accessToken == null && !refresh()) throw ApiException(401, "unauthenticated", "Please sign in again")
            try {
                exec(request(method, path, body, accessToken, extra), ser)
            } catch (e: ApiException) {
                if (e.status != 401 || !refresh()) throw e
                exec(request(method, path, body, accessToken, extra), ser)
            }
        }

    suspend fun login(email: String, password: String): User = withContext(Dispatchers.IO) {
        val body = json.encodeToString(mapOf("email" to email, "password" to password, "client" to "mobile"))
        val res = exec(request("POST", "/v1/auth/login", body, null), TokenResponse.serializer())!!
        accessToken = res.accessToken
        store.refreshToken = res.refreshToken
        store.email = email
        res.user
    }

    /** Restores a session from the stored refresh token, or returns null. */
    suspend fun restore(): User? = withContext(Dispatchers.IO) {
        if (store.serverUrl.isBlank() || !refresh()) null else runCatching { me() }.getOrNull()
    }

    suspend fun logout() {
        withContext(Dispatchers.IO) {
            runCatching { exec<Unit>(request("POST", "/v1/auth/logout", null, accessToken), null) }
        }
        accessToken = null
        store.refreshToken = null
    }

    suspend fun me(): User = call("GET", "/v1/me", null, User.serializer())!!
    suspend fun devices(): List<Device> = call("GET", "/v1/devices?limit=200", null, DeviceList.serializer())!!.items
    suspend fun device(id: String): Device = call("GET", "/v1/devices/$id", null, Device.serializer())!!
    suspend fun transition(id: String, op: String): Device = call("POST", "/v1/devices/$id/$op", null, Device.serializer())!!

    suspend fun runCommand(deviceId: String, cmd: CreateCommand): Command =
        call("POST", "/v1/devices/$deviceId/commands", json.encodeToString(CreateCommand.serializer(), cmd),
            Command.serializer(), mapOf("Idempotency-Key" to UUID.randomUUID().toString()))!!

    suspend fun command(id: String): Command = call("GET", "/v1/commands/$id", null, Command.serializer())!!

    suspend fun createExitSession(deviceId: String, label: String): ExitSessionCreated =
        call("POST", "/v1/devices/$deviceId/exit-sessions", json.encodeToString(mapOf("client_label" to label)),
            ExitSessionCreated.serializer())!!

    suspend fun endExitSession(id: String) {
        call<Unit>("DELETE", "/v1/exit-sessions/$id", null, null)
    }
}
