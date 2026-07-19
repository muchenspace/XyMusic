package com.xymusic.app.architecture

import java.nio.file.Files
import java.nio.file.Path
import java.nio.file.Paths
import javax.xml.parsers.DocumentBuilderFactory
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Test
import org.w3c.dom.Element

class ReleaseCleartextTrafficPolicyTest {
    @Test
    fun allBuildVariantsKeepPublicHttpEnabled() {
        val manifests = Files.walk(appSourceRoot).use { paths ->
            paths
                .filter { path ->
                    Files.isRegularFile(path) && path.fileName.toString() == MANIFEST_FILE_NAME
                }
                .sorted()
                .toList()
        }
        val mainManifest = appSourceRoot.resolve(Paths.get("main", MANIFEST_FILE_NAME))
        assertFalse("Missing app main manifest", mainManifest !in manifests)

        manifests.forEach { manifest ->
            val application = parseApplication(manifest)
            val cleartextValue = application.getAttributeNS(ANDROID_NAMESPACE, CLEAR_TEXT_ATTRIBUTE)
            if (manifest == mainManifest) {
                assertEquals(
                    "The main manifest must explicitly allow public HTTP",
                    "true",
                    cleartextValue,
                )
            } else {
                assertFalse(
                    "${relativePath(manifest)} must not disable public HTTP",
                    cleartextValue == "false",
                )
            }
            assertFalse(
                "${relativePath(manifest)} introduces an unverified network security config",
                application.hasAttributeNS(ANDROID_NAMESPACE, NETWORK_SECURITY_CONFIG_ATTRIBUTE),
            )
        }
    }

    private fun parseApplication(manifest: Path): Element = DocumentBuilderFactory.newInstance().apply {
        isNamespaceAware = true
        setFeature("http://apache.org/xml/features/disallow-doctype-decl", true)
        setFeature("http://xml.org/sax/features/external-general-entities", false)
        setFeature("http://xml.org/sax/features/external-parameter-entities", false)
        setAttribute(ACCESS_EXTERNAL_DTD, "")
        setAttribute(ACCESS_EXTERNAL_SCHEMA, "")
    }.newDocumentBuilder()
        .parse(manifest.toFile())
        .documentElement
        .getElementsByTagName("application")
        .item(0)
        .let { node -> node as? Element }
        ?: error("Missing application element: ${relativePath(manifest)}")

    private fun relativePath(path: Path): String = projectRoot.relativize(path).toString().replace('\\', '/')

    companion object {
        private const val ANDROID_NAMESPACE = "http://schemas.android.com/apk/res/android"
        private const val CLEAR_TEXT_ATTRIBUTE = "usesCleartextTraffic"
        private const val NETWORK_SECURITY_CONFIG_ATTRIBUTE = "networkSecurityConfig"
        private const val MANIFEST_FILE_NAME = "AndroidManifest.xml"
        private const val ACCESS_EXTERNAL_DTD =
            "http://javax.xml.XMLConstants/property/accessExternalDTD"
        private const val ACCESS_EXTERNAL_SCHEMA =
            "http://javax.xml.XMLConstants/property/accessExternalSchema"

        private val projectRoot = findProjectRoot()
        private val appSourceRoot = projectRoot.resolve(Paths.get("app", "src"))

        private fun findProjectRoot(): Path {
            var currentDirectory: Path? = Paths.get("").toAbsolutePath().normalize()
            while (currentDirectory != null) {
                val directory = currentDirectory
                if (
                    Files.isRegularFile(directory.resolve("settings.gradle.kts")) &&
                    Files.isDirectory(directory.resolve(Paths.get("app", "src")))
                ) {
                    return directory
                }
                currentDirectory = directory.parent
            }
            error("Cannot locate project root")
        }
    }
}
