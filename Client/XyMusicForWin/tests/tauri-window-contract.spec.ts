import { readFileSync } from "node:fs";
import path from "node:path";
import { describe, expect, it } from "vitest";

const projectRoot = path.resolve(import.meta.dirname, "..");

describe("Tauri window integration", () => {
  it("hides the main window from the custom close button", () => {
    const source = readFileSync(
      path.join(projectRoot, "src/infrastructure/windows/TauriDesktopWindow.ts"),
      "utf8",
    );

    expect(source).toContain('invoke("hide_main_window")');
    expect(source).not.toContain("getCurrentWindow().close()");
  });

  it("keeps the app alive in the tray until the explicit exit action", () => {
    const traySource = readFileSync(path.join(projectRoot, "src-tauri/src/tray.rs"), "utf8");
    const appSource = readFileSync(path.join(projectRoot, "src-tauri/src/lib.rs"), "utf8");

    expect(appSource).toContain("api.prevent_close()");
    expect(appSource).toContain("window.hide()");
    expect(traySource).toContain('.text(SHOW_MENU_ID, "打开 XyMusic")');
    expect(traySource).toContain('.text(EXIT_MENU_ID, "退出")');
    expect(traySource).toContain("Some(TrayMenuAction::Exit) => app.exit(0)");
  });
});
