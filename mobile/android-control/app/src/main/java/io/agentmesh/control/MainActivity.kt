package io.agentmesh.control

import android.Manifest
import android.content.Intent
import android.net.Uri
import android.net.VpnService
import android.os.Build
import android.os.Bundle
import android.os.PowerManager
import android.provider.Settings
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
        askBatteryExemptionOnce()
        startExitWithConsent(d)
    }

    /**
     * Battery savers (notably OnePlus/Oppo/Xiaomi) freeze backgrounded apps even
     * while their VPN is active, which drops the tunnel after ~1 minute. Ask
     * once to exempt this app.
     */
    private fun askBatteryExemptionOnce() {
        val pm = getSystemService(PowerManager::class.java)
        val prefs = getSharedPreferences("agentmesh", MODE_PRIVATE)
        if (pm.isIgnoringBatteryOptimizations(packageName) || prefs.getBoolean("asked_battery", false)) return
        prefs.edit().putBoolean("asked_battery", true).apply()
        runCatching {
            startActivity(Intent(Settings.ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS, Uri.parse("package:$packageName")))
        }
    }

    private fun startExitWithConsent(d: Device) {
        val consent = VpnService.prepare(this)
        if (consent == null) vm.startExit(d) else {
            pendingExit = d
            vpnConsent.launch(consent)
        }
    }
}
