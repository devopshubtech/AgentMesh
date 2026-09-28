package io.agentmesh.control.data

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

// Mirrors docs/api.md (snake_case JSON).

@Serializable
data class User(
    val id: String,
    val email: String,
    @SerialName("display_name") val displayName: String = "",
    val role: String = "",
    val permissions: List<String> = emptyList(),
) {
    fun can(p: String) = p in permissions
}

@Serializable
data class TokenResponse(
    @SerialName("access_token") val accessToken: String,
    @SerialName("expires_in") val expiresIn: Int,
    val user: User,
    @SerialName("refresh_token") val refreshToken: String? = null,
)

@Serializable
data class Cpu(val model: String = "", val cores: Int = 0, val threads: Int = 0)

@Serializable
data class Memory(@SerialName("total_bytes") val totalBytes: Long = 0, @SerialName("used_bytes") val usedBytes: Long = 0)

@Serializable
data class Disk(
    val mount: String = "",
    val fstype: String = "",
    @SerialName("total_bytes") val totalBytes: Long = 0,
    @SerialName("used_bytes") val usedBytes: Long = 0,
)

@Serializable
data class NetIf(val name: String = "", val mac: String = "", val addrs: List<String> = emptyList())

@Serializable
data class Inventory(
    val cpu: Cpu = Cpu(),
    val memory: Memory = Memory(),
    val disks: List<Disk> = emptyList(),
    val network: List<NetIf> = emptyList(),
    @SerialName("uptime_s") val uptimeS: Long = 0,
)

@Serializable
data class Device(
    val id: String,
    val name: String,
    val hostname: String = "",
    val status: String,
    val connectivity: String,
    val platform: String = "",
    val arch: String = "",
    @SerialName("os_name") val osName: String = "",
    @SerialName("os_version") val osVersion: String = "",
    @SerialName("agent_version") val agentVersion: String = "",
    val capabilities: List<String> = emptyList(),
    val inventory: Inventory? = null,
    @SerialName("last_seen_at") val lastSeenAt: String? = null,
    @SerialName("last_ip") val lastIp: String? = null,
) {
    val online get() = connectivity == "online"
    val exitNodeCapable get() = "exit_node" in capabilities
}

@Serializable
data class DeviceList(val items: List<Device>, @SerialName("next_cursor") val nextCursor: String? = null)

@Serializable
data class CommandResult(
    @SerialName("exit_code") val exitCode: Int? = null,
    val stdout: String = "",
    val stderr: String = "",
    val truncated: Boolean = false,
    val error: String? = null,
    @SerialName("duration_ms") val durationMs: Long = 0,
)

@Serializable
data class Command(
    val id: String,
    val kind: String,
    val action: String? = null,
    val argv: List<String>? = null,
    val status: String,
    val result: CommandResult? = null,
) {
    val finished get() = status !in setOf("queued", "sent", "acked", "running")
}

@Serializable
data class CreateCommand(
    val kind: String,
    val action: String? = null,
    val argv: List<String>? = null,
    val shell: Boolean = false,
    @SerialName("timeout_s") val timeoutS: Int = 60,
)

@Serializable
data class ExitSession(val id: String, @SerialName("device_id") val deviceId: String, val status: String)

@Serializable
data class ExitSessionCreated(
    val session: ExitSession,
    @SerialName("relay_url") val relayUrl: String,
    val ticket: String,
)

@Serializable
data class ApiErrorBody(val error: ApiErrorDetail)

@Serializable
data class ApiErrorDetail(val code: String, val message: String, @SerialName("request_id") val requestId: String = "")

class ApiException(val status: Int, val code: String, override val message: String) : Exception(message)
