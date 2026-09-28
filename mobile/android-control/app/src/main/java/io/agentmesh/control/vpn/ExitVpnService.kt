package io.agentmesh.control.vpn

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.net.IpPrefix
import android.net.VpnService
import android.os.Build
import android.os.ParcelFileDescriptor
import io.agentmesh.control.MainActivity
import io.agentmesh.tunnel.Engine
import io.agentmesh.tunnel.Tunnel
import java.net.InetAddress
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch

/** Live tunnel state shown by the UI. */
sealed interface VpnStatus {
    data object Idle : VpnStatus
    data class Connecting(val deviceName: String) : VpnStatus
    data class Connected(
        val deviceName: String,
        val sessionId: String,
        val sinceMs: Long,
        val bytesUp: Long,
        val bytesDown: Long,
        val flows: Long,
        val failedFlows: Long,
    ) : VpnStatus
    data class Failed(val message: String) : VpnStatus
}

/**
 * Routes this phone's traffic through an AgentMesh exit-node device.
 *
 * All IPv4/IPv6 traffic is captured (private LAN ranges are left on the local
 * network on Android 13+), terminated by the Go engine and carried over the
 * relay to the agent. This app itself is excluded from the VPN so it can keep
 * talking to the control plane directly.
 */
class ExitVpnService : VpnService() {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)
    @Volatile private var engine: Engine? = null

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        when (intent?.action) {
            ACTION_CONNECT -> {
                val relay = intent.getStringExtra(EXTRA_RELAY_URL).orEmpty()
                val ticket = intent.getStringExtra(EXTRA_TICKET).orEmpty()
                val ca = intent.getStringExtra(EXTRA_CA_PEM).orEmpty()
                val session = intent.getStringExtra(EXTRA_SESSION_ID).orEmpty()
                val device = intent.getStringExtra(EXTRA_DEVICE_NAME).orEmpty()
                goForeground("Connecting via $device…")
                _status.value = VpnStatus.Connecting(device)
                scope.launch { connect(relay, ticket, ca, session, device) }
            }
            ACTION_DISCONNECT -> shutdown(null)
        }
        return START_NOT_STICKY
    }

    private fun connect(relay: String, ticket: String, ca: String, session: String, device: String) {
        engine?.stop()
        val builder = Builder()
            .setSession("AgentMesh via $device")
            .setMtu(MTU)
            .addAddress("10.111.0.2", 24)
            .addRoute("0.0.0.0", 0)
            .addAddress("fd00:a6e5:6d65::2", 64)
            .addRoute("::", 0)
            .addDnsServer("1.1.1.1")
            .addDnsServer("1.0.0.1")
            .addDisallowedApplication(packageName)
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            // Keep the local network (printers, the AgentMesh server on the LAN, ...) direct.
            for ((net, len) in LOCAL_RANGES) builder.excludeRoute(IpPrefix(InetAddress.getByName(net), len))
        }
        val pfd: ParcelFileDescriptor = try {
            builder.establish() ?: return shutdown("VPN permission was not granted")
        } catch (e: Exception) {
            return shutdown("Could not create VPN interface: ${e.message}")
        }
        val fd = pfd.detachFd() // ownership moves to the Go engine
        val eng = Tunnel.newEngine()
        try {
            eng.start(fd.toLong(), relay, ticket, ca, MTU.toLong())
        } catch (e: Exception) {
            runCatching { ParcelFileDescriptor.adoptFd(fd).close() }
            return shutdown("Tunnel failed: ${e.message}")
        }
        engine = eng
        currentEngine = eng
        val since = System.currentTimeMillis()
        scope.launch {
            while (isActive) {
                val e = engine ?: break
                if (!e.isRunning) {
                    shutdown(e.lastError().ifBlank { "Tunnel closed" })
                    break
                }
                _status.value = VpnStatus.Connected(device, session, since, e.bytesUp(), e.bytesDown(), e.flows(), e.failedFlows())
                notify("Via $device · ↑ ${human(e.bytesUp())} ↓ ${human(e.bytesDown())}")
                delay(2000)
            }
        }
    }

    private fun shutdown(error: String?) {
        engine?.stop()
        engine = null
        currentEngine = null
        _status.value = if (error == null) VpnStatus.Idle else VpnStatus.Failed(error)
        stopForeground(STOP_FOREGROUND_REMOVE)
        stopSelf()
    }

    override fun onRevoke() = shutdown("VPN was turned off by the system or another VPN app")

    override fun onDestroy() {
        engine?.stop()
        currentEngine = null
        scope.cancel()
        super.onDestroy()
    }

    // ------------------------------------------------------------ notification

    private fun notification(text: String): Notification {
        val nm = getSystemService(NotificationManager::class.java)
        if (nm.getNotificationChannel(CHANNEL) == null) {
            nm.createNotificationChannel(NotificationChannel(CHANNEL, "Exit node tunnel", NotificationManager.IMPORTANCE_LOW))
        }
        val open = PendingIntent.getActivity(this, 0, Intent(this, MainActivity::class.java), PendingIntent.FLAG_IMMUTABLE)
        val stop = PendingIntent.getService(this, 1, Intent(this, ExitVpnService::class.java).setAction(ACTION_DISCONNECT),
            PendingIntent.FLAG_IMMUTABLE)
        return Notification.Builder(this, CHANNEL)
            .setSmallIcon(android.R.drawable.ic_lock_lock)
            .setContentTitle("AgentMesh exit node")
            .setContentText(text)
            .setContentIntent(open)
            .setOngoing(true)
            .addAction(Notification.Action.Builder(null, "Disconnect", stop).build())
            .build()
    }

    private fun goForeground(text: String) {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.UPSIDE_DOWN_CAKE) {
            startForeground(NOTIFICATION_ID, notification(text), ServiceInfo.FOREGROUND_SERVICE_TYPE_SPECIAL_USE)
        } else {
            startForeground(NOTIFICATION_ID, notification(text))
        }
    }

    private fun notify(text: String) =
        getSystemService(NotificationManager::class.java).notify(NOTIFICATION_ID, notification(text))

    companion object {
        const val ACTION_CONNECT = "io.agentmesh.control.CONNECT"
        const val ACTION_DISCONNECT = "io.agentmesh.control.DISCONNECT"
        const val EXTRA_RELAY_URL = "relay_url"
        const val EXTRA_TICKET = "ticket"
        const val EXTRA_CA_PEM = "ca_pem"
        const val EXTRA_SESSION_ID = "session_id"
        const val EXTRA_DEVICE_NAME = "device_name"
        private const val CHANNEL = "exit_tunnel"
        private const val NOTIFICATION_ID = 7
        private const val MTU = 1500
        private val LOCAL_RANGES = listOf(
            "10.0.0.0" to 8, "172.16.0.0" to 12, "192.168.0.0" to 16, "169.254.0.0" to 16, "100.64.0.0" to 10,
        )

        private val _status = MutableStateFlow<VpnStatus>(VpnStatus.Idle)
        val status: StateFlow<VpnStatus> = _status

        /** The running engine, for the in-app "what is my IP" check. */
        @Volatile var currentEngine: Engine? = null
            private set

        fun connect(ctx: Context, relayUrl: String, ticket: String, caPem: String, sessionId: String, deviceName: String) {
            val i = Intent(ctx, ExitVpnService::class.java).setAction(ACTION_CONNECT)
                .putExtra(EXTRA_RELAY_URL, relayUrl).putExtra(EXTRA_TICKET, ticket).putExtra(EXTRA_CA_PEM, caPem)
                .putExtra(EXTRA_SESSION_ID, sessionId).putExtra(EXTRA_DEVICE_NAME, deviceName)
            ctx.startForegroundService(i)
        }

        fun disconnect(ctx: Context) {
            ctx.startService(Intent(ctx, ExitVpnService::class.java).setAction(ACTION_DISCONNECT))
        }

        fun human(b: Long): String = when {
            b >= 1L shl 30 -> "%.1f GB".format(b / (1L shl 30).toDouble())
            b >= 1L shl 20 -> "%.1f MB".format(b / (1L shl 20).toDouble())
            b >= 1L shl 10 -> "%.1f KB".format(b / 1024.0)
            else -> "$b B"
        }
    }
}
