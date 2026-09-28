package io.agentmesh.control.ui

import android.os.Build
import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.Checkbox
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LinearProgressIndicator
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
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import io.agentmesh.control.data.CreateCommand
import io.agentmesh.control.data.Device
import io.agentmesh.control.vpn.ExitVpnService
import io.agentmesh.control.vpn.VpnStatus

private val Online = Color(0xFF16A34A)
private val Muted = Color(0xFF9CA3AF)

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
fun AgentMeshApp(vm: AppViewModel, onUseExitNode: (Device) -> Unit) {
    val st by vm.state.collectAsState()
    val vpn by ExitVpnService.status.collectAsState()
    val snackbar = remember { SnackbarHostState() }
    LaunchedEffect(st.message) {
        st.message?.let {
            snackbar.showSnackbar(it)
            vm.toast(null)
        }
    }
    AgentMeshTheme {
        Scaffold(
            snackbarHost = { SnackbarHost(snackbar) },
            topBar = {
                TopAppBar(
                    title = {
                        Text(
                            when (val s = st.screen) {
                                is Screen.DeviceDetail -> st.device?.name ?: "Device"
                                Screen.Setup -> "Servers"
                                Screen.Login -> "Sign in"
                                else -> "AgentMesh"
                            }
                        )
                    },
                    navigationIcon = {
                        if (st.screen is Screen.DeviceDetail) {
                            IconButton(onClick = vm::back) { Icon(Icons.AutoMirrored.Filled.ArrowBack, "Back") }
                        }
                        if (st.screen is Screen.Setup && vm.savedServer.isNotBlank()) {
                            IconButton(onClick = { vm.selectServer(vm.savedServer) }) { Icon(Icons.AutoMirrored.Filled.ArrowBack, "Back") }
                        }
                    },
                    actions = {
                        if (st.screen is Screen.Devices || st.screen is Screen.DeviceDetail) {
                            IconButton(onClick = vm::refresh) { Icon(Icons.Filled.Refresh, "Refresh") }
                        }
                        if (st.screen is Screen.Login || st.screen is Screen.Devices) {
                            IconButton(onClick = vm::openSetup) { Icon(Icons.Filled.Settings, "Server settings") }
                        }
                    },
                )
            },
        ) { pad ->
            Column(Modifier.padding(pad).fillMaxSize()) {
                if (st.busy) LinearProgressIndicator(Modifier.fillMaxWidth()) else Spacer(Modifier.height(4.dp))
                VpnBanner(vpn, st.exitIp, onCheckIp = vm::checkExitIp, onStop = vm::stopExit)
                when (val s = st.screen) {
                    Screen.Loading -> Box(Modifier.fillMaxSize(), Alignment.Center) { CircularProgressIndicator() }
                    Screen.Setup -> {
                        BackHandler(enabled = vm.savedServer.isNotBlank()) { vm.selectServer(vm.savedServer) }
                        SetupScreen(vm)
                    }
                    Screen.Login -> LoginScreen(vm)
                    Screen.Devices -> {
                        ServerBar(vm.savedServer, onSwitch = vm::openSetup)
                        DevicesScreen(st.devices, st.user?.email.orEmpty(), vm::openDevice, vm::logout)
                    }
                    is Screen.DeviceDetail -> {
                        BackHandler(onBack = vm::back)
                        st.device?.let { DeviceScreen(it, st, vm, vpn, onUseExitNode) }
                    }
                }
            }
        }
    }
}

@Composable
private fun VpnBanner(vpn: VpnStatus, exitIp: String?, onCheckIp: () -> Unit, onStop: () -> Unit) {
    val (title, detail) = when (vpn) {
        VpnStatus.Idle -> return
        is VpnStatus.Connecting -> "Connecting via ${vpn.deviceName}…" to null
        is VpnStatus.Connected -> "Internet via ${vpn.deviceName}" to
            "↑ ${ExitVpnService.human(vpn.bytesUp)}  ↓ ${ExitVpnService.human(vpn.bytesDown)}  ·  ${vpn.flows} flows" +
            (if (vpn.failedFlows > 0) " (${vpn.failedFlows} refused)" else "") +
            (exitIp?.let { "\nExit IP: $it" } ?: "")
        is VpnStatus.Failed -> "Exit node disconnected" to vpn.message
    }
    Card(
        Modifier.fillMaxWidth().padding(12.dp),
        colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.primaryContainer),
    ) {
        Column(Modifier.padding(12.dp)) {
            Text(title, fontWeight = FontWeight.SemiBold)
            detail?.let { Text(it, fontSize = 13.sp) }
            if (vpn is VpnStatus.Connected || vpn is VpnStatus.Connecting) {
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp), modifier = Modifier.padding(top = 8.dp)) {
                    if (vpn is VpnStatus.Connected) OutlinedButton(onClick = onCheckIp) { Text("Check my IP") }
                    Button(onClick = onStop) { Text("Disconnect") }
                }
            }
        }
    }
}

@Composable
private fun SetupScreen(vm: AppViewModel) {
    val st by vm.state.collectAsState()
    var url by rememberSaveable { mutableStateOf("https://") }
    var relayViaServer by remember { mutableStateOf(vm.relayViaServer) }
    Column(Modifier.padding(16.dp).verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(12.dp)) {
        if (vm.servers.isNotEmpty()) {
            Text("Saved servers", fontWeight = FontWeight.SemiBold)
            vm.servers.forEach { s ->
                Card(Modifier.fillMaxWidth()) {
                    Row(Modifier.padding(horizontal = 12.dp, vertical = 6.dp), verticalAlignment = Alignment.CenterVertically) {
                        Column(Modifier.weight(1f)) {
                            Text(s, fontSize = 14.sp)
                            if (s == vm.savedServer) Text("current", fontSize = 11.sp, color = MaterialTheme.colorScheme.primary)
                        }
                        if (s != vm.savedServer) TextButton(onClick = { vm.selectServer(s) }) { Text("Use") }
                        TextButton(onClick = { vm.removeServer(s) }) { Text("Remove") }
                    }
                }
            }
            HorizontalDivider()
        }
        Text("Add a server", fontWeight = FontWeight.SemiBold)
        Text(
            "Use the public address to connect from mobile data or any network (for example " +
                "https://your-name.trycloudflare.com or https://mesh.example.com), or the LAN address " +
                "(https://192.168.x.x:13443) on the same Wi-Fi.", fontSize = 13.sp)
        OutlinedTextField(url, { url = it }, label = { Text("Server URL") }, singleLine = true,
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri), modifier = Modifier.fillMaxWidth())
        Button(onClick = { vm.saveServer(url, trustCandidate = false) }, modifier = Modifier.fillMaxWidth()) {
            Text("Continue (publicly trusted certificate)")
        }
        OutlinedButton(onClick = { vm.fetchCa(url) }, modifier = Modifier.fillMaxWidth()) {
            Text("Private / development server: trust its CA…")
        }
        if (vm.hasCustomCa) TextButton(onClick = vm::clearCa) { Text("Remove custom CA of the current server") }
        HorizontalDivider()
        Row(verticalAlignment = Alignment.CenterVertically) {
            Checkbox(checked = relayViaServer, onCheckedChange = { relayViaServer = it; vm.relayViaServer = it })
            Text("Send exit-node traffic through the server URL (works on mobile data). Recommended.", fontSize = 13.sp)
        }
    }
    st.caCandidate?.let { c ->
        AlertDialog(
            onDismissRequest = vm::rejectCa,
            title = { Text("Trust this certificate authority?") },
            text = {
                Column {
                    Text(c.subject, fontWeight = FontWeight.SemiBold)
                    Spacer(Modifier.height(8.dp))
                    Text("SHA-256 fingerprint:")
                    Text(c.fingerprint, fontFamily = FontFamily.Monospace, fontSize = 12.sp)
                    Spacer(Modifier.height(8.dp))
                    Text("Only trust it if this matches the fingerprint printed by the server's dev-setup (amctl dev-certs).")
                }
            },
            confirmButton = { Button(onClick = { vm.saveServer(url, trustCandidate = true) }) { Text("Trust & continue") } },
            dismissButton = { TextButton(onClick = vm::rejectCa) { Text("Cancel") } },
        )
    }
}

@Composable
private fun ServerBar(server: String, onSwitch: () -> Unit) {
    Card(Modifier.fillMaxWidth().padding(horizontal = 12.dp, vertical = 4.dp)) {
        Row(Modifier.padding(horizontal = 12.dp, vertical = 4.dp), verticalAlignment = Alignment.CenterVertically) {
            Column(Modifier.weight(1f)) {
                Text("Server", fontSize = 11.sp, color = Muted)
                Text(server, fontSize = 13.sp)
            }
            TextButton(onClick = onSwitch) { Text("Switch server") }
        }
    }
}

@Composable
private fun LoginScreen(vm: AppViewModel) {
    var email by rememberSaveable { mutableStateOf(vm.savedEmail) }
    var password by remember { mutableStateOf("") }
    Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
        ServerBar(vm.savedServer, onSwitch = vm::openSetup)
        OutlinedTextField(email, { email = it }, label = { Text("Email") }, singleLine = true,
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Email), modifier = Modifier.fillMaxWidth())
        OutlinedTextField(password, { password = it }, label = { Text("Password") }, singleLine = true,
            visualTransformation = PasswordVisualTransformation(),
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password), modifier = Modifier.fillMaxWidth())
        Button(onClick = { vm.login(email, password) }, enabled = email.isNotBlank() && password.isNotBlank(),
            modifier = Modifier.fillMaxWidth()) { Text("Sign in") }
    }
}

@Composable
private fun Dot(online: Boolean) =
    Box(Modifier.size(10.dp).background(if (online) Online else Muted, CircleShape))

@Composable
private fun DevicesScreen(devices: List<Device>, email: String, open: (Device) -> Unit, logout: () -> Unit) {
    LazyColumn(contentPadding = PaddingValues(bottom = 24.dp)) {
        item {
            Row(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp), verticalAlignment = Alignment.CenterVertically) {
                Text("${devices.count { it.online }} of ${devices.size} online", Modifier.weight(1f), fontWeight = FontWeight.SemiBold)
                TextButton(onClick = logout) { Text("Sign out") }
            }
            Text(email, Modifier.padding(horizontal = 16.dp), color = Muted, fontSize = 12.sp)
        }
        if (devices.isEmpty()) item { Text("No devices yet. Enroll an agent from the dashboard.", Modifier.padding(16.dp)) }
        items(devices, key = { it.id }) { d ->
            Row(
                Modifier.fillMaxWidth().clickable { open(d) }.padding(horizontal = 16.dp, vertical = 12.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Dot(d.online)
                Spacer(Modifier.width(12.dp))
                Column(Modifier.weight(1f)) {
                    Text(d.name, fontWeight = FontWeight.SemiBold)
                    Text("${d.osName} ${d.osVersion}".trim().ifBlank { d.platform }, fontSize = 13.sp, color = Muted)
                }
                Column(horizontalAlignment = Alignment.End) {
                    Text(if (d.status == "active") d.connectivity else d.status, fontSize = 13.sp)
                    if (d.exitNodeCapable) Text("exit node", fontSize = 11.sp, color = MaterialTheme.colorScheme.primary)
                }
            }
            HorizontalDivider()
        }
    }
}

@Composable
private fun InfoRow(k: String, v: String) {
    Row(Modifier.fillMaxWidth().padding(vertical = 3.dp)) {
        Text(k, Modifier.width(110.dp), color = Muted, fontSize = 14.sp)
        Text(v, fontSize = 14.sp)
    }
}

private fun gb(b: Long) = "%.1f GB".format(b / 1_073_741_824.0)

@Composable
private fun DeviceScreen(d: Device, st: UiState, vm: AppViewModel, vpn: VpnStatus, onUseExitNode: (Device) -> Unit) {
    val user = st.user
    var cmdLine by rememberSaveable { mutableStateOf("") }
    Column(Modifier.padding(16.dp).verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(12.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Dot(d.online)
            Spacer(Modifier.width(8.dp))
            Text("${d.status} · ${d.connectivity}", fontWeight = FontWeight.SemiBold)
        }
        Card(Modifier.fillMaxWidth()) {
            Column(Modifier.padding(12.dp)) {
                InfoRow("OS", "${d.osName} ${d.osVersion}".trim())
                InfoRow("Platform", "${d.platform}/${d.arch}")
                InfoRow("Agent", d.agentVersion)
                d.inventory?.let { inv ->
                    InfoRow("CPU", "${inv.cpu.model} (${inv.cpu.cores}c/${inv.cpu.threads}t)")
                    InfoRow("Memory", "${gb(inv.memory.usedBytes)} / ${gb(inv.memory.totalBytes)}")
                    inv.disks.forEach { InfoRow("Disk ${it.mount}", "${gb(it.usedBytes)} / ${gb(it.totalBytes)}") }
                    inv.network.take(4).forEach { InfoRow(it.name, it.addrs.joinToString(", ")) }
                }
                d.lastIp?.let { InfoRow("Last IP", it) }
                d.lastSeenAt?.let { InfoRow("Last seen", it.replace('T', ' ').take(19)) }
            }
        }

        // Exit node
        if (user?.can("sessions.exit_node") == true) {
            Card(Modifier.fillMaxWidth()) {
                Column(Modifier.padding(12.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    Text("Exit node", fontWeight = FontWeight.SemiBold)
                    when {
                        !d.exitNodeCapable -> Text(
                            "This device does not allow exit-node sessions. Enable it on the device with " +
                                "\"allow_exit_node\": true in the agent's local policy.", fontSize = 13.sp)
                        !d.online -> Text("The device must be online.", fontSize = 13.sp)
                        vpn is VpnStatus.Connected || vpn is VpnStatus.Connecting ->
                            Text("Your phone's internet traffic is routed through a device. See the banner above.", fontSize = 13.sp)
                        else -> {
                            Text("Route this phone's internet traffic through ${d.name}. Websites will see its IP address.", fontSize = 13.sp)
                            Button(onClick = { onUseExitNode(d) }, modifier = Modifier.fillMaxWidth()) { Text("Use as exit node") }
                        }
                    }
                }
            }
        }

        // Commands
        val canAction = user?.can("commands.execute.action") == true
        val canExec = user?.can("commands.execute.exec") == true && "exec" in d.capabilities
        if ((canAction || canExec) && d.status == "active") {
            Card(Modifier.fillMaxWidth()) {
                Column(Modifier.padding(12.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    Text("Commands", fontWeight = FontWeight.SemiBold)
                    if (canAction) Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        OutlinedButton(onClick = { vm.runCommand(CreateCommand(kind = "action", action = "ping")) }) { Text("Ping") }
                        OutlinedButton(onClick = { vm.runCommand(CreateCommand(kind = "action", action = "inventory.refresh")) }) { Text("Refresh info") }
                    }
                    if (canExec) {
                        OutlinedTextField(cmdLine, { cmdLine = it }, label = { Text("Command (runs in the device shell)") },
                            singleLine = true, modifier = Modifier.fillMaxWidth())
                        Button(onClick = {
                            val shell = "exec.shell" in d.capabilities
                            vm.runCommand(
                                if (shell) CreateCommand(kind = "exec", argv = listOf(cmdLine), shell = true)
                                else CreateCommand(kind = "exec", argv = cmdLine.trim().split(Regex("\\s+")))
                            )
                        }, enabled = cmdLine.isNotBlank()) { Text("Run") }
                    }
                    st.lastCommand?.let { c ->
                        HorizontalDivider()
                        Text("Status: ${c.status}" + (c.result?.exitCode?.let { " · exit $it" } ?: ""), fontSize = 13.sp)
                        c.result?.let { r ->
                            val out = (r.stdout + (if (r.stderr.isNotBlank()) "\n[stderr]\n" + r.stderr else "")).ifBlank { r.error.orEmpty() }
                            Text(out.take(4000), fontFamily = FontFamily.Monospace, fontSize = 12.sp)
                        }
                    }
                }
            }
        }

        // Access management
        if (user?.can("devices.manage") == true) {
            Card(Modifier.fillMaxWidth()) {
                Column(Modifier.padding(12.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    Text("Access", fontWeight = FontWeight.SemiBold)
                    Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        when (d.status) {
                            "pending" -> Button(onClick = { vm.transition("approve") }) { Text("Approve") }
                            "active" -> OutlinedButton(onClick = { vm.transition("disable") }) { Text("Disable") }
                            "disabled" -> Button(onClick = { vm.transition("enable") }) { Text("Enable") }
                        }
                    }
                }
            }
        }
        Spacer(Modifier.height(24.dp))
    }
}
