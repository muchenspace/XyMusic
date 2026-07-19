package com.xymusic.app.architecture

import java.nio.charset.StandardCharsets
import java.nio.file.Files
import java.nio.file.Path
import java.nio.file.Paths
import java.util.stream.Collectors
import org.junit.Assert.fail
import org.junit.Test

class FeatureDependencyArchitectureTest {
    @Test
    fun featureImportsFollowApprovedDirections() {
        val violations =
            featureImports()
                .filter { dependency ->
                    dependency.sourceFeature != dependency.targetFeature &&
                        dependency.targetFeature !in
                        allowedFeatureDependencies[dependency.sourceFeature].orEmpty()
                }.map { dependency ->
                    "${relativePath(dependency.sourceFile)}: " +
                        "${dependency.sourceFeature} -> ${dependency.targetFeature} " +
                        "(${dependency.importPath})"
                }.sorted()

        assertNoViolations(
            message =
            "Unapproved cross-feature imports found. " +
                "Move shared contracts to core or use an approved dependency direction.",
            violations = violations,
        )
    }

    @Test
    fun featureImportsDoNotCreateCycles() {
        val dependencyGraph =
            featureImports()
                .filter { it.sourceFeature != it.targetFeature }
                .groupBy(
                    keySelector = FeatureImport::sourceFeature,
                    valueTransform = FeatureImport::targetFeature,
                ).mapValues { (_, targets) -> targets.toSet() }

        assertNoViolations(
            message = "Circular feature dependencies found.",
            violations = findCycles(dependencyGraph),
        )
    }

    @Test
    fun sharedMediaLayersDoNotDependOnCatalogInternals() {
        val allMainSources = mainSourceRoots.flatMap(::kotlinSourceFiles)
        val sourcesByLayer =
            sharedMediaPackagePrefixes.mapValues { (_, packagePrefix) ->
                allMainSources.filter { sourceFile ->
                    val packageName = packageOf(sourceFile)
                    packageName == packagePrefix || packageName.startsWith("$packagePrefix.")
                }
            }
        val missingLayers =
            sourcesByLayer
                .filterValues { sources -> sources.isEmpty() }
                .keys
                .map { layerName -> "Missing shared media layer: $layerName" }

        val catalogImports =
            sourcesByLayer.values
                .flatten()
                .flatMap { sourceFile ->
                    importsOf(sourceFile)
                        .filter { importPath -> importPath.startsWith(CATALOG_PACKAGE_PREFIX) }
                        .map { importPath -> "${relativePath(sourceFile)} imports $importPath" }
                }

        assertNoViolations(
            message = "Shared media model, data, and UI must not depend on catalog internals.",
            violations = (missingLayers + catalogImports).sorted(),
        )
    }

    private fun featureImports(): List<FeatureImport> = kotlinSourceFiles(featureSourceRoot).flatMap { sourceFile ->
        val sourceFeature =
            featureName(packageOf(sourceFile))
                ?: error("Source is outside a feature package: ${relativePath(sourceFile)}")

        importsOf(sourceFile).mapNotNull { importPath ->
            val targetFeature = featureName(importPath) ?: return@mapNotNull null
            FeatureImport(
                sourceFeature = sourceFeature,
                targetFeature = targetFeature,
                sourceFile = sourceFile,
                importPath = importPath,
            )
        }
    }

    private fun findCycles(dependencyGraph: Map<String, Set<String>>): List<String> {
        val activePath = mutableListOf<String>()
        val completed = mutableSetOf<String>()
        val cycles = mutableSetOf<String>()

        fun visit(feature: String) {
            val activeIndex = activePath.indexOf(feature)
            if (activeIndex >= 0) {
                cycles +=
                    (activePath.subList(activeIndex, activePath.size) + feature)
                        .joinToString(" -> ")
                return
            }
            if (feature in completed) return

            activePath += feature
            dependencyGraph[feature].orEmpty().sorted().forEach(::visit)
            activePath.removeAt(activePath.lastIndex)
            completed += feature
        }

        val allFeatures = dependencyGraph.keys + dependencyGraph.values.flatten()
        allFeatures.sorted().forEach(::visit)
        return cycles.sorted()
    }

    private fun packageOf(sourceFile: Path): String {
        val packageLine =
            sourceText(sourceFile)
                .lineSequence()
                .map(String::trim)
                .firstOrNull { it.startsWith("package ") }
                ?: error("Missing package declaration: ${relativePath(sourceFile)}")
        return packageLine.removePrefix("package ").trim()
    }

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

    private fun featureName(packageOrImport: String): String? {
        if (!packageOrImport.startsWith(FEATURE_PACKAGE_PREFIX)) return null
        return packageOrImport
            .removePrefix(FEATURE_PACKAGE_PREFIX)
            .substringBefore('.')
            .takeIf(String::isNotBlank)
    }

    private fun sourceText(sourceFile: Path): String = String(Files.readAllBytes(sourceFile), StandardCharsets.UTF_8)

    private fun kotlinSourceFiles(root: Path): List<Path> {
        check(Files.isDirectory(root)) { "Source directory does not exist: $root" }
        val paths = Files.walk(root)
        return try {
            paths
                .filter { path ->
                    Files.isRegularFile(path) && path.fileName.toString().endsWith(".kt")
                }.sorted()
                .collect(Collectors.toList())
        } finally {
            paths.close()
        }
    }

    private fun relativePath(path: Path): String = projectRoot.relativize(path).toString().replace('\\', '/')

    private fun assertNoViolations(message: String, violations: List<String>) {
        if (violations.isNotEmpty()) {
            fail("$message\n${violations.joinToString(separator = "\n")}")
        }
    }

    private data class FeatureImport(
        val sourceFeature: String,
        val targetFeature: String,
        val sourceFile: Path,
        val importPath: String,
    )

    companion object {
        private const val FEATURE_PACKAGE_PREFIX = "com.xymusic.app.feature."
        private const val CATALOG_PACKAGE_PREFIX = "com.xymusic.app.feature.catalog."

        private val allowedFeatureDependencies =
            mapOf(
                "library" to setOf("player", "playlist"),
                "playlist" to setOf("player"),
                "settings" to setOf("auth", "server"),
            )

        private val sharedMediaPackagePrefixes =
            mapOf(
                "model" to "com.xymusic.app.core.model.media",
                "data" to "com.xymusic.app.core.data.media",
                "UI" to "com.xymusic.app.core.ui.media",
            )

        private val javaMainSourceSuffix = Paths.get("src", "main", "java")
        private val kotlinMainSourceSuffix = Paths.get("src", "main", "kotlin")
        private val projectRoot: Path = findProjectRoot()
        private val mainSourceRoots: List<Path> = findMainSourceRoots(projectRoot)
        private val mainSourceRoot: Path =
            projectRoot.resolve(Paths.get("app", "src", "main", "java"))
        private val featureSourceRoot: Path =
            mainSourceRoot.resolve(Paths.get("com", "xymusic", "app", "feature"))

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

        private fun findMainSourceRoots(root: Path): List<Path> {
            val paths = Files.walk(root, 5)
            return try {
                paths
                    .filter { path ->
                        Files.isDirectory(path) &&
                            (
                                path.endsWith(javaMainSourceSuffix) ||
                                    path.endsWith(kotlinMainSourceSuffix)
                                )
                    }.sorted()
                    .collect(Collectors.toList())
            } finally {
                paths.close()
            }
        }
    }
}
