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
import androidx.activity.result.PickVisualMediaRequest
import androidx.activity.result.contract.ActivityResultContracts
import androidx.activity.viewModels
import com.journeyapps.barcodescanner.ScanContract
import com.journeyapps.barcodescanner.ScanOptions
import io.agentmesh.control.data.ConnectProfile
import io.agentmesh.control.ui.AgentMeshApp
import io.agentmesh.control.ui.AppViewModel

class MainActivity : ComponentActivity() {
    private val vm: AppViewModel by viewModels()
    private var pending: ConnectProfile? = null

    private val vpnConsent = registerForActivityResult(ActivityResultContracts.StartActivityForResult()) { res ->
        val p = pending
        pending = null
        if (res.resultCode == RESULT_OK && p != null) vm.connect(p) else vm.toast("Allow the VPN request to connect")
    }

    private val notificationPermission = registerForActivityResult(ActivityResultContracts.RequestPermission()) { }

    private val scanner = registerForActivityResult(ScanContract()) { result ->
        result.contents?.let { text -> vm.importLink(text)?.let(::requestConnect) }
    }

    // System photo picker: no storage permission needed.
    private val galleryPicker = registerForActivityResult(ActivityResultContracts.PickVisualMedia()) { uri ->
        uri?.let { vm.importQrImage(it, ::requestConnect) }
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        setContent { AgentMeshApp(vm, onScan = ::scan, onPickImage = ::pickImage, onConnect = ::requestConnect) }
        if (savedInstanceState == null) handleDeepLink(intent)
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        handleDeepLink(intent)
    }

    /** agentmesh://join?... from the connect web page (or an https link shared to the app). */
    private fun handleDeepLink(intent: Intent?) {
        val data = intent?.dataString ?: intent?.getStringExtra(Intent.EXTRA_TEXT) ?: return
        vm.importLink(data)?.let(::requestConnect)
    }

    private fun scan() {
        scanner.launch(
            ScanOptions()
                .setDesiredBarcodeFormats(ScanOptions.QR_CODE)
                .setPrompt("Scan the QR code from AgentMesh → Connect a phone")
                .setBeepEnabled(false)
                .setOrientationLocked(false)
        )
    }

    private fun pickImage() {
        galleryPicker.launch(PickVisualMediaRequest(ActivityResultContracts.PickVisualMedia.ImageOnly))
    }

    private fun requestConnect(p: ConnectProfile) {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            notificationPermission.launch(Manifest.permission.POST_NOTIFICATIONS)
        }
        askBatteryExemptionOnce()
        val consent = VpnService.prepare(this)
        if (consent == null) vm.connect(p) else {
            pending = p
            vpnConsent.launch(consent)
        }
    }

    /**
     * Battery savers (notably OnePlus/Oppo/Xiaomi) freeze backgrounded apps even
     * while their VPN is active. Ask once to exempt this app.
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
}
