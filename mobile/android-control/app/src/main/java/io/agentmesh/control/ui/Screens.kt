package io.agentmesh.control.ui

import android.os.Build
import androidx.compose.foundation.clickable
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.LocalTextStyle
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.dynamicDarkColorScheme
import androidx.compose.material3.dynamicLightColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import io.agentmesh.control.BuildConfig
import io.agentmesh.control.data.ConnectProfile
import io.agentmesh.control.vpn.ExitVpnService
import io.agentmesh.control.vpn.VpnStatus
import java.text.DateFormat
import java.util.Date

private val Muted = Color(0xFF8A8F98)
private val Good = Color(0xFF16A34A)

@Composable
private fun AgentMeshTheme(content: @Composable () -> Unit) {
    val dark = isSystemInDarkTheme()
    val ctx = LocalContext.current
    val scheme = when {
        Build.VERSION.SDK_INT >= Build.VERSION_CODES.S -> if (dark) dynamicDarkColorScheme(ctx) else dynamicLightColorScheme(ctx)
        dark -> darkColorScheme()
        else -> lightColorScheme(primary = Color(0xFF3949AB))
    }
    MaterialTheme(colorScheme = scheme, content = content)
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun AgentMeshApp(vm: AppViewModel, onScan: () -> Unit, onPickImage: () -> Unit, onConnect: (ConnectProfile) -> Unit) {
    val st by vm.state.collectAsState()
    val vpn by ExitVpnService.status.collectAsState()
    val snackbar = remember { SnackbarHostState() }
    LaunchedEffect(st.message) {
        st.message?.let {
            snackbar.showSnackbar(it)
            vm.toast(null)
        }
    }
    LaunchedEffect(vpn) { if (vpn is VpnStatus.Connected || vpn is VpnStatus.Idle) vm.refresh() }

    AgentMeshTheme {
        Scaffold(
            snackbarHost = { SnackbarHost(snackbar) },
            topBar = { TopAppBar(title = { Text("AgentMesh") }) },
        ) { pad ->
            Column(
                Modifier.padding(pad).fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp),
                verticalArrangement = Arrangement.spacedBy(14.dp),
            ) {
                StatusCard(vpn, st.exitIp, st.checkingIp, onCheckIp = vm::checkExitIp, onStop = vm::disconnect)

                if (st.profiles.isEmpty()) {
                    WelcomeCard(onAdd = { vm.showAdd(true) })
                } else {
                    Text("Saved servers", fontWeight = FontWeight.SemiBold, fontSize = 18.sp)
                    st.profiles.forEach { p ->
                        val active = (vpn as? VpnStatus.Connected)?.deviceName?.let { it == p.deviceName || it == p.name } == true
                        ProfileCard(p, active, onConnect = { onConnect(p) }, onEdit = { vm.edit(p) })
                    }
                    OutlinedButton(onClick = { vm.showAdd(true) }, modifier = Modifier.fillMaxWidth().height(52.dp)) {
                        Text("+ Connect to remote server")
                    }
                }
                Spacer(Modifier.height(8.dp))
                Text("AgentMesh v${BuildConfig.VERSION_NAME}", color = Muted, fontSize = 12.sp)
            }
        }

        if (st.showAdd) AddServerDialog(busy = st.pairing, onDismiss = { vm.showAdd(false) }, onScan = onScan, onPickImage = onPickImage, onSubmit = { text ->
            vm.submitCodeOrLink(text, onConnect)
        })
        st.editing?.let { p ->
            EditServerDialog(p, onDismiss = { vm.edit(null) }, onSave = { name, server, link -> vm.saveEdit(p, name, server, link) },
                onDelete = { vm.delete(p) })
        }
    }
}

@Composable
private fun WelcomeCard(onAdd: () -> Unit) {
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(20.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            Text("Use another device's internet", fontWeight = FontWeight.SemiBold, fontSize = 20.sp)
            Text(
                "Connect this phone to a computer running AgentMesh. Your internet will go out through that computer and websites will see its IP address — on mobile data or any Wi-Fi.",
                fontSize = 14.sp,
            )
            Button(onClick = onAdd, modifier = Modifier.fillMaxWidth().height(56.dp)) {
                Text("Connect to remote server", fontSize = 16.sp)
            }
            Text("You need the QR code or 6-digit pairing code from the AgentMesh dashboard → Connect a phone.", color = Muted, fontSize = 12.sp)
        }
    }
}

@Composable
private fun ProfileCard(p: ConnectProfile, active: Boolean, onConnect: () -> Unit, onEdit: () -> Unit) {
    Card(
        Modifier.fillMaxWidth().clickable(onClick = onConnect),
        colors = if (active) CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.primaryContainer) else CardDefaults.cardColors(),
    ) {
        Row(Modifier.padding(16.dp), verticalAlignment = Alignment.CenterVertically) {
            Column(Modifier.weight(1f)) {
                Text(p.name, fontWeight = FontWeight.SemiBold, fontSize = 16.sp)
                val via = p.deviceName.takeIf { it.isNotBlank() && it != p.name }?.let { "via $it · " } ?: ""
                Text(via + p.serverUrl.removePrefix("https://"), color = Muted, fontSize = 12.sp, maxLines = 1)
                if (p.lastConnectedAt > 0) {
                    Text("Last connected " + DateFormat.getDateTimeInstance(DateFormat.SHORT, DateFormat.SHORT).format(Date(p.lastConnectedAt)),
                        color = Muted, fontSize = 11.sp)
                }
            }
            TextButton(onClick = onEdit) { Text("Edit") }
            if (active) Text("Connected", color = Good, fontWeight = FontWeight.SemiBold)
            else Button(onClick = onConnect) { Text("Connect") }
        }
    }
}

@Composable
private fun StatusCard(vpn: VpnStatus, exitIp: String?, checking: Boolean, onCheckIp: () -> Unit, onStop: () -> Unit) {
    val (title, detail) = when (vpn) {
        VpnStatus.Idle -> return
        is VpnStatus.Connecting -> "Connecting to ${vpn.deviceName}…" to null
        is VpnStatus.Reconnecting -> "Reconnecting to ${vpn.deviceName} (attempt ${vpn.attempt})…" to vpn.reason
        is VpnStatus.Connected -> "Connected — internet via ${vpn.deviceName}" to
            "↑ ${ExitVpnService.human(vpn.bytesUp)}   ↓ ${ExitVpnService.human(vpn.bytesDown)}"
        is VpnStatus.Failed -> "Disconnected" to vpn.message
    }
    Card(Modifier.fillMaxWidth(), colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.secondaryContainer)) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(title, fontWeight = FontWeight.SemiBold, fontSize = 17.sp)
            detail?.let { Text(it, fontSize = 13.sp) }
            if (vpn is VpnStatus.Connecting || vpn is VpnStatus.Reconnecting) LinearProgressIndicator(Modifier.fillMaxWidth())
            if (vpn is VpnStatus.Connected) {
                Text(if (exitIp != null) "Your IP now: $exitIp" else "Tap 'Check my IP' to see the IP websites see.", fontSize = 14.sp,
                    fontWeight = if (exitIp != null) FontWeight.SemiBold else FontWeight.Normal)
            }
            if (vpn !is VpnStatus.Failed) {
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    if (vpn is VpnStatus.Connected) OutlinedButton(onClick = onCheckIp, enabled = !checking) { Text(if (checking) "Checking…" else "Check my IP") }
                    Button(onClick = onStop) { Text("Disconnect") }
                }
            }
        }
    }
}

@Composable
private fun AddServerDialog(busy: Boolean, onDismiss: () -> Unit, onScan: () -> Unit, onPickImage: () -> Unit, onSubmit: (String) -> Unit) {
    var text by remember { mutableStateOf("") }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("Connect to remote server") },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
                Button(onClick = onScan, enabled = !busy, modifier = Modifier.fillMaxWidth().height(52.dp)) { Text("Scan QR code") }
                OutlinedButton(onClick = onPickImage, enabled = !busy, modifier = Modifier.fillMaxWidth().height(52.dp)) {
                    Text("Choose QR code from gallery")
                }
                Text("Tip: take a screenshot of the QR code, then choose it here.", color = Muted, fontSize = 12.sp)
                Text("or enter the 6-digit pairing code (or paste the link):", color = Muted, fontSize = 13.sp)
                OutlinedTextField(text, { text = it }, placeholder = { Text("123 456") }, singleLine = true, enabled = !busy,
                    textStyle = LocalTextStyle.current.copy(fontSize = 22.sp, letterSpacing = 4.sp),
                    modifier = Modifier.fillMaxWidth(), keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number))
                Text("Get the code from the dashboard → Connect a phone → Pairing code.", color = Muted, fontSize = 12.sp)
                if (busy) LinearProgressIndicator(Modifier.fillMaxWidth())
            }
        },
        confirmButton = { Button(onClick = { onSubmit(text.trim()) }, enabled = text.isNotBlank() && !busy) { Text(if (busy) "Pairing…" else "Connect") } },
        dismissButton = { TextButton(onClick = onDismiss) { Text("Cancel") } },
    )
}

@Composable
private fun EditServerDialog(
    p: ConnectProfile, onDismiss: () -> Unit, onSave: (String, String, String) -> Unit, onDelete: () -> Unit,
) {
    var name by remember { mutableStateOf(p.name) }
    var server by remember { mutableStateOf(p.serverUrl) }
    var link by remember { mutableStateOf("") }
    var confirmDelete by remember { mutableStateOf(false) }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("Edit saved server") },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                OutlinedTextField(name, { name = it }, label = { Text("Name") }, singleLine = true, modifier = Modifier.fillMaxWidth())
                OutlinedTextField(server, { server = it }, label = { Text("Server address") }, singleLine = true,
                    supportingText = { Text("IP, domain or https URL. Updated automatically when it changes.") },
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri), modifier = Modifier.fillMaxWidth())
                OutlinedTextField(link, { link = it }, label = { Text("New connect link (optional)") }, minLines = 2,
                    supportingText = { Text("Paste a new link if the old one was revoked.") }, modifier = Modifier.fillMaxWidth())
                if (confirmDelete) {
                    Text("Delete '${p.name}' from this phone?", color = MaterialTheme.colorScheme.error, fontWeight = FontWeight.SemiBold)
                }
            }
        },
        confirmButton = { Button(onClick = { onSave(name, server, link) }) { Text("Save") } },
        dismissButton = {
            TextButton(onClick = { if (confirmDelete) onDelete() else confirmDelete = true }) {
                Text(if (confirmDelete) "Yes, delete" else "Delete", color = MaterialTheme.colorScheme.error)
            }
        },
    )
}
