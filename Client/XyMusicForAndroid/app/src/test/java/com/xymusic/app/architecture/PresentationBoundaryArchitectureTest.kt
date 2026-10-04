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
 *
 * App 模块自身的 UI 层（`app.*`、`ui.*` 与 app 内 `core.ui.*`）适用同一约束，
 * 只有组合根（`app.di`、`app.session`）可以装配基础设施适配器。
 */
class PresentationBoundaryArchitectureTest {
    @Test
    fun presentationDoesNotImportInfrastructure() {
        val violations =
            presentationSourceFiles().flatMap { sourceFile ->
                importsOf(sourceFile)
                    .filter(::isForbiddenFeaturePresentationImport)
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

    /**
     * App 模块自身 UI 层不得 import data/数据库/网络/会话/同步/安全/性能/分页基础设施。
     * 组合根包（`app.di`、`app.session`）必须装配这些适配器，按包排除；
     * app shell 根包保留最小允许清单，其余 app 内 core 包按 fail-closed 处理。
     */
    @Test
    fun appUiLayerDoesNotImportInfrastructure() {
        val violations =
            appUiLayerSourceFiles().flatMap { sourceFile ->
                val shellAllowedImports =
                    if (packageOf(sourceFile) == APP_SHELL_PACKAGE) {
                        shellCompositionAllowedImports
                    } else {
                        emptySet()
                    }
                importsOf(sourceFile)
                    .filter { importPath -> targetsAppUiInfrastructure(importPath, shellAllowedImports) }
                    .map { importPath -> "${relativePath(sourceFile)}: $importPath" }
            }

        assertNoViolations("app 模块 UI 层不得 import data/网络/会话等基础设施", violations)
    }

    /**
     * App 模块自身 UI 层的源码：`app.*`、`ui.*` 与 app 内 `core.ui.*`。
     * 组合根包必须装配基础设施，按包（而非文件名）排除。
     */
    private fun appUiLayerSourceFiles(): List<Path> =
        appUiLayerRoots.flatMap(::kotlinSourceFiles).filterNot { sourceFile ->
            val packageName = packageOf(sourceFile)
            compositionRootPackages.any { root -> packageName == root || packageName.startsWith("$root.") }
        }

    /**
     * App 模块 UI 层只允许共享工具/模型/UI 契约（见 [allowedAppUiCorePrefixes]）；
     * 其余 app 内 `core.*` 与 `data.*` 一律视为基础设施（fail-closed），
     * 新增包若确属契约，必须显式加入允许清单并说明原因。
     */
    private fun targetsAppUiInfrastructure(importPath: String, shellAllowedImports: Set<String>): Boolean {
        if (importPath.startsWith("com.xymusic.app.data.")) return true
        if (!importPath.startsWith("com.xymusic.app.core.")) return false
        return allowedAppUiCorePrefixes.none(importPath::startsWith) && importPath !in shellAllowedImports
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

    /**
     * fail-closed：app 模块 core.* 只允许 core.common 工具（合法例外）与
     * core:model/core:ui 契约（含 app 内 core.ui 共享 UI），其余包（含未来新增）
     * 一律视为基础设施；同时禁止直接依赖网络框架。
     */
    private fun isForbiddenFeaturePresentationImport(importPath: String): Boolean {
        if (forbiddenFrameworkPrefixes.any(importPath::startsWith)) return true
        if (importPath.startsWith(APP_DATA_PACKAGE_PREFIX)) return true
        if (!importPath.startsWith(APP_CORE_PACKAGE_PREFIX)) return false
        val corePackage = importPath.removePrefix(APP_CORE_PACKAGE_PREFIX).substringBefore('.')
        return corePackage !in allowedFeaturePresentationCorePackages
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

    private fun kotlinSourceFiles(root: Path): List<Path> {
        check(Files.isDirectory(root)) { "Source directory does not exist: $root" }
        Files.walk(root).use { paths ->
            return paths
                .filter { path -> Files.isRegularFile(path) && path.fileName.toString().endsWith(".kt") }
                .sorted()
                .collect(Collectors.toList())
        }
    }

    private fun relativePath(path: Path): String = projectRoot.relativize(path).toString().replace('\\', '/')

    private fun assertNoViolations(message: String, violations: List<String>) {
        if (violations.isNotEmpty()) {
            fail("$message\n${violations.sorted().joinToString(separator = "\n")}")
        }
    }

    private companion object {
        private const val FEATURE_PACKAGE_PREFIX = "com.xymusic.app.feature."

        private val forbiddenFeatureLayers = setOf("data", "service")

        private const val APP_DATA_PACKAGE_PREFIX = "com.xymusic.app.data."
        private const val APP_CORE_PACKAGE_PREFIX = "com.xymusic.app.core."

        /**
         * feature presentation 只允许 core.common 工具（合法例外）、
         * core:model 与 core:ui 契约（含 app 内 core.ui 共享 UI）；
         * 其余 app 内 core.*（data/database/network/paging/performance/security/session/sync）
         * 均为基础设施，fail-closed。
         */
        private val allowedFeaturePresentationCorePackages =
            setOf(
                "common",
                "model",
                "ui",
            )

        private val forbiddenFrameworkPrefixes =
            listOf(
                "okhttp3.",
                "retrofit2.",
            )

        private const val APP_SHELL_PACKAGE = "com.xymusic.app.app"

        /**
         * App UI 层允许的 app 内 core 契约：
         * - core.common：共享协程/异常工具（用户认可的合法例外）；
         * - core.model：纯 Kotlin 共享模型；
         * - core.ui：共享 UI 组件与 UI 契约。
         * 其余 core.* 一律视为基础设施（fail-closed），新增契约必须显式审核。
         */
        private val allowedAppUiCorePrefixes =
            listOf(
                "com.xymusic.app.core.common.",
                "com.xymusic.app.core.model.",
                "com.xymusic.app.core.ui.",
            )

        /**
         * 组合根包按目录整体排除：它们必须 import 基础设施完成装配，不属于表现层。
         * - app.di：Hilt 绑定模块；
         * - app.session：会话实现（token vault/network/database）。
         */
        private val compositionRootPackages =
            setOf(
                "com.xymusic.app.app.di",
                "com.xymusic.app.app.session",
            )

        /**
         * App shell 根包（com.xymusic.app.app）本身是组合根的一部分，精确列出其跨层依赖：
         * - 会话契约：shell 渲染会话状态、触发恢复与失效；
         * - HTTP 客户端 qualifier 与数据库：ServerCacheCleaner 清理旧 server 数据；
         * - ServerRuntimeCoordinator/SessionMutationCoordinator：ServerSwitchCoordinator 切换编排。
         * 新增条目必须在此说明原因。
         */
        private val shellCompositionAllowedImports =
            setOf(
                "com.xymusic.app.core.database.XyMusicDatabase",
                "com.xymusic.app.core.network.ApiHttpClient",
                "com.xymusic.app.core.network.AuthHttpClient",
                "com.xymusic.app.core.network.MediaHttpClient",
                "com.xymusic.app.core.network.ServerRuntimeCoordinator",
                "com.xymusic.app.core.session.AppSessionProvider",
                "com.xymusic.app.core.session.AppSessionState",
                "com.xymusic.app.core.session.SessionInvalidator",
                "com.xymusic.app.core.session.SessionMutationCoordinator",
            )

        private val outputPortSuffixes = listOf("Repository", "Store")

        private val projectRoot: Path = findProjectRoot()
        private val featureSourceRoot: Path =
            projectRoot.resolve(Paths.get("app", "src", "main", "java", "com", "xymusic", "app", "feature"))
        private val appUiLayerRoots =
            listOf(
                projectRoot.resolve(Paths.get("app", "src", "main", "java", "com", "xymusic", "app", "app")),
                projectRoot.resolve(Paths.get("app", "src", "main", "java", "com", "xymusic", "app", "ui")),
                projectRoot.resolve(Paths.get("app", "src", "main", "java", "com", "xymusic", "app", "core", "ui")),
            )

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
