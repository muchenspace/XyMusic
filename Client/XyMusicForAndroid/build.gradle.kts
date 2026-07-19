import dev.detekt.gradle.Detekt
import dev.detekt.gradle.extensions.DetektExtension
import org.jlleitschuh.gradle.ktlint.KtlintExtension
import org.jetbrains.kotlin.gradle.tasks.KotlinCompilationTask

plugins {
    alias(libs.plugins.android.application) apply false
    alias(libs.plugins.android.library) apply false
    alias(libs.plugins.kotlin.android) apply false
    alias(libs.plugins.kotlin.jvm) apply false
    alias(libs.plugins.kotlin.compose) apply false
    alias(libs.plugins.kotlin.serialization) apply false
    alias(libs.plugins.ksp) apply false
    alias(libs.plugins.hilt) apply false
    alias(libs.plugins.android.test) apply false
    alias(libs.plugins.androidx.baselineprofile) apply false
    alias(libs.plugins.detekt) apply false
    alias(libs.plugins.ktlint) apply false
}

val detektVersion = libs.versions.detekt.get()
val ktlintVersion = libs.versions.ktlint.core.get()

subprojects {
    pluginManager.apply("dev.detekt")
    pluginManager.apply("org.jlleitschuh.gradle.ktlint")

    extensions.configure<DetektExtension> {
        toolVersion.set(detektVersion)
        config.setFrom(rootProject.files("config/detekt/detekt.yml"))
        source.setFrom(
            fileTree("src") {
                include("**/*.kt", "**/*.kts")
                exclude("**/build/**", "**/generated/**", "**/schemas/**")
            },
        )
        basePath.set(rootProject.layout.projectDirectory)
        buildUponDefaultConfig.set(true)
        allRules.set(false)
        parallel.set(true)
        ignoreFailures.set(false)
        autoCorrect.set(false)
    }

    tasks.withType<Detekt>().configureEach {
        exclude("**/build/**", "**/generated/**", "**/schemas/**")
        exclude { element ->
            val path = element.file.invariantSeparatorsPath
            path.contains("/build/") ||
                path.contains("/generated/") ||
                path.contains("/schemas/")
        }
    }

    extensions.configure<KtlintExtension> {
        version.set(ktlintVersion)
        ignoreFailures.set(false)
        outputToConsole.set(true)
        filter {
            exclude("**/build/**")
            exclude("**/generated/**")
            exclude("**/schemas/**")
            exclude { element ->
                val path = element.file.invariantSeparatorsPath
                path.contains("/build/") ||
                    path.contains("/generated/") ||
                    path.contains("/schemas/")
            }
        }
    }

    listOf(
        "com.android.application",
        "com.android.library",
        "com.android.test",
    ).forEach { androidPluginId ->
        pluginManager.withPlugin(androidPluginId) {
            extensions.configure<KtlintExtension> {
                android.set(true)
            }
        }
    }

    tasks.withType<KotlinCompilationTask<*>>().configureEach {
        compilerOptions {
            allWarningsAsErrors.set(true)
        }
    }
}
