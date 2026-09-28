package io.agentmesh.control

import android.Manifest
import android.net.VpnService
import android.os.Build
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.activity.result.contract.ActivityResultContracts
import androidx.activity.viewModels
import io.agentmesh.control.data.Device
import io.agentmesh.control.ui.AgentMeshApp
import io.agentmesh.control.ui.AppViewModel

class MainActivity : ComponentActivity() {
    private val vm: AppViewModel by viewModels()
    private var pendingExit: Device? = null

    private val vpnConsent = registerForActivityResult(ActivityResultContracts.StartActivityForResult()) { res ->
        val d = pendingExit
        pendingExit = null
        if (res.resultCode == RESULT_OK && d != null) vm.startExit(d) else vm.toast("VPN permission is required to use an exit node")
    }

    private val notificationPermission = registerForActivityResult(ActivityResultContracts.RequestPermission()) { }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        setContent { AgentMeshApp(vm, onUseExitNode = ::requestExit) }
    }

    private fun requestExit(d: Device) {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            notificationPermission.launch(Manifest.permission.POST_NOTIFICATIONS)
        }
        val consent = VpnService.prepare(this)
        if (consent == null) vm.startExit(d) else {
            pendingExit = d
            vpnConsent.launch(consent)
        }
    }
}
