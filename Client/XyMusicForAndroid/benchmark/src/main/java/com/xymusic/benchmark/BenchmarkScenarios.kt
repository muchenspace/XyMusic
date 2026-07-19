package com.xymusic.benchmark

import androidx.benchmark.macro.MacrobenchmarkScope
import androidx.test.uiautomator.By
import androidx.test.uiautomator.BySelector
import androidx.test.uiautomator.Direction
import androidx.test.uiautomator.UiDevice
import androidx.test.uiautomator.UiObject2
import androidx.test.uiautomator.Until

internal const val PACKAGE_NAME = "com.xymusic.app"

private const val UI_TIMEOUT_MILLIS = 15_000L

private const val SERVER_SETUP_TITLE = "配置服务端"
private const val SERVER_SAVE_ACTION = "保存并继续"
private const val AUTH_SIGN_IN_ACTION = "登录"
private const val AUTH_REGISTER_ACTION = "创建账号"
private const val AUTH_SIGN_IN_TITLE = "欢迎回来"
private const val AUTH_REGISTER_TITLE = "创建你的音乐空间"
private const val HOME_SEARCH_HINT = "搜索歌曲、专辑或音乐人"
private const val SEARCH_TITLE = "搜索"
private const val NAVIGATION_HOME = "首页"
private const val NAVIGATION_MINE = "我的"
private const val SETTINGS_TITLE = "设置"

private const val BENCHMARK_SERVER_HOST = "127.0.0.1"
private const val BENCHMARK_SERVER_PORT = "1"

internal fun MacrobenchmarkScope.startActivityAndWaitForInteractiveContent() {
    startActivityAndWait()
    device.waitForInteractiveContent()
}

internal fun MacrobenchmarkScope.runBaselineProfileJourney() {
    startActivityAndWaitForInteractiveContent()

    if (device.hasAppText(SERVER_SETUP_TITLE)) {
        device.configureBenchmarkServer()
        device.waitForInteractiveContent()
    }

    when {
        device.hasAppText(HOME_SEARCH_HINT) -> device.exerciseSignedInJourney()
        device.hasAuthEntryActions() -> device.exerciseSignedOutJourney()
    }
}

private fun UiDevice.configureBenchmarkServer() {
    val fields =
        wait(Until.findObjects(appEditTextSelector()), UI_TIMEOUT_MILLIS)
            .orEmpty()
            .sortedBy { field -> field.visibleBounds.top }
    check(fields.size >= 2) {
        "Server setup did not expose both endpoint fields; found ${fields.size}"
    }

    fields[0].text = BENCHMARK_SERVER_HOST
    fields[1].text = BENCHMARK_SERVER_PORT
    requireAppText(SERVER_SAVE_ACTION).clickThroughClickableAncestor()
    check(wait(Until.gone(appTextSelector(SERVER_SETUP_TITLE)), UI_TIMEOUT_MILLIS) == true) {
        "Server setup did not finish within ${UI_TIMEOUT_MILLIS}ms"
    }
}

private fun UiDevice.exerciseSignedOutJourney() {
    requireAppText(AUTH_SIGN_IN_ACTION).clickThroughClickableAncestor()
    awaitAppText(AUTH_SIGN_IN_TITLE)
    navigateBackAndAwaitGone(appTextSelector(AUTH_SIGN_IN_TITLE))

    requireAppText(AUTH_REGISTER_ACTION).clickThroughClickableAncestor()
    awaitAppText(AUTH_REGISTER_TITLE)
    navigateBackAndAwaitGone(appTextSelector(AUTH_REGISTER_TITLE))
    check(hasAuthEntryActions()) { "Auth entry screen was not restored after profile journey" }
}

private fun UiDevice.exerciseSignedInJourney() {
    findObject(appScrollableSelector())?.scroll(Direction.DOWN, 0.6f)
    findObject(appScrollableSelector())?.scroll(Direction.UP, 0.6f)
    awaitAppText(HOME_SEARCH_HINT)

    requireAppText(HOME_SEARCH_HINT).clickThroughClickableAncestor()
    awaitAppText(SEARCH_TITLE)
    navigateBackAndAwaitGone(appTextSelector(SEARCH_TITLE))
    awaitAppText(HOME_SEARCH_HINT)

    val mineDestination =
        findAppObject(appDescriptionSelector(NAVIGATION_MINE))
            ?: findAppObject(appTextSelector(NAVIGATION_MINE))
            ?: return
    mineDestination.clickThroughClickableAncestor()

    val settingsAction = awaitAppObject(appDescriptionSelector(SETTINGS_TITLE))
    settingsAction.clickThroughClickableAncestor()
    awaitAppText(SETTINGS_TITLE)
    navigateBackAndAwaitGone(appTextSelector(SETTINGS_TITLE))
    awaitAppObject(appDescriptionSelector(SETTINGS_TITLE))

    val homeDestination =
        findAppObject(appDescriptionSelector(NAVIGATION_HOME))
            ?: findAppObject(appTextSelector(NAVIGATION_HOME))
            ?: return
    homeDestination.clickThroughClickableAncestor()
    awaitAppText(HOME_SEARCH_HINT)
}

private fun UiDevice.waitForInteractiveContent() {
    check(wait(Until.hasObject(appInteractiveSelector()), UI_TIMEOUT_MILLIS) == true) {
        "XyMusic did not expose enabled interactive content within ${UI_TIMEOUT_MILLIS}ms; " +
            "currentPackage=$currentPackageName"
    }
}

private fun UiDevice.navigateBackAndAwaitGone(selector: BySelector) {
    pressBack()
    check(wait(Until.gone(selector), UI_TIMEOUT_MILLIS) == true) {
        "Previous page did not leave the hierarchy within ${UI_TIMEOUT_MILLIS}ms"
    }
    waitForInteractiveContent()
}

private fun UiDevice.hasAuthEntryActions(): Boolean =
    hasAppText(AUTH_SIGN_IN_ACTION) && hasAppText(AUTH_REGISTER_ACTION)

private fun UiDevice.hasAppText(text: String): Boolean = hasObject(appTextSelector(text))

private fun UiDevice.requireAppText(text: String): UiObject2 = awaitAppObject(appTextSelector(text))

private fun UiDevice.awaitAppText(text: String): UiObject2 = awaitAppObject(appTextSelector(text))

private fun UiDevice.awaitAppObject(selector: BySelector): UiObject2 =
    checkNotNull(wait(Until.findObject(selector), UI_TIMEOUT_MILLIS)) {
        "Expected XyMusic UI object was not available within ${UI_TIMEOUT_MILLIS}ms: $selector"
    }

private fun UiDevice.findAppObject(selector: BySelector): UiObject2? = findObject(selector)

private fun UiObject2.clickThroughClickableAncestor() {
    var target: UiObject2? = this
    while (target != null && (!target.isClickable || !target.isEnabled)) {
        target = target.parent
    }
    (target ?: this).click()
}

private fun appInteractiveSelector(): BySelector = By.pkg(PACKAGE_NAME).clickable(true).enabled(true)

private fun appEditTextSelector(): BySelector = By.pkg(PACKAGE_NAME).clazz("android.widget.EditText").enabled(true)

private fun appScrollableSelector(): BySelector = By.pkg(PACKAGE_NAME).scrollable(true).enabled(true)

private fun appTextSelector(text: String): BySelector = By.pkg(PACKAGE_NAME).text(text)

private fun appDescriptionSelector(description: String): BySelector = By.pkg(PACKAGE_NAME).desc(description)
