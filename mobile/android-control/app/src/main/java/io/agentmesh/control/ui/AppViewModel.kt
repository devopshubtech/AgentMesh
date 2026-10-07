package io.agentmesh.control.ui

import android.app.Application
import android.os.Build
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import io.agentmesh.control.App
import io.agentmesh.control.data.ConnectProfile
import io.agentmesh.control.data.Links
import io.agentmesh.control.data.QrImage
import io.agentmesh.control.vpn.ExitVpnService
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

data class UiState(
    val profiles: List<ConnectProfile> = emptyList(),
    val message: String? = null,
    val showAdd: Boolean = false,
    val editing: ConnectProfile? = null,
    val exitIp: String? = null,
    val checkingIp: Boolean = false,
    val pairing: Boolean = false,
)

class AppViewModel(app: Application) : AndroidViewModel(app) {
    private val store = (app as App).profiles
    private val _state = MutableStateFlow(UiState(profiles = store.all()))
    val state: StateFlow<UiState> = _state

    private fun reload() = _state.update { it.copy(profiles = store.all()) }

    fun toast(msg: String?) = _state.update { it.copy(message = msg) }
    fun showAdd(show: Boolean) = _state.update { it.copy(showAdd = show) }
    fun edit(p: ConnectProfile?) = _state.update { it.copy(editing = p) }

    /** Saves a scanned/pasted/deep-linked connect link. Returns the saved server, or null if invalid. */
    fun importLink(raw: String): ConnectProfile? {
        val link = Links.parse(raw)
        if (link == null) {
            toast("That is not an AgentMesh connect link. Scan the QR code from 'Connect a phone' in the dashboard.")
            return null
        }
        val p = store.import(link)
        _state.update { it.copy(showAdd = false) }
        reload()
        return p
    }

    /** Reads the QR code in a picture chosen from the gallery and saves its link. */
    fun importQrImage(uri: android.net.Uri, onSaved: (ConnectProfile) -> Unit) {
        viewModelScope.launch {
            val app = getApplication<App>()
            val text = withContext(Dispatchers.Default) { runCatching { QrImage.read(app.contentResolver, uri) }.getOrNull() }
            if (text == null) {
                toast("No QR code found in that picture. Choose a clear screenshot of the QR code from Connect a phone.")
                return@launch
            }
            importLink(text)?.let(onSaved)
        }
    }

    /**
     * Handles what was typed in the Add dialog: a 6-digit pairing code is
     * exchanged with the server for a connect link; anything else is treated
     * as a pasted link. onSaved runs with the new server when it worked.
     */
    fun submitCodeOrLink(raw: String, onSaved: (ConnectProfile) -> Unit) {
        val digits = raw.filter { it.isDigit() }
        if (digits.length != 6 || raw.any { it.isLetter() }) {
            importLink(raw)?.let(onSaved)
            return
        }
        _state.update { it.copy(pairing = true) }
        viewModelScope.launch {
            val app = getApplication<App>()
            val link = runCatching { app.connect.pair(digits, "Android ${Build.MANUFACTURER} ${Build.MODEL}") }
            _state.update { it.copy(pairing = false) }
            link.onFailure { return@launch toast(it.message ?: "Pairing failed") }
            val p = store.import(link.getOrThrow())
            _state.update { it.copy(showAdd = false) }
            reload()
            onSaved(p)
        }
    }

    fun saveEdit(p: ConnectProfile, name: String, server: String, newLink: String) {
        var updated = p.copy(name = name.trim().ifBlank { p.name })
        if (newLink.isNotBlank()) {
            val link = Links.parse(newLink) ?: return toast("The new link is not a valid AgentMesh connect link")
            updated = store.replaceLink(updated, link)
        } else if (server.isNotBlank() && server.trim() != p.serverUrl) {
            val normalized = Links.normalizeServer(server) ?: return toast("Enter a server address like 203.0.113.7 or https://name.example.com")
            updated = updated.copy(serverUrl = normalized)
        }
        store.upsert(updated)
        _state.update { it.copy(editing = null) }
        reload()
        toast("Saved")
    }

    fun delete(p: ConnectProfile) {
        store.delete(p.id)
        _state.update { it.copy(editing = null) }
        reload()
    }

    /** Call after VPN consent was granted. */
    fun connect(p: ConnectProfile) {
        _state.update { it.copy(exitIp = null) }
        ExitVpnService.connect(getApplication(), p.id)
    }

    fun disconnect() {
        ExitVpnService.disconnect(getApplication())
        _state.update { it.copy(exitIp = null) }
        viewModelScope.launch { kotlinx.coroutines.delay(1500); reload() }
    }

    fun refresh() = reload()

    fun checkExitIp() {
        val eng = ExitVpnService.currentEngine ?: return toast("Not connected")
        _state.update { it.copy(checkingIp = true) }
        viewModelScope.launch {
            val ip = withContext(Dispatchers.IO) { runCatching { eng.publicIP() } }
            _state.update { it.copy(checkingIp = false, exitIp = ip.getOrNull()) }
            ip.exceptionOrNull()?.let { toast("Could not check IP: ${it.message}") }
        }
    }
}
