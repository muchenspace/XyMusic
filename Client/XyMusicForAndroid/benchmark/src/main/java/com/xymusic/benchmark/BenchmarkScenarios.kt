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

private const val SERVER_HOST_TAG = "server_config_host"
private const val SERVER_PORT_TAG = "server_config_port"
private const val SERVER_SAVE_TAG = "server_config_save"
private const val AUTH_ENTRY_TAG = "auth_entry_actions"
private const val AUTH_SIGN_IN_TAG = "auth_entry_sign_in"
private const val AUTH_REGISTER_TAG = "auth_entry_register"
private const val AUTH_LOGIN_USERNAME_TAG = "auth_username"
private const val AUTH_SWITCH_TO_REGISTER_TAG = "auth_switch_to_register"
private const val AUTH_REGISTER_CONFIRM_PASSWORD_TAG = "auth_confirm_password"
private const val HOME_SEARCH_TAG = "home_search"
private const val SEARCH_INPUT_TAG = "search_input"
private const val NAVIGATION_HOME_TAG = "main_navigation_home"
private const val NAVIGATION_MINE_TAG = "main_navigation_mine"
private const val MINE_SETTINGS_TAG = "mine_settings"
private const val SETTINGS_ROOT_TAG = "settings_root"
private const val PLAYER_MINI_BAR_TAG = "player_mini_bar"
private const val PLAYER_TOP_BAR_TAG = "player_top_bar"
private const val PLAYER_CONTENT_PAGER_TAG = "player_content_pager"

private const val BENCHMARK_SERVER_HOST = "127.0.0.1"
private const val BENCHMARK_SERVER_PORT = "1"

internal fun MacrobenchmarkScope.startActivityAndWaitForInteractiveContent() {
    startActivityAndWait()
    device.waitForInteractiveContent()
}

internal fun MacrobenchmarkScope.runBaselineProfileJourney() {
    startActivityAndWaitForInteractiveContent()

    if (device.hasAppResource(SERVER_HOST_TAG)) {
        device.configureBenchmarkServer()
        device.waitForInteractiveContent()
    }

    when {
        device.hasAppResource(HOME_SEARCH_TAG) -> device.exerciseSignedInJourney()
        device.hasAppResource(AUTH_ENTRY_TAG) -> device.exerciseSignedOutJourney()
    }
}

private fun UiDevice.configureBenchmarkServer() {
    val fields =
        listOf(
            awaitAppObject(appResourceIdSelector(SERVER_HOST_TAG)),
            awaitAppObject(appResourceIdSelector(SERVER_PORT_TAG)),
        ).sortedBy { field -> field.visibleBounds.top }

    fields[0].text = BENCHMARK_SERVER_HOST
    fields[1].text = BENCHMARK_SERVER_PORT
    requireAppResource(SERVER_SAVE_TAG).clickThroughClickableAncestor()
    check(wait(Until.gone(appResourceIdSelector(SERVER_HOST_TAG)), UI_TIMEOUT_MILLIS) == true) {
        "Server setup did not finish within ${UI_TIMEOUT_MILLIS}ms"
    }
}

private fun UiDevice.exerciseSignedOutJourney() {
    requireAppResource(AUTH_SIGN_IN_TAG).clickThroughClickableAncestor()
    awaitAppResource(AUTH_LOGIN_USERNAME_TAG)
    requireAppResource(AUTH_SWITCH_TO_REGISTER_TAG).clickThroughClickableAncestor()
    awaitAppResource(AUTH_REGISTER_CONFIRM_PASSWORD_TAG)
    navigateBackAndAwaitGone(appResourceIdSelector(AUTH_REGISTER_CONFIRM_PASSWORD_TAG))
    awaitAppResource(AUTH_LOGIN_USERNAME_TAG)
    navigateBackAndAwaitGone(appResourceIdSelector(AUTH_LOGIN_USERNAME_TAG))

    requireAppResource(AUTH_REGISTER_TAG).clickThroughClickableAncestor()
    awaitAppResource(AUTH_REGISTER_CONFIRM_PASSWORD_TAG)
    navigateBackAndAwaitGone(appResourceIdSelector(AUTH_REGISTER_CONFIRM_PASSWORD_TAG))
    check(hasAppResource(AUTH_ENTRY_TAG)) { "Auth entry screen was not restored after profile journey" }
}

private fun UiDevice.exerciseSignedInJourney() {
    findObject(appScrollableSelector())?.scroll(Direction.DOWN, 0.6f)
    findObject(appScrollableSelector())?.scroll(Direction.UP, 0.6f)
    awaitAppResource(HOME_SEARCH_TAG)

    requireAppResource(HOME_SEARCH_TAG).clickThroughClickableAncestor()
    awaitAppResource(SEARCH_INPUT_TAG)
    navigateBackAndAwaitGone(appResourceIdSelector(SEARCH_INPUT_TAG))
    awaitAppResource(HOME_SEARCH_TAG)

    val mineDestination = findAppObject(appResourceIdSelector(NAVIGATION_MINE_TAG)) ?: return
    mineDestination.clickThroughClickableAncestor()

    val settingsAction = awaitAppObject(appResourceIdSelector(MINE_SETTINGS_TAG))
    settingsAction.clickThroughClickableAncestor()
    awaitAppResource(SETTINGS_ROOT_TAG)
    navigateBackAndAwaitGone(appResourceIdSelector(SETTINGS_ROOT_TAG))
    awaitAppObject(appResourceIdSelector(MINE_SETTINGS_TAG))

    val homeDestination = findAppObject(appResourceIdSelector(NAVIGATION_HOME_TAG)) ?: return
    homeDestination.clickThroughClickableAncestor()
    awaitAppResource(HOME_SEARCH_TAG)

    val miniPlayer = findAppObject(appResourceIdSelector(PLAYER_MINI_BAR_TAG)) ?: return
    miniPlayer.clickThroughClickableAncestor()
    awaitAppResource(PLAYER_TOP_BAR_TAG)
    awaitAppResource(PLAYER_CONTENT_PAGER_TAG).swipe(Direction.LEFT, 0.72f)
    waitForIdle()
    awaitAppResource(PLAYER_CONTENT_PAGER_TAG).swipe(Direction.RIGHT, 0.72f)
    waitForIdle()
    navigateBackAndAwaitGone(appResourceIdSelector(PLAYER_TOP_BAR_TAG))
    awaitAppResource(HOME_SEARCH_TAG)
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

private fun UiDevice.hasAppResource(tag: String): Boolean = hasObject(appResourceIdSelector(tag))

private fun UiDevice.requireAppResource(tag: String): UiObject2 = awaitAppResource(tag)

private fun UiDevice.awaitAppResource(tag: String): UiObject2 = awaitAppObject(appResourceIdSelector(tag))

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

private fun appScrollableSelector(): BySelector = By.pkg(PACKAGE_NAME).scrollable(true).enabled(true)

private fun appResourceIdSelector(tag: String): BySelector = By.res(tag)
