package io.agentmesh.control.data

import android.content.ContentResolver
import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.net.Uri
import com.google.zxing.BarcodeFormat
import com.google.zxing.BinaryBitmap
import com.google.zxing.DecodeHintType
import com.google.zxing.NotFoundException
import com.google.zxing.RGBLuminanceSource
import com.google.zxing.common.GlobalHistogramBinarizer
import com.google.zxing.common.HybridBinarizer
import com.google.zxing.qrcode.QRCodeReader

/**
 * Reads a QR code from a picture chosen in the gallery (e.g. a screenshot of
 * the dashboard's "Connect a phone" page). Returns null if none is found.
 */
object QrImage {
    private const val MAX_SIDE = 1600 // large camera photos are scaled down first

    fun read(resolver: ContentResolver, uri: Uri): String? {
        val bounds = BitmapFactory.Options().apply { inJustDecodeBounds = true }
        resolver.openInputStream(uri)?.use { BitmapFactory.decodeStream(it, null, bounds) }
        if (bounds.outWidth <= 0 || bounds.outHeight <= 0) return null
        var sample = 1
        while (maxOf(bounds.outWidth, bounds.outHeight) / sample > MAX_SIDE) sample *= 2
        val bmp = resolver.openInputStream(uri)?.use {
            BitmapFactory.decodeStream(it, null, BitmapFactory.Options().apply { inSampleSize = sample })
        } ?: return null
        return try { decode(bmp) } finally { bmp.recycle() }
    }

    private fun decode(bmp: Bitmap): String? {
        val px = IntArray(bmp.width * bmp.height)
        bmp.getPixels(px, 0, bmp.width, 0, 0, bmp.width, bmp.height)
        val src = RGBLuminanceSource(bmp.width, bmp.height, px)
        val hints = mapOf(DecodeHintType.TRY_HARDER to true, DecodeHintType.POSSIBLE_FORMATS to listOf(BarcodeFormat.QR_CODE))
        // Hybrid works for most screenshots; the global binarizer catches low-contrast photos.
        for (binary in listOf(BinaryBitmap(HybridBinarizer(src)), BinaryBitmap(GlobalHistogramBinarizer(src)))) {
            try {
                return QRCodeReader().decode(binary, hints).text
            } catch (_: NotFoundException) {
            } catch (_: com.google.zxing.ChecksumException) {
            } catch (_: com.google.zxing.FormatException) {
            }
        }
        return null
    }
}
