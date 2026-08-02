from pathlib import Path

from playwright.sync_api import sync_playwright


output = Path("browser-smoke.png")
console_errors: list[str] = []
page_errors: list[str] = []

with sync_playwright() as playwright:
    browser = playwright.chromium.launch(headless=True)
    page = browser.new_page(viewport={"width": 1440, "height": 900}, device_scale_factor=1)
    page.on("console", lambda message: console_errors.append(message.text) if message.type == "error" else None)
    page.on("pageerror", lambda error: page_errors.append(str(error)))
    page.goto("http://127.0.0.1:1420", wait_until="domcontentloaded", timeout=20_000)
    page.wait_for_load_state("networkidle", timeout=20_000)
    page.screenshot(path=str(output), full_page=True)
    print(f"title={page.title()}")
    print(f"body={page.locator('body').inner_text().strip()[:500]}")
    print(f"console_errors={console_errors}")
    print(f"page_errors={page_errors}")
    browser.close()
