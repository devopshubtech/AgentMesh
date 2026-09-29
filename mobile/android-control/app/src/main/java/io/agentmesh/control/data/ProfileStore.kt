package io.agentmesh.control.data

import android.content.Context
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import android.util.Base64
import java.security.KeyStore
import java.security.MessageDigest
import java.util.UUID
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec
import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.json.Json

/**
 * Saved remote servers. Connect keys are encrypted with a non-exportable
 * AES-GCM key held in the Android Keystore.
 */
class ProfileStore(context: Context) {
    private val prefs = context.getSharedPreferences("agentmesh_profiles", Context.MODE_PRIVATE)
    private val json = Json { ignoreUnknownKeys = true }
    private val ser = ListSerializer(ConnectProfile.serializer())

    fun all(): List<ConnectProfile> =
        runCatching { json.decodeFromString(ser, prefs.getString("profiles", "[]") ?: "[]") }.getOrDefault(emptyList())
            .sortedByDescending { it.lastConnectedAt }

    fun get(id: String): ConnectProfile? = all().firstOrNull { it.id == id }

    @Synchronized
    fun upsert(p: ConnectProfile) {
        val list = all().filter { it.id != p.id } + p
        prefs.edit().putString("profiles", json.encodeToString(ser, list)).apply()
    }

    @Synchronized
    fun delete(id: String) {
        prefs.edit().putString("profiles", json.encodeToString(ser, all().filter { it.id != id })).apply()
    }

    fun key(p: ConnectProfile): String = decrypt(p.keyEnc)

    /** Adds a link, or refreshes the saved server that already uses the same key. */
    fun import(link: ConnectLink, name: String? = null): ConnectProfile {
        val hash = sha256(link.key)
        val existing = all().firstOrNull { it.keyHash == hash }
        val p = (existing ?: ConnectProfile(
            id = UUID.randomUUID().toString(), name = name ?: hostOf(link.serverUrl),
            serverUrl = link.serverUrl, keyEnc = encrypt(link.key), keyHash = hash,
        )).copy(serverUrl = link.serverUrl, rendezvousUrl = link.rendezvousUrl.ifBlank { existing?.rendezvousUrl.orEmpty() })
        upsert(p)
        return p
    }

    /** Replaces the key of an existing server with the one from a new link. */
    fun replaceLink(p: ConnectProfile, link: ConnectLink): ConnectProfile =
        p.copy(serverUrl = link.serverUrl, keyEnc = encrypt(link.key), keyHash = sha256(link.key),
            rendezvousUrl = link.rendezvousUrl.ifBlank { p.rendezvousUrl }).also { upsert(it) }

    private fun hostOf(url: String) = url.removePrefix("https://").substringBefore('/').substringBefore('.')

    private fun sha256(s: String) = MessageDigest.getInstance("SHA-256").digest(s.toByteArray()).joinToString("") { "%02x".format(it) }

    private fun secretKey(): SecretKey {
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
        c.init(Cipher.ENCRYPT_MODE, secretKey())
        return Base64.encodeToString(c.iv + c.doFinal(plain.toByteArray()), Base64.NO_WRAP)
    }

    private fun decrypt(enc: String): String {
        val raw = Base64.decode(enc, Base64.NO_WRAP)
        val c = Cipher.getInstance("AES/GCM/NoPadding")
        c.init(Cipher.DECRYPT_MODE, secretKey(), GCMParameterSpec(128, raw, 0, 12))
        return String(c.doFinal(raw, 12, raw.size - 12))
    }

    private companion object {
        const val ALIAS = "agentmesh_connect_keys"
    }
}
