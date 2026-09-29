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
import io.agentmesh.control.App
import io.agentmesh.control.MainActivity
import io.agentmesh.control.data.ApiException
import io.agentmesh.tunnel.Engine
import io.agentmesh.tunnel.Tunnel
import java.net.InetAddress
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
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
    data class Reconnecting(val deviceName: String, val attempt: Int, val reason: String) : VpnStatus
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
 * All IPv4 traffic is captured (private LAN ranges stay on the local network
 * on Android 13+; IPv6 is blocked), terminated by the Go engine and carried over the
 * relay to the agent. This app itself is excluded from the VPN so it can keep
 * talking to the control plane directly.
 *
 * If the tunnel drops (network change, phone stalled the app, server
 * restart) the service requests a fresh session and reconnects on its own.
 */
class ExitVpnService : VpnService() {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)
    @Volatile private var engine: Engine? = null
    @Volatile private var userStopped = false
    private var monitor: Job? = null
    private var profileId = ""
    private var deviceName = ""

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        when (intent?.action) {
            ACTION_CONNECT -> {
                userStopped = false
                profileId = intent.getStringExtra(EXTRA_PROFILE_ID).orEmpty()
                val profile = (application as App).profiles.get(profileId)
                deviceName = profile?.let { it.deviceName.ifBlank { it.name } } ?: "server"
                // Must enter the foreground promptly after startForegroundService().
                goForeground("Connecting via $deviceName…")
                if (profile == null) {
                    shutdown("Saved server not found")
                    return START_NOT_STICKY
                }
                _status.value = VpnStatus.Connecting(deviceName)
                scope.launch { openAndConnect(attemptReason = null) }
            }
            ACTION_DISCONNECT -> {
                userStopped = true
                shutdown(null)
            }
        }
        return START_NOT_STICKY
    }

    /** Establishes the VPN interface and starts the engine. Returns an error or null. */
    private fun connect(relay: String, ticket: String, session: String): String? {
        engine?.stop()
        val builder = Builder()
            .setSession("AgentMesh via $deviceName")
            .setMtu(MTU)
            .addAddress("10.111.0.2", 24)
            .addRoute("0.0.0.0", 0)
            // No IPv6 address or route on purpose: Android then BLOCKS IPv6 for
            // the duration of the VPN (it does not leak around it), so apps fail
            // over to IPv4 instantly. Exit devices are often IPv4-only; tunnelling
            // IPv6 to them made dual-stack sites hang on mobile networks.
            .addDnsServer("1.1.1.1")
            .addDnsServer("1.0.0.1")
            .addDisallowedApplication(packageName)
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            // Keep the local network (printers, a LAN AgentMesh server, ...) direct.
            for ((net, len) in LOCAL_RANGES) builder.excludeRoute(IpPrefix(InetAddress.getByName(net), len))
        }
        val pfd: ParcelFileDescriptor = try {
            builder.establish() ?: return "VPN permission was not granted"
        } catch (e: Exception) {
            return "Could not create VPN interface: ${e.message}"
        }
        val fd = pfd.detachFd() // ownership moves to the Go engine
        val eng = Tunnel.newEngine()
        try {
            eng.start(fd.toLong(), relay, ticket, "", MTU.toLong())
        } catch (e: Exception) {
            runCatching { ParcelFileDescriptor.adoptFd(fd).close() }
            return "Tunnel failed: ${e.message}"
        }
        engine = eng
        currentEngine = eng
        val since = System.currentTimeMillis()
        monitor?.cancel()
        monitor = scope.launch {
            while (isActive) {
                if (engine !== eng) break
                if (!eng.isRunning) {
                    if (!userStopped) reconnect(eng.lastError().ifBlank { "Tunnel closed" })
                    break
                }
                _status.value = VpnStatus.Connected(deviceName, session, since, eng.bytesUp(), eng.bytesDown(), eng.flows(), eng.failedFlows())
                notify("Via $deviceName · ↑ ${human(eng.bytesUp())} ↓ ${human(eng.bytesDown())}")
                delay(2000)
            }
        }
        return null
    }

    /** Opens a fresh session with the saved server's connect key and starts the tunnel. */
    private suspend fun openAndConnect(attemptReason: String?): Boolean {
        val app = application as App
        val profile = app.profiles.get(profileId) ?: return false.also { shutdown("Saved server was deleted") }
        val grant = try {
            app.connect.open(profile, "Android ${Build.MANUFACTURER} ${Build.MODEL}")
        } catch (e: ApiException) {
            // The server answered "no" (link revoked/expired, device offline): retrying will not help.
            userStopped = true
            shutdown(e.message)
            return false
        } catch (e: Exception) {
            if (attemptReason == null) reconnect("Server unreachable: ${e.message}")
            return false
        }
        deviceName = grant.deviceName.ifBlank { deviceName }
        app.profiles.get(profileId)?.let {
            app.profiles.upsert(it.copy(deviceName = deviceName, lastConnectedAt = System.currentTimeMillis()))
        }
        val err = connect(grant.relayUrl, grant.ticket, grant.sessionId)
        if (err != null) {
            if (attemptReason == null) reconnect(err)
            return false
        }
        return true
    }

    /** Retries with backoff when the tunnel drops or the server is briefly unreachable. */
    private suspend fun reconnect(reason: String) {
        for ((i, wait) in RETRY_DELAYS_S.withIndex()) {
            if (userStopped) return
            _status.value = VpnStatus.Reconnecting(deviceName, i + 1, reason)
            notify("Reconnecting to $deviceName (attempt ${i + 1})…")
            delay(wait * 1000L)
            if (userStopped) return
            if (openAndConnect(attemptReason = reason)) return
        }
        if (!userStopped) shutdown("Lost connection to $deviceName: $reason")
    }
    private fun shutdown(error: String?) {
        monitor?.cancel()
        engine?.stop()
        engine = null
        currentEngine = null
        _status.value = if (error == null) VpnStatus.Idle else VpnStatus.Failed(error)
        stopForeground(STOP_FOREGROUND_REMOVE)
        stopSelf()
    }

    override fun onRevoke() {
        userStopped = true
        shutdown("VPN was turned off by the system or another VPN app")
    }

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
        const val EXTRA_PROFILE_ID = "profile_id"
        private const val CHANNEL = "exit_tunnel"
        private const val NOTIFICATION_ID = 7
        private const val MTU = 1500
        private val RETRY_DELAYS_S = listOf(1L, 3L, 5L, 10L, 20L, 30L)
        private val LOCAL_RANGES = listOf(
            "10.0.0.0" to 8, "172.16.0.0" to 12, "192.168.0.0" to 16, "169.254.0.0" to 16, "100.64.0.0" to 10,
        )

        private val _status = MutableStateFlow<VpnStatus>(VpnStatus.Idle)
        val status: StateFlow<VpnStatus> = _status

        /** The running engine, for the in-app "what is my IP" check. */
        @Volatile var currentEngine: Engine? = null
            private set

        fun connect(ctx: Context, profileId: String) {
            ctx.startForegroundService(Intent(ctx, ExitVpnService::class.java).setAction(ACTION_CONNECT).putExtra(EXTRA_PROFILE_ID, profileId))
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
