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

  it("routes main-window mode changes through guarded native commands", () => {
    const source = readFileSync(
      path.join(projectRoot, "src/infrastructure/windows/TauriDesktopWindow.ts"),
      "utf8",
    );
    const nativeSource = readFileSync(path.join(projectRoot, "src-tauri/src/window.rs"), "utf8");
    const capabilitySource = readFileSync(path.join(projectRoot, "src-tauri/capabilities/default.json"), "utf8");

    expect(source).toContain('invoke("toggle_main_window_maximize")');
    expect(source).toContain('invoke("toggle_main_window_fullscreen")');
    expect(source).not.toContain("setFullscreen(");
    expect(source).not.toContain("appWindow.toggleMaximize()");
    expect(nativeSource).toContain("set_resizable(false)");
    expect(nativeSource).toContain("set_resizable(true)");
    expect(nativeSource).toContain("run_on_main_window_thread");
    expect(nativeSource).toContain("window.unmaximize()");
    expect(nativeSource).toContain("TrueFullscreenState");
    expect(nativeSource).toContain("true fullscreen is unavailable in mini mode");
    expect(capabilitySource).not.toContain("allow-set-fullscreen");
    expect(capabilitySource).not.toContain("allow-toggle-maximize");
  });


  it("does not expose a second fullscreen exit control in the player", () => {
    const source = readFileSync(
      path.join(projectRoot, "src/presentation/components/PlayerBar.vue"),
      "utf8",
    );

    expect(source).not.toContain("Maximize2");
    expect(source).not.toContain("toggleFullscreen");
  });

  it("removes every main-window drag region in true fullscreen", () => {
    const appSource = readFileSync(path.join(projectRoot, "src/App.vue"), "utf8");
    const sidebarSource = readFileSync(path.join(projectRoot, "src/presentation/components/AppSidebar.vue"), "utf8");
    const topBarSource = readFileSync(path.join(projectRoot, "src/presentation/components/TopBar.vue"), "utf8");
    const lyricsSource = readFileSync(path.join(projectRoot, "src/presentation/components/LyricsView.vue"), "utf8");
    const miniSource = readFileSync(path.join(projectRoot, "src/presentation/components/MiniPlayer.vue"), "utf8");

    expect(appSource).toContain(":fullscreen=\"windowFullscreen\"");
    expect(sidebarSource).toContain("v-if=\"props.fullscreen\"");
    expect(topBarSource).toContain("v-if=\"props.fullscreen\"");
    expect(lyricsSource).toContain("v-if=\"props.fullscreen\"");
    expect(miniSource).toContain("v-if=\"fullscreen\"");
  });
});
