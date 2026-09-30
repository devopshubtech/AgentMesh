import java.util.Properties

plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
    id("org.jetbrains.kotlin.plugin.compose")
    id("org.jetbrains.kotlin.plugin.serialization")
}

// Release signing key lives outside git (created by build.sh on first build).
val keystoreProps = Properties().apply {
    val f = rootProject.file("keystore/keystore.properties")
    if (f.exists()) f.inputStream().use { load(it) }
}

android {
    namespace = "io.agentmesh.control"
    compileSdk = 35

    defaultConfig {
        applicationId = "io.agentmesh.control"
        minSdk = 26
        targetSdk = 35
        versionCode = (System.getenv("AGENTMESH_VERSION_CODE") ?: "1").toInt()
        versionName = System.getenv("AGENTMESH_VERSION_NAME") ?: "0.2.0"
        // Where the app looks up the server's current address when the user
        // types a pairing code (no QR link to carry it). start-agentmesh.ps1
        // keeps this gist pointing at the current public tunnel URL.
        val rendezvous = System.getenv("AGENTMESH_DEFAULT_RENDEZVOUS")
            ?: (project.findProperty("agentmesh.defaultRendezvous") as String?) ?: ""
        buildConfigField("String", "DEFAULT_RENDEZVOUS", "\"$rendezvous\"")
    }

    signingConfigs {
        if (keystoreProps.getProperty("storeFile") != null) {
            create("release") {
                storeFile = rootProject.file(keystoreProps.getProperty("storeFile"))
                storePassword = keystoreProps.getProperty("storePassword")
                keyAlias = keystoreProps.getProperty("keyAlias")
                keyPassword = keystoreProps.getProperty("keyPassword")
            }
        }
    }

    buildTypes {
        release {
            // gomobile's JNI bindings are looked up reflectively; keep code as-is.
            isMinifyEnabled = false
            signingConfig = signingConfigs.findByName("release")
        }
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
    kotlinOptions { jvmTarget = "17" }
    buildFeatures {
        compose = true
        buildConfig = true
    }
    packaging { resources.excludes += "/META-INF/{AL2.0,LGPL2.1}" }
}

dependencies {
    // Exit-node engine (Go, built with gomobile by build.sh).
    implementation(files("libs/agentmesh-tunnel.aar"))

    implementation("androidx.core:core-ktx:1.15.0")
    implementation("androidx.activity:activity-compose:1.9.3")
    implementation("androidx.lifecycle:lifecycle-viewmodel-compose:2.8.7")
    implementation("androidx.lifecycle:lifecycle-runtime-compose:2.8.7")
    implementation(platform("androidx.compose:compose-bom:2024.12.01"))
    implementation("androidx.compose.ui:ui")
    implementation("androidx.compose.material3:material3")
    implementation("androidx.compose.material:material-icons-core")

    implementation("com.squareup.okhttp3:okhttp:4.12.0")
    // QR code scanning (camera) for connect links.
    implementation("com.journeyapps:zxing-android-embedded:4.3.0")
    implementation("com.google.zxing:core:3.5.3")
    implementation("org.jetbrains.kotlinx:kotlinx-serialization-json:1.7.3")
    implementation("org.jetbrains.kotlinx:kotlinx-coroutines-android:1.9.0")
}
