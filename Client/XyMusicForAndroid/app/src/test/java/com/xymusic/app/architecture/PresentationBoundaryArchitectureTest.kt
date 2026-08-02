package com.xymusic.app.architecture

import java.nio.charset.StandardCharsets
import java.nio.file.Files
import java.nio.file.Path
import java.nio.file.Paths
import java.util.stream.Collectors
import org.junit.Assert.fail
import org.junit.Test

/**
 * Presentation 只能调用输入用例边界：
 * 不得 import data/网络基础设施，不得直接依赖 Repository/Store 输出端口，
 * 也不得横向 import 其他 feature 的 presentation。
 */
class PresentationBoundaryArchitectureTest {
    @Test
    fun presentationDoesNotImportInfrastructure() {
        val violations =
            presentationSourceFiles().flatMap { sourceFile ->
                importsOf(sourceFile)
                    .filter { importPath -> forbiddenInfrastructurePrefixes.any(importPath::startsWith) }
                    .map { importPath -> "${relativePath(sourceFile)}: $importPath" }
            }

        assertNoViolations("Presentation 不得 import data/网络基础设施", violations)
    }

    @Test
    fun presentationDoesNotImportApplicationCompositionRoot() {
        val violations = presentationSourceFiles().flatMap { sourceFile ->
            importsOf(sourceFile)
                .filter { importPath -> importPath.startsWith("com.xymusic.app.app.") }
                .map { importPath -> "${relativePath(sourceFile)}: $importPath" }
        }

        assertNoViolations("Feature presentation must not import app composition internals", violations)
    }

    @Test
    fun presentationDoesNotImportFeatureInfrastructure() {
        val violations =
            presentationSourceFiles().flatMap { sourceFile ->
                importsOf(sourceFile)
                    .filter { importPath -> featureLayerOf(importPath) in forbiddenFeatureLayers }
                    .map { importPath -> "${relativePath(sourceFile)}: $importPath" }
            }

        assertNoViolations("feature presentation must not import feature data/service", violations)
    }

    @Test
    fun presentationDoesNotImportOutputPorts() {
        val violations =
            presentationSourceFiles().flatMap { sourceFile ->
                importsOf(sourceFile)
                    .filter { importPath ->
                        importPath.startsWith("com.xymusic.app.") &&
                            (importPath.contains(".domain.") || importPath.contains(".domain")) &&
                            outputPortSuffixes.any(importPath::endsWith)
                    }.map { importPath -> "${relativePath(sourceFile)}: $importPath" }
            }

        assertNoViolations("Presentation 不得直接依赖 Repository/Store 输出端口", violations)
    }

    @Test
    fun presentationDoesNotImportOtherFeaturePresentation() {
        val violations =
            presentationSourceFiles().mapNotNull { sourceFile ->
                val sourceFeature = featureOf(packageOf(sourceFile)) ?: return@mapNotNull null
                importsOf(sourceFile)
                    .filter { importPath ->
                        val targetFeature = featureOf(importPath)
                        targetFeature != null &&
                            targetFeature != sourceFeature &&
                            importPath.contains(".presentation.")
                    }.map { importPath -> "${relativePath(sourceFile)}: $importPath" }
                    .takeIf(List<String>::isNotEmpty)
            }.flatten()

        assertNoViolations("feature presentation 不得横向 import 其他 feature presentation", violations)
    }

    private fun presentationSourceFiles(): List<Path> {
        Files.walk(featureSourceRoot).use { paths ->
            return paths
                .filter { path ->
                    Files.isRegularFile(path) &&
                        path.fileName.toString().endsWith(".kt") &&
                        path.toString().replace('\\', '/').contains("/presentation/")
                }.sorted()
                .collect(Collectors.toList())
        }
    }

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
                .trim()
        }.toList()

    private fun featureOf(packageOrImport: String): String? {
        if (!packageOrImport.startsWith(FEATURE_PACKAGE_PREFIX)) return null
        return packageOrImport
            .removePrefix(FEATURE_PACKAGE_PREFIX)
            .substringBefore('.')
            .takeIf(String::isNotBlank)
    }

    private fun featureLayerOf(importPath: String): String? = importPath
        .removePrefix(FEATURE_PACKAGE_PREFIX)
        .takeIf { it != importPath }
        ?.substringAfter('.', missingDelimiterValue = "")
        ?.substringBefore('.', missingDelimiterValue = "")
        ?.takeIf(String::isNotBlank)

    private fun sourceText(sourceFile: Path): String = String(Files.readAllBytes(sourceFile), StandardCharsets.UTF_8)

    private fun relativePath(path: Path): String = projectRoot.relativize(path).toString().replace('\\', '/')

    private fun assertNoViolations(message: String, violations: List<String>) {
        if (violations.isNotEmpty()) {
            fail("$message\n${violations.sorted().joinToString(separator = "\n")}")
        }
    }

    private companion object {
        private const val FEATURE_PACKAGE_PREFIX = "com.xymusic.app.feature."

        private val forbiddenFeatureLayers = setOf("data", "service")

        private val forbiddenInfrastructurePrefixes =
            listOf(
                "com.xymusic.app.data.",
                "com.xymusic.app.core.data.",
                "com.xymusic.app.core.network.",
                "com.xymusic.app.core.database.",
                "okhttp3.",
                "retrofit2.",
            )

        private val outputPortSuffixes = listOf("Repository", "Store")

        private val projectRoot: Path = findProjectRoot()
        private val featureSourceRoot: Path =
            projectRoot.resolve(Paths.get("app", "src", "main", "java", "com", "xymusic", "app", "feature"))

        private fun findProjectRoot(): Path {
            var currentDirectory: Path? = Paths.get("").toAbsolutePath().normalize()
            while (currentDirectory != null) {
                val directory = currentDirectory
                val settingsFile = directory.resolve("settings.gradle.kts")
                val appSourceRoot = directory.resolve(Paths.get("app", "src", "main", "java"))
                if (Files.isRegularFile(settingsFile) && Files.isDirectory(appSourceRoot)) {
                    return directory
                }
                currentDirectory = directory.parent
            }
            error("Cannot locate the project root from ${Paths.get("").toAbsolutePath()}")
        }
    }
}
