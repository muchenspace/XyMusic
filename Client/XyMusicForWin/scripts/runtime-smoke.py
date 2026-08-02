from pathlib import Path

from playwright.sync_api import sync_playwright


APP_URL = "http://127.0.0.1:5173"
ARTIFACTS = Path("artifacts")


def check_viewport(page, name, width, height):
    errors = []
    page.set_viewport_size({"width": width, "height": height})
    page.on("pageerror", lambda error: errors.append(f"pageerror: {error}"))
    page.on(
        "console",
        lambda message: errors.append(f"console: {message.text}")
        if message.type == "error"
        else None,
    )
    page.goto(APP_URL, wait_until="networkidle")
    page.locator(".login-card").wait_for(state="visible")
    brand_mark = page.locator(".login-brand-mark")
    brand = brand_mark.evaluate(
        """image => ({
            complete: image.complete,
            src: image.currentSrc,
            naturalWidth: image.naturalWidth,
            naturalHeight: image.naturalHeight,
        })"""
    )
    if not brand["complete"] or not brand["src"].endswith(".webp"):
        raise AssertionError(f"{name} viewport did not load the WebP brand mark")
    if brand["naturalWidth"] != 512 or brand["naturalHeight"] != 512:
        raise AssertionError(f"{name} viewport loaded an unexpected brand-mark size: {brand}")
    overflow = page.evaluate("document.documentElement.scrollWidth > document.documentElement.clientWidth")
    if overflow:
        raise AssertionError(f"{name} viewport has horizontal overflow")
    page.screenshot(path=str(ARTIFACTS / f"runtime-smoke-{name}.png"), full_page=True)
    if errors:
        raise AssertionError("; ".join(errors))


def main():
    ARTIFACTS.mkdir(exist_ok=True)
    with sync_playwright() as playwright:
        browser = playwright.chromium.launch(headless=True)
        try:
            check_viewport(browser.new_page(), "desktop", 1440, 900)
            check_viewport(browser.new_page(), "mobile", 390, 844)
        finally:
            browser.close()


if __name__ == "__main__":
    main()
