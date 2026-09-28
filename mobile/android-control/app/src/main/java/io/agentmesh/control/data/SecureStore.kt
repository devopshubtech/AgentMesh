package io.agentmesh.control.data

import android.content.Context
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import android.util.Base64
import java.security.KeyStore
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec

/**
 * App settings and credentials.
 *
 * Several servers can be saved (e.g. "home Wi-Fi" and "remote/public"); each
 * keeps its own CA certificate and refresh token, so switching back and forth
 * does not require signing in again. Refresh tokens are encrypted with a
 * non-exportable AES-GCM key held in the Android Keystore.
 */
class SecureStore(context: Context) {
    private val prefs = context.getSharedPreferences("agentmesh", Context.MODE_PRIVATE)

    init {
        migrateLegacy()
    }

    /** The server currently in use. Setting it also adds it to [servers]. */
    var serverUrl: String
        get() = prefs.getString("server_url", "") ?: ""
        set(v) {
            val u = v.trim().trimEnd('/')
            prefs.edit().putString("server_url", u).apply()
            if (u.isNotBlank() && u !in servers) prefs.edit().putString("servers", (servers + u).joinToString("\n")).apply()
        }

    /** All saved servers, in the order they were added. */
    val servers: List<String>
        get() = (prefs.getString("servers", "") ?: "").split("\n").filter { it.isNotBlank() }

    fun removeServer(url: String) {
        prefs.edit()
            .putString("servers", servers.filter { it != url }.joinToString("\n"))
            .remove("ca_pem::$url")
            .remove("refresh_token_enc::$url")
            .apply()
        if (serverUrl == url) prefs.edit().putString("server_url", servers.firstOrNull() ?: "").apply()
    }

    /** Private CA for the current server ("" = system trust only). */
    var caPem: String
        get() = prefs.getString("ca_pem::$serverUrl", "") ?: ""
        set(v) = prefs.edit().putString("ca_pem::$serverUrl", v).apply()

    var email: String
        get() = prefs.getString("email", "") ?: ""
        set(v) = prefs.edit().putString("email", v).apply()

    /** Route exit-node traffic via "<server URL>/v1/relay" instead of the address the API advertises. */
    var relayViaServer: Boolean
        get() = prefs.getBoolean("relay_via_server", true)
        set(v) = prefs.edit().putBoolean("relay_via_server", v).apply()

    var refreshToken: String?
        get() = prefs.getString("refresh_token_enc::$serverUrl", null)?.let { runCatching { decrypt(it) }.getOrNull() }
        set(v) {
            val k = "refresh_token_enc::$serverUrl"
            if (v == null) prefs.edit().remove(k).apply() else prefs.edit().putString(k, encrypt(v)).apply()
        }

    /** v0.2.0 stored one CA / refresh token globally; attach them to the saved server. */
    private fun migrateLegacy() {
        val url = serverUrl
        if (url.isBlank()) return
        val e = prefs.edit()
        prefs.getString("ca_pem", null)?.let { e.putString("ca_pem::$url", it).remove("ca_pem") }
        prefs.getString("refresh_token_enc", null)?.let { e.putString("refresh_token_enc::$url", it).remove("refresh_token_enc") }
        if (url !in servers) e.putString("servers", (servers + url).joinToString("\n"))
        e.apply()
    }

    private fun key(): SecretKey {
        val ks = KeyStore.getInstance("AndroidKeyStore").apply { load(null) }
        (ks.getKey(ALIAS, null) as? SecretKey)?.let { return it }
        val gen = KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, "AndroidKeyStore")
        gen.init(
            KeyGenParameterSpec.Builder(ALIAS, KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT)
                .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
                .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
                .setKeySize(256)
                .build()
        )
        return gen.generateKey()
    }

    private fun encrypt(plain: String): String {
        val c = Cipher.getInstance("AES/GCM/NoPadding")
        c.init(Cipher.ENCRYPT_MODE, key())
        val out = c.iv + c.doFinal(plain.toByteArray())
        return Base64.encodeToString(out, Base64.NO_WRAP)
    }

    private fun decrypt(enc: String): String {
        val raw = Base64.decode(enc, Base64.NO_WRAP)
        val c = Cipher.getInstance("AES/GCM/NoPadding")
        c.init(Cipher.DECRYPT_MODE, key(), GCMParameterSpec(128, raw, 0, 12))
        return String(c.doFinal(raw, 12, raw.size - 12))
    }

    private companion object {
        const val ALIAS = "agentmesh_refresh_token"
    }
}
