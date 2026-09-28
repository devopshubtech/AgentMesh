package io.agentmesh.control.ui

import android.app.Application
import android.os.Build
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import io.agentmesh.control.App
import io.agentmesh.control.data.ApiException
import io.agentmesh.control.data.Command
import io.agentmesh.control.data.CreateCommand
import io.agentmesh.control.data.Device
import io.agentmesh.control.data.Tls
import io.agentmesh.control.data.User
import io.agentmesh.control.vpn.ExitVpnService
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

sealed interface Screen {
    data object Loading : Screen
    data object Setup : Screen
    data object Login : Screen
    data object Devices : Screen
    data class DeviceDetail(val id: String) : Screen
}

data class CaCandidate(val fingerprint: String, val pem: String, val subject: String)

data class UiState(
    val screen: Screen = Screen.Loading,
    val user: User? = null,
    val devices: List<Device> = emptyList(),
    val device: Device? = null,
    val busy: Boolean = false,
    val message: String? = null,
    val caCandidate: CaCandidate? = null,
    val lastCommand: Command? = null,
    val exitIp: String? = null,
)

class AppViewModel(app: Application) : AndroidViewModel(app) {
    private val appCtx = app as App
    private val api = appCtx.api
    private val store = appCtx.store
    private val _state = MutableStateFlow(UiState())
    val state: StateFlow<UiState> = _state
    private var poller: Job? = null

    val savedServer get() = store.serverUrl
    val savedEmail get() = store.email
    val hasCustomCa get() = store.caPem.isNotBlank()
    val servers get() = store.servers
    var relayViaServer: Boolean
        get() = store.relayViaServer
        set(v) { store.relayViaServer = v }

    /** Switches to a saved server, reusing its stored session when still valid. */
    fun selectServer(url: String) = launchBusy {
        ExitVpnService.disconnect(getApplication())
        store.serverUrl = url
        api.reconfigure()
        _state.update { UiState(screen = Screen.Loading) }
        val user = runCatching { api.restore() }.getOrNull()
        if (user == null) go(Screen.Login) else {
            _state.update { it.copy(user = user) }
            go(Screen.Devices)
        }
    }

    fun removeServer(url: String) {
        store.removeServer(url)
        if (store.serverUrl.isBlank()) go(Screen.Setup) else api.reconfigure()
        _state.update { it.copy(message = "Removed $url") }
    }

    init {
        viewModelScope.launch {
            if (store.serverUrl.isBlank()) return@launch go(Screen.Setup)
            val user = runCatching { api.restore() }.getOrNull()
            if (user == null) go(Screen.Login) else {
                _state.update { it.copy(user = user) }
                go(Screen.Devices)
            }
        }
    }

    private fun go(s: Screen) {
        _state.update { it.copy(screen = s, message = null) }
        poller?.cancel()
        if (s is Screen.Devices || s is Screen.DeviceDetail) {
            poller = viewModelScope.launch {
                while (isActive) {
                    reload(quiet = true)
                    delay(10_000)
                }
            }
        }
    }

    fun toast(msg: String?) = _state.update { it.copy(message = msg) }

    private fun friendly(e: Throwable): String = when (e) {
        is ApiException -> e.message
        is javax.net.ssl.SSLException -> "TLS error: ${e.message}. Is the server certificate trusted?"
        is java.net.UnknownHostException, is java.net.ConnectException -> "Cannot reach the server: ${e.message}"
        else -> e.message ?: e.javaClass.simpleName
    }

    private fun launchBusy(block: suspend () -> Unit) = viewModelScope.launch {
        _state.update { it.copy(busy = true) }
        try {
            block()
        } catch (e: ApiException) {
            if (e.status == 401) {
                _state.update { it.copy(user = null) }
                go(Screen.Login)
            }
            toast(friendly(e))
        } catch (e: Exception) {
            toast(friendly(e))
        } finally {
            _state.update { it.copy(busy = false) }
        }
    }

    // ------------------------------------------------------------ server setup

    fun openSetup() = go(Screen.Setup)

    private fun normalize(url: String): String? {
        val u = url.trim().trimEnd('/')
        return if (u.startsWith("https://") && u.length > 10) u else null
    }

    /** Downloads the server's CA for trust-on-first-use confirmation. */
    fun fetchCa(url: String) = launchBusy {
        val u = normalize(url) ?: return@launchBusy toast("Server URL must start with https://")
        val ca = withContext(Dispatchers.IO) { Tls.fetchServerCa(u) }
        _state.update { it.copy(caCandidate = CaCandidate(Tls.fingerprint(ca), Tls.toPem(ca), ca.subjectX500Principal.name)) }
    }

    fun rejectCa() = _state.update { it.copy(caCandidate = null) }

    fun saveServer(url: String, trustCandidate: Boolean) {
        val u = normalize(url) ?: return toast("Server URL must start with https://")
        val candidate = _state.value.caCandidate
        _state.update { it.copy(caCandidate = null) }
        store.serverUrl = u
        if (trustCandidate && candidate != null) store.caPem = candidate.pem
        selectServer(u)
    }

    fun clearCa() {
        store.caPem = ""
        api.reconfigure()
        toast("Custom CA removed; only system-trusted certificates are accepted")
    }

    // ------------------------------------------------------------ auth

    fun login(email: String, password: String) = launchBusy {
        val user = api.login(email.trim(), password)
        _state.update { it.copy(user = user) }
        go(Screen.Devices)
    }

    fun logout() = launchBusy {
        ExitVpnService.disconnect(getApplication())
        api.logout()
        _state.update { UiState(screen = Screen.Login) }
        go(Screen.Login)
    }

    // ------------------------------------------------------------ devices

    private suspend fun reload(quiet: Boolean) {
        try {
            when (val s = _state.value.screen) {
                is Screen.Devices -> {
                    val list = api.devices().filter { it.status != "revoked" }
                    _state.update { it.copy(devices = list) }
                }
                is Screen.DeviceDetail -> {
                    val d = api.device(s.id)
                    _state.update { it.copy(device = d) }
                }
                else -> Unit
            }
        } catch (e: Exception) {
            if (!quiet) toast(friendly(e))
        }
    }

    fun refresh() = launchBusy { reload(quiet = false) }

    fun openDevice(d: Device) {
        _state.update { it.copy(device = d, lastCommand = null) }
        go(Screen.DeviceDetail(d.id))
    }

    fun back() {
        if (_state.value.screen is Screen.DeviceDetail) go(Screen.Devices)
    }

    fun transition(op: String) = launchBusy {
        val id = _state.value.device?.id ?: return@launchBusy
        val d = api.transition(id, op)
        _state.update { it.copy(device = d) }
        toast("Device ${d.status}")
    }

    fun runCommand(cmd: CreateCommand) = launchBusy {
        val id = _state.value.device?.id ?: return@launchBusy
        var c = api.runCommand(id, cmd)
        _state.update { it.copy(lastCommand = c) }
        repeat(60) {
            if (c.finished) return@repeat
            delay(1000)
            c = api.command(c.id)
            _state.update { it.copy(lastCommand = c) }
        }
    }

    // ------------------------------------------------------------ exit node

    /** Call after VPN consent was granted. */
    fun startExit(d: Device) = launchBusy {
        val label = "Android ${Build.MANUFACTURER} ${Build.MODEL}"
        val s = api.createExitSession(d.id, label)
        // Relaying via the URL this app already reaches works from any network
        // (Wi-Fi or mobile data); the advertised gateway address may be LAN-only.
        val relay = if (store.relayViaServer) api.serverUrl + "/v1/relay" else s.relayUrl
        ExitVpnService.connect(getApplication(), relay, s.ticket, api.caPem, s.session.id, d.name)
        _state.update { it.copy(exitIp = null) }
    }

    fun stopExit() {
        ExitVpnService.disconnect(getApplication())
        _state.update { it.copy(exitIp = null) }
    }

    fun checkExitIp() = launchBusy {
        val eng = ExitVpnService.currentEngine ?: return@launchBusy toast("Tunnel is not connected")
        val ip = withContext(Dispatchers.IO) { eng.publicIP() }
        _state.update { it.copy(exitIp = ip) }
    }
}
