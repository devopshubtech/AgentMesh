package io.agentmesh.control.vpn

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.net.ConnectivityManager
import android.net.IpPrefix
import android.net.Network
import android.net.VpnService
import android.os.Build
import android.os.ParcelFileDescriptor
import io.agentmesh.control.App
import io.agentmesh.control.MainActivity
import io.agentmesh.control.data.ApiException
import io.agentmesh.control.data.SessionGrant
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
 * If the relay drops (network change, phone stalled the app, server restart)
 * the service requests a fresh session and swaps it in without taking the
 * VPN interface down.
 */
class ExitVpnService : VpnService() {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)
    @Volatile private var engine: Engine? = null
    @Volatile private var userStopped = false
    private var monitor: Job? = null
    private var profileId = ""
    private var deviceName = ""
    @Volatile private var sessionId = ""
    @Volatile private var networkChanged = false
    @Volatile private var underlying: Network? = null
    private var netCallback: ConnectivityManager.NetworkCallback? = null

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
    private fun connect(grant: SessionGrant): String? {
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
            eng.start(fd.toLong(), grant.relayUrl, grant.ticket, "", MTU.toLong())
        } catch (e: Exception) {
            runCatching { ParcelFileDescriptor.adoptFd(fd).close() }
            return "Tunnel failed: ${e.message}"
        }
        engine = eng
        currentEngine = eng
        sessionId = grant.sessionId
        startMonitor(eng)
        return null
    }

    /**
     * Watches the engine. When only the relay link drops (server hiccup,
     * phone switched between Wi-Fi and mobile data, congested exit device)
     * the VPN interface stays up and a new relay link is swapped in, so apps
     * barely notice. Only if the engine itself dies is the VPN rebuilt.
     */
    private fun startMonitor(eng: Engine) {
        val since = System.currentTimeMillis()
        monitor?.cancel()
        monitor = scope.launch {
            while (isActive) {
                if (engine !== eng) break
                if (!eng.isRunning) {
                    if (!userStopped) reconnect(eng.lastError().ifBlank { "Tunnel closed" })
                    break
                }
                if (!eng.relayUp() || networkChanged) {
                    val why = if (eng.relayUp()) "Network changed" else eng.lastError().ifBlank { "relay connection closed" }
                    networkChanged = false
                    if (!recoverRelay(eng, why)) break
                    continue
                }
                _status.value = VpnStatus.Connected(deviceName, sessionId, since, eng.bytesUp(), eng.bytesDown(), eng.flows(), eng.failedFlows())
                notify("Via $deviceName · ↑ ${human(eng.bytesUp())} ↓ ${human(eng.bytesDown())}")
                delay(1000)
            }
        }
    }

    /** Swaps a fresh relay link into the running engine. False if we gave up. */
    private suspend fun recoverRelay(eng: Engine, reason: String): Boolean {
        val deadline = System.currentTimeMillis() + GIVE_UP_AFTER_MS
        var attempt = 0
        var why = reason
        while (!userStopped && engine === eng && eng.isRunning) {
            attempt++
            _status.value = VpnStatus.Reconnecting(deviceName, attempt, why)
            notify("Reconnecting to $deviceName (attempt $attempt)…")
            val grant = try {
                openSession() ?: return false
            } catch (e: Exception) {
                why = "Server unreachable: ${e.message}"
                null
            }
            if (grant != null) {
                try {
                    eng.reconnect(grant.relayUrl, grant.ticket, "")
                    sessionId = grant.sessionId
                    return true
                } catch (e: Exception) {
                    why = e.message ?: "relay connect failed"
                }
            }
            if (System.currentTimeMillis() > deadline) {
                shutdown("Lost connection to $deviceName: $why")
                return false
            }
            delay(RELAY_RETRY_S[minOf(attempt - 1, RELAY_RETRY_S.lastIndex)] * 1000L)
        }
        return false
    }

    /**
     * Asks the server for a new session with the saved connect key. Returns
     * null (after shutting down) when the server says no for good: link
     * revoked/expired or profile deleted. Throws on transient failures.
     */
    private suspend fun openSession(): SessionGrant? {
        val app = application as App
        val profile = app.profiles.get(profileId) ?: return null.also { shutdown("Saved server was deleted") }
        val grant = try {
            app.connect.open(profile, "Android ${Build.MANUFACTURER} ${Build.MODEL}")
        } catch (e: ApiException) {
            // The server answered "no" (link revoked/expired, device offline): retrying will not help.
            userStopped = true
            shutdown(e.message)
            return null
        }
        deviceName = grant.deviceName.ifBlank { deviceName }
        app.profiles.get(profileId)?.let {
            app.profiles.upsert(it.copy(deviceName = deviceName, lastConnectedAt = System.currentTimeMillis()))
        }
        return grant
    }

    /** Opens a fresh session and (re)builds the VPN. Returns true when connected. */
    private suspend fun openAndConnect(attemptReason: String?): Boolean {
        val grant = try {
            openSession() ?: return false
        } catch (e: Exception) {
            if (attemptReason == null) reconnect("Server unreachable: ${e.message}")
            return false
        }
        val err = connect(grant)
        if (err != null) {
            if (attemptReason == null) reconnect(err)
            return false
        }
        watchNetwork()
        return true
    }

    /** Retries with backoff when the tunnel could not be (re)built. */
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

    /**
     * Follows the phone's real (non-VPN) default network. On a switch between
     * Wi-Fi and mobile data the old relay socket is dead but would take the
     * keepalive timeout to notice, so reconnect straight away instead.
     */
    private fun watchNetwork() {
        if (netCallback != null) return
        val cm = getSystemService(ConnectivityManager::class.java)
        val cb = object : ConnectivityManager.NetworkCallback() {
            override fun onAvailable(network: Network) {
                runCatching { setUnderlyingNetworks(arrayOf(network)) }
                val prev = underlying
                underlying = network
                if (prev != null && prev != network) networkChanged = true
            }
        }
        runCatching { cm.registerDefaultNetworkCallback(cb) }.onSuccess { netCallback = cb }
    }

    private fun unwatchNetwork() {
        val cb = netCallback ?: return
        netCallback = null
        underlying = null
        runCatching { getSystemService(ConnectivityManager::class.java).unregisterNetworkCallback(cb) }
    }

    private fun shutdown(error: String?) {
        monitor?.cancel()
        unwatchNetwork()
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
        unwatchNetwork()
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
        // Relay swaps: first retry at once, then back off; keep the VPN up
        // (traffic held, nothing leaks) for up to 10 minutes before giving up.
        private val RELAY_RETRY_S = listOf(0L, 1L, 2L, 3L, 5L, 10L, 15L, 30L)
        private const val GIVE_UP_AFTER_MS = 10 * 60 * 1000L
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
