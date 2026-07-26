package com.xymusic.app.domain.architecture

import com.google.common.truth.Truth.assertWithMessage
import java.nio.charset.StandardCharsets
import java.nio.file.Files
import java.nio.file.Path
import java.nio.file.Paths
import java.util.stream.Collectors
import org.junit.Test

/**
 * domain 必须保持纯 Kotlin/JVM：
 * 不得引用 Android、DI 框架、网络/存储框架或 app 外层包。
 */
class DomainPurityArchitectureTest {
    @Test
    fun domainSourcesOnlyImportApprovedPackages() {
        val violations =
            domainSourceFiles().flatMap { sourceFile ->
                importsOf(sourceFile)
                    .filterNot { importPath -> approvedImportPrefixes.any(importPath::startsWith) }
                    .map { importPath -> "${sourceFile.fileName}: $importPath" }
            }

        assertWithMessage("domain 源码引用了未被允许的外层依赖")
            .that(violations)
            .isEmpty()
    }

    @Test
    fun domainSourcesNeverImportForbiddenFrameworks() {
        val violations =
            domainSourceFiles().flatMap { sourceFile ->
                importsOf(sourceFile)
                    .filter { importPath -> forbiddenImportPrefixes.any(importPath::startsWith) }
                    .map { importPath -> "${sourceFile.fileName}: $importPath" }
            }

        assertWithMessage("domain 源码不得引用 Android/DI/网络框架")
            .that(violations)
            .isEmpty()
    }

    @Test
    fun domainBuildScriptStaysPureJvm() {
        val buildScript =
            String(
                Files.readAllBytes(domainModuleRoot.resolve("build.gradle.kts")),
                StandardCharsets.UTF_8,
            )

        forbiddenBuildScriptFragments.forEach { fragment ->
            assertWithMessage("domain/build.gradle.kts 不得包含 $fragment")
                .that(buildScript)
                .doesNotContain(fragment)
        }
        assertWithMessage("domain 必须使用 kotlin jvm 插件")
            .that(buildScript)
            .contains("libs.plugins.kotlin.jvm")
    }

    private fun domainSourceFiles(): List<Path> {
        val mainSourceRoot = domainModuleRoot.resolve(Paths.get("src", "main", "java"))
        check(Files.isDirectory(mainSourceRoot)) { "domain 主源码目录不存在: $mainSourceRoot" }
        Files.walk(mainSourceRoot).use { paths ->
            return paths
                .filter { path -> Files.isRegularFile(path) && path.fileName.toString().endsWith(".kt") }
                .sorted()
                .collect(Collectors.toList())
        }
    }

    private fun importsOf(sourceFile: Path): List<String> =
        String(Files.readAllBytes(sourceFile), StandardCharsets.UTF_8)
            .lineSequence()
            .map(String::trim)
            .filter { it.startsWith("import ") }
            .map { line ->
                line
                    .removePrefix("import ")
                    .substringBefore(" as ")
                    .trim()
            }.toList()

    private companion object {
        private val approvedImportPrefixes =
            listOf(
                "java.",
                "kotlin.",
                "kotlinx.coroutines.",
                "com.xymusic.app.core.model.",
                "com.xymusic.app.domain.",
                "com.xymusic.app.feature.",
            )

        private val forbiddenImportPrefixes =
            listOf(
                "android.",
                "androidx.",
                "javax.inject.",
                "dagger.",
                "okhttp3.",
                "retrofit2.",
                "kotlinx.serialization.",
                "com.xymusic.app.core.network.",
                "com.xymusic.app.core.preferences.",
                "com.xymusic.app.core.data.",
                "com.xymusic.app.data.",
                "com.xymusic.app.core.ui.",
            )

        private val forbiddenBuildScriptFragments =
            listOf(
                "android.library",
                "kotlin.android",
                "plugins.ksp",
                "plugins.hilt",
                "hilt.android",
                "hilt.compiler",
                "paging",
                "coroutines.android",
                "core:network",
                "core:preferences",
            )

        private val domainModuleRoot: Path = findDomainModuleRoot()

        private fun findDomainModuleRoot(): Path {
            var currentDirectory: Path? = Paths.get("").toAbsolutePath().normalize()
            while (currentDirectory != null) {
                if (currentDirectory.fileName?.toString() == "domain" &&
                    Files.isRegularFile(currentDirectory.resolve("build.gradle.kts"))
                ) {
                    return currentDirectory
                }
                val candidate = currentDirectory.resolve("domain")
                if (Files.isRegularFile(candidate.resolve("build.gradle.kts"))) {
                    return candidate
                }
                currentDirectory = currentDirectory.parent
            }
            error("无法定位 domain 模块目录: ${Paths.get("").toAbsolutePath()}")
        }
    }
}
