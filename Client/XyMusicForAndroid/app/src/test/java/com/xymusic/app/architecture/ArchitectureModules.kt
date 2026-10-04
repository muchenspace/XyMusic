package com.xymusic.app.architecture

import java.nio.file.Files
import java.nio.file.Path
import java.nio.file.Paths
import java.util.stream.Collectors

/**
 * Maps source files and imports to Gradle modules by source path.
 *
 * The app module and the standalone core modules share the `com.xymusic.app.core.*`
 * package space (split packages), so a package prefix alone cannot tell which module
 * a symbol belongs to. Module ownership is therefore derived from the file path, and
 * imports are attributed to the modules whose source trees can provide them.
 */
internal object ArchitectureModules {
    const val APP = ":app"
    const val CORE_MODEL = ":core:model"
    const val CORE_DATABASE = ":core:database"
    const val CORE_NETWORK = ":core:network"
    const val CORE_UI = ":core:ui"
    const val DOMAIN = ":domain"

    val projectRoot: Path = findProjectRoot()

    private val MAIN_SOURCE_SUFFIXES =
        listOf(
            Paths.get("src", "main", "java"),
            Paths.get("src", "main", "kotlin"),
        )

    /** Main source root of every Gradle module, keyed by module path. */
    val mainSourceRoots: Map<String, Path> =
        mapOf(
            APP to projectRoot.resolve("app"),
            CORE_MODEL to projectRoot.resolve(Paths.get("core", "model")),
            CORE_DATABASE to projectRoot.resolve(Paths.get("core", "database")),
            CORE_NETWORK to projectRoot.resolve(Paths.get("core", "network")),
            CORE_UI to projectRoot.resolve(Paths.get("core", "ui")),
            DOMAIN to projectRoot.resolve("domain"),
        ).mapValues { (module, moduleRoot) -> mainSourceRoot(module, moduleRoot) }

    /** Derives the Gradle module that owns [sourceFile] from its path. */
    fun moduleOf(sourceFile: Path): String {
        val path = sourceFile.toAbsolutePath().normalize()
        return mainSourceRoots.entries
            .filter { (_, root) -> path.startsWith(root) }
            .maxByOrNull { (_, root) -> root.nameCount }
            ?.key
            ?: error("Source file is not inside a known module: $path")
    }

    /** All Kotlin main source files of [module]. */
    fun sourceFilesOf(module: String): List<Path> = kotlinSourceFiles(mainSourceRoots.getValue(module))

    /**
     * Returns the modules that can provide [importPath], preferring an exact
     * declaration file and otherwise the deepest matching package directory.
     * Split packages can resolve to more than one module.
     */
    fun modulesForImport(importPath: String): Set<String> {
        if (!importPath.startsWith("com.xymusic.app.")) return emptySet()
        if (importPath.endsWith(".*")) {
            return modulesForDirectory(importPath.removeSuffix(".*").replace('.', '/'))
        }
        val packagePath = importPath.substringBeforeLast('.', missingDelimiterValue = "").replace('.', '/')
        val symbol = importPath.substringAfterLast('.')
        val declaringFiles = modulesDeclaringFile(packagePath, symbol)
        return declaringFiles.ifEmpty { modulesForDirectory(packagePath) }
    }

    private fun modulesDeclaringFile(packagePath: String, symbol: String): Set<String> = mainSourceRoots
        .filter { (_, root) -> Files.isRegularFile(root.resolve(packagePath).resolve("$symbol.kt")) }
        .keys

    private fun modulesForDirectory(relativeDirectory: String): Set<String> {
        var current = relativeDirectory
        while (current.isNotEmpty()) {
            val matches =
                mainSourceRoots
                    .filter { (_, root) -> Files.isDirectory(root.resolve(current)) }
                    .keys
            if (matches.isNotEmpty()) return matches
            current = current.substringBeforeLast('/', missingDelimiterValue = "")
        }
        return emptySet()
    }

    private fun kotlinSourceFiles(root: Path): List<Path> {
        check(Files.isDirectory(root)) { "Source directory does not exist: $root" }
        Files.walk(root).use { paths ->
            return paths
                .filter { path -> Files.isRegularFile(path) && path.fileName.toString().endsWith(".kt") }
                .sorted()
                .collect(Collectors.toList())
        }
    }

    private fun mainSourceRoot(module: String, moduleRoot: Path): Path = MAIN_SOURCE_SUFFIXES
        .map(moduleRoot::resolve)
        .firstOrNull(Files::isDirectory)
        ?: error("Missing main source root for module $module: $moduleRoot")

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
        error("Cannot locate the project root from ${Paths.get("").toAbsolutePath()}")
    }
}
