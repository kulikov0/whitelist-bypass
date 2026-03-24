plugins {
    alias(libs.plugins.android.application)
    alias(libs.plugins.kotlin.android)
}

val versionMajor = 0
val versionMinor = 1
val versionPatch = 7
val versionBuild = System.getenv("BUILD_NUMBER")?.toIntOrNull() ?: 0

android {
    namespace = "bypass.whitelist"
    compileSdk {
        version = release(36)
    }

    buildFeatures {
        buildConfig = true
    }

    defaultConfig {
        applicationId = "bypass.whitelist"
        minSdk = 23
        targetSdk = 36
        versionCode = 1_000_000 * versionMajor + 1_000 * versionMinor + versionPatch + versionBuild
        versionName = "$versionMajor.$versionMinor.$versionPatch"

        // URL of the link server (returns current call link as JSON: {"link":"..."})
        // Override at build time: -PlinkServerUrl=http://YOUR_SERVER_IP:8080/link
        val linkServerUrl = project.findProperty("linkServerUrl") as String?
            ?: "http://YOUR_SERVER_IP:8080/link"
        buildConfigField("String", "LINK_SERVER_URL", "\"$linkServerUrl\"")

        testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"
    }

    signingConfigs {
        getByName("debug") {
            storeFile = file("../debug.keystore")
            storePassword = "android"
            keyAlias = "debug"
            keyPassword = "android"
        }
    }

    buildTypes {
        debug {
            signingConfig = signingConfigs.getByName("debug")
        }
        release {
            isMinifyEnabled = false
            signingConfig = signingConfigs.getByName("debug")
            proguardFiles(
                getDefaultProguardFile("proguard-android-optimize.txt"),
                "proguard-rules.pro"
            )
        }
    }
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_11
        targetCompatibility = JavaVersion.VERSION_11
    }
    kotlinOptions {
        jvmTarget = "11"
    }
}

dependencies {
    implementation(fileTree(mapOf("dir" to "libs", "include" to listOf("*.aar"))))
    implementation(libs.androidx.core.ktx)
    implementation(libs.androidx.appcompat)
    implementation(libs.material)
    implementation(libs.androidx.activity)
    implementation(libs.androidx.constraintlayout)
    testImplementation(libs.junit)
    androidTestImplementation(libs.androidx.junit)
    androidTestImplementation(libs.androidx.espresso.core)
}