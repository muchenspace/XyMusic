package com.xymusic.app.architecture

import java.nio.charset.StandardCharsets
import java.nio.file.Files
import java.nio.file.Path
import java.nio.file.Paths
import java.util.stream.Collectors
import org.junit.Assert.fail
import org.junit.Test

/**
 * Keeps feature packages directed toward the domain and shared contracts.
 * Framework adapters may be shared by outer layers, but service/data code may
 * not depend on another feature layer that owns runtime or UI behavior.
 */
class LayerDependencyArchitectureTest {
    @Test
    fun sharedCoreDoesNotDependOnFeatureImplementations() {
        val violations = sharedSourceFiles(coreSourceRoot).flatMap { sourceFile ->
            importsOf(sourceFile)
                .filter { importPath -> importPath.startsWith(FEATURE_PACKAGE_PREFIX) }
                .map { importPath -> "${relativePath(sourceFile)}: $importPath" }
        }

        assertNoViolations(
            "Shared core must not depend on feature implementations or feature UI.",
            violations,
        )
    }

    @Test
    fun sharedDataDoesNotDependOnFeatureImplementations() {
        val violations = sharedSourceFiles(dataSourceRoot).flatMap { sourceFile ->
            importsOf(sourceFile)
                .filter { importPath -> importPath.startsWith(FEATURE_PACKAGE_PREFIX) }
                .map { importPath -> "${relativePath(sourceFile)}: $importPath" }
        }

        assertNoViolations(
            "Shared data adapters must be feature-agnostic; bind feature handlers in app composition.",
            violations,
        )
    }

    @Test
    fun featurePresentationDoesNotDependOnApplicationOrInfrastructureLayers() {
        val violations =
            sourceFilesByLayer("presentation").flatMap { sourceFile ->
                importsOf(sourceFile)
                    .filter { importPath ->
                        importPath.startsWith("com.xymusic.app.app.") ||
                            featureLayerOf(importPath) in setOf("data", "service")
                    }.map { importPath -> "${relativePath(sourceFile)}: $importPath" }
            }

        assertNoViolations(
            "Feature presentation must depend on domain/shared UI contracts, " +
                "not the application composition root or infrastructure.",
            violations,
        )
    }

    @Test
    fun featureDataDoesNotDependOnPresentationOrService() {
        val violations =
            sourceFilesByLayer("data").flatMap { sourceFile ->
                importsOf(sourceFile)
                    .filter { importPath ->
                        featureLayerOf(importPath) in setOf("presentation", "service")
                    }.map { importPath -> "${relativePath(sourceFile)}: $importPath" }
            }

        assertNoViolations(
            "Feature data adapters must not depend on UI or service implementations.",
            violations,
        )
    }

    @Test
    fun featureServicesDoNotDependOnDataImplementations() {
        val violations =
            sourceFilesByLayer("service").flatMap { sourceFile ->
                importsOf(sourceFile)
                    .filter { importPath -> featureLayerOf(importPath) == "data" }
                    .map { importPath -> "${relativePath(sourceFile)}: $importPath" }
            }

        assertNoViolations(
            "Player services must consume domain ports and shared adapters, " +
                "not data implementations.",
            violations,
        )
    }

    @Test
    fun featureSourcesDoNotDependOnApplicationCompositionRoot() {
        val violations =
            kotlinSourceFiles(featureSourceRoot).flatMap { sourceFile ->
                importsOf(sourceFile)
                    .filter { importPath -> importPath.startsWith("com.xymusic.app.app.") }
                    .map { importPath -> "${relativePath(sourceFile)}: $importPath" }
            }

        assertNoViolations(
            "Feature implementations must be composed by app, not import app internals.",
            violations,
        )
    }

    private fun sourceFilesByLayer(layer: String): List<Path> =
        kotlinSourceFiles(featureSourceRoot).filter { sourceFile ->
            featureLayerOf(packageOf(sourceFile)) == layer
        }

    private fun sharedSourceFiles(root: Path): List<Path> = kotlinSourceFiles(root)

    private fun packageOf(sourceFile: Path): String = sourceText(sourceFile)
        .lineSequence()
        .map(String::trim)
        .firstOrNull { it.startsWith("package ") }
        ?.removePrefix("package ")
        ?.trim()
        ?: error("Missing package declaration: ${relativePath(sourceFile)}")

    private fun importsOf(sourceFile: Path): List<String> = sourceText(sourceFile)
        .lineSequence()
        .map(String::trim)
        .filter { it.startsWith("import ") }
        .map { line ->
            line
                .removePrefix("import ")
                .substringBefore(" as ")
                .substringBefore("//")
                .trim()
        }.toList()

    private fun featureLayerOf(packageOrImport: String): String? {
        if (!packageOrImport.startsWith(FEATURE_PACKAGE_PREFIX)) return null
        return packageOrImport
            .removePrefix(FEATURE_PACKAGE_PREFIX)
            .split('.')
            .getOrNull(1)
            ?.takeIf(String::isNotBlank)
    }

    private fun sourceText(sourceFile: Path): String = String(Files.readAllBytes(sourceFile), StandardCharsets.UTF_8)

    private fun kotlinSourceFiles(root: Path): List<Path> {
        check(Files.isDirectory(root)) { "Source directory does not exist: $root" }
        Files.walk(root).use { paths ->
            return paths
                .filter { path ->
                    Files.isRegularFile(path) && path.fileName.toString().endsWith(".kt")
                }.sorted()
                .collect(Collectors.toList())
        }
    }

    private fun relativePath(path: Path): String = projectRoot.relativize(path).toString().replace('\\', '/')

    private fun assertNoViolations(message: String, violations: List<String>) {
        if (violations.isNotEmpty()) {
            fail("$message\n${violations.sorted().joinToString(separator = "\n")}")
        }
    }

    companion object {
        private const val FEATURE_PACKAGE_PREFIX = "com.xymusic.app.feature."
        private val projectRoot: Path = findProjectRoot()
        private val mainSourceRoot = projectRoot.resolve(Paths.get("app", "src", "main", "java"))
        private val coreSourceRoot = mainSourceRoot.resolve(Paths.get("com", "xymusic", "app", "core"))
        private val dataSourceRoot = mainSourceRoot.resolve(Paths.get("com", "xymusic", "app", "data"))
        private val featureSourceRoot: Path =
            mainSourceRoot.resolve(Paths.get("com", "xymusic", "app", "feature"))

        private fun findProjectRoot(): Path {
            var currentDirectory: Path? = Paths.get("").toAbsolutePath().normalize()
            while (currentDirectory != null) {
                val directory = currentDirectory
                if (
                    Files.isRegularFile(directory.resolve("settings.gradle.kts")) &&
                    Files.isDirectory(directory.resolve(Paths.get("app", "src", "main", "java")))
                ) {
                    return directory
                }
                currentDirectory = directory.parent
            }
            error("Cannot locate project root")
        }
    }
}
