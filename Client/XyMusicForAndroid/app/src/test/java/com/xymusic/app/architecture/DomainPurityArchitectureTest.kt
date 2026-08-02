package com.xymusic.app.architecture

import java.nio.charset.StandardCharsets
import java.nio.file.Files
import java.nio.file.Path
import java.nio.file.Paths
import java.util.stream.Collectors
import org.junit.Assert.fail
import org.junit.Test

class DomainPurityArchitectureTest {
    @Test
    fun domainSourcesDoNotDependOnPlatformOrInfrastructure() {
        val violations = domainSourceFiles().flatMap { sourceFile ->
            importsOf(sourceFile)
                .filter(::isForbiddenImport)
                .map { importPath -> "${relativePath(sourceFile)}: $importPath" }
        }.sorted()

        if (violations.isNotEmpty()) {
            fail(
                "The domain module must remain platform-independent and infrastructure-free.\n" +
                    violations.joinToString(separator = "\n"),
            )
        }
    }

    private fun domainSourceFiles(): List<Path> {
        Files.walk(domainSourceRoot).use { paths ->
            return paths
                .filter { path ->
                    Files.isRegularFile(path) && path.fileName.toString().endsWith(".kt")
                }.sorted()
                .collect(Collectors.toList())
        }
    }

    private fun importsOf(sourceFile: Path): List<String> = sourceText(sourceFile)
        .lineSequence()
        .map(String::trim)
        .filter { it.startsWith("import ") }
        .map { line ->
            line
                .removePrefix("import ")
                .substringBefore(" as ")
                .trim()
        }.toList()

    private fun isForbiddenImport(importPath: String): Boolean = forbiddenPrefixes.any(importPath::startsWith) ||
        featureLayerOf(importPath) in forbiddenFeatureLayers

    private fun featureLayerOf(importPath: String): String? = importPath
        .removePrefix(FEATURE_PACKAGE_PREFIX)
        .takeIf { it != importPath }
        ?.substringAfter('.', missingDelimiterValue = "")
        ?.substringBefore('.', missingDelimiterValue = "")
        ?.takeIf(String::isNotBlank)

    private fun sourceText(sourceFile: Path): String = String(Files.readAllBytes(sourceFile), StandardCharsets.UTF_8)

    private fun relativePath(path: Path): String = projectRoot.relativize(path).toString().replace('\\', '/')

    companion object {
        private const val FEATURE_PACKAGE_PREFIX = "com.xymusic.app.feature."

        private val forbiddenFeatureLayers = setOf("data", "presentation", "service")

        private val forbiddenPrefixes = listOf(
            "android.",
            "androidx.",
            "com.xymusic.app.app.",
            "com.xymusic.app.data.",
            "com.xymusic.app.core.data.",
            "com.xymusic.app.core.database.",
            "com.xymusic.app.core.network.",
            "com.xymusic.app.core.ui.",
            "dagger.",
            "javax.inject.",
            "okhttp3.",
            "retrofit2.",
            "coil3.",
        )

        private val projectRoot: Path = findProjectRoot()
        private val domainSourceRoot: Path =
            projectRoot.resolve(Paths.get("domain", "src", "main"))

        private fun findProjectRoot(): Path {
            var currentDirectory: Path? = Paths.get("").toAbsolutePath().normalize()
            while (currentDirectory != null) {
                val directory = currentDirectory
                if (
                    Files.isRegularFile(directory.resolve("settings.gradle.kts")) &&
                    Files.isDirectory(directory.resolve(Paths.get("domain", "src", "main")))
                ) {
                    return directory
                }
                currentDirectory = directory.parent
            }
            error("Cannot locate project root")
        }
    }
}
