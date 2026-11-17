import { readFileSync } from "node:fs";
import path from "node:path";
import { describe, expect, it } from "vitest";

const projectRoot = path.resolve(import.meta.dirname, "..");

describe("desktop lyrics Tauri configuration", () => {
  it("defines an independent transparent overlay window", () => {
    const config = readJson("src-tauri/tauri.conf.json") as {
      app: { windows: Array<Record<string, unknown>> };
    };
    const lyricsWindow = config.app.windows.find((window) => window.label === "desktop-lyrics");

    expect(lyricsWindow).toMatchObject({
      url: "index.html?window=desktop-lyrics",
      transparent: true,
      decorations: false,
      alwaysOnTop: true,
      visible: false,
      skipTaskbar: true,
      shadow: false,
    });
    expect(lyricsWindow?.parent).toBeUndefined();
  });

  it("grants only the window and event capabilities used by the overlay", () => {
    const capability = readJson("src-tauri/capabilities/desktop-lyrics.json") as {
      windows: string[];
      permissions: string[];
    };

    expect(capability.windows).toEqual(["desktop-lyrics"]);
    expect(capability.permissions).toEqual(expect.arrayContaining([
      "core:event:allow-listen",
      "core:event:allow-unlisten",
      "core:event:allow-emit",
      "core:window:allow-start-dragging",
      "core:window:allow-available-monitors",
      "core:window:allow-primary-monitor",
      "core:window:allow-set-position",
      "core:window:allow-set-size",
    ]));
    for (const permission of capability.permissions) {
      expect(permission).not.toMatch(/^allow-(?:credential|hide-main-window|toggle-main-window|set-mini-mode|update-media|clear-media|.*desktop-lyrics)/u);
    }
  });

  it("enforces an explicit CSP and scopes custom Tauri commands to the main window", () => {
    const config = readJson("src-tauri/tauri.conf.json") as {
      app: { security?: { csp?: unknown } };
    };
    const csp = config.app.security?.csp;
    expect(typeof csp).toBe("string");
    expect(csp).toContain("default-src 'self'");
    expect(csp).toContain("script-src 'self'");
    expect(csp).toContain("object-src 'none'");
    expect(csp).toContain("frame-ancestors 'none'");

    const libSource = readFileSync(path.join(projectRoot, "src-tauri/src/lib.rs"), "utf8");
    const buildSource = readFileSync(path.join(projectRoot, "src-tauri/build.rs"), "utf8");
    const mainCapability = readJson("src-tauri/capabilities/default.json") as {
      windows: string[];
      permissions: string[];
    };
    const handlerBlock = libSource.match(/generate_handler!\[([\s\S]*?)\]/u)?.[1] ?? "";
    const registeredCommands = [...handlerBlock.matchAll(/::([a-z0-9_]+)/gu)].map((match) => match[1]!);

    expect(registeredCommands).toHaveLength(15);
    expect(mainCapability.windows).toEqual(["main"]);
    for (const command of registeredCommands) {
      expect(buildSource).toContain(`"${command}"`);
      expect(mainCapability.permissions).toContain(`allow-${command.replaceAll("_", "-")}`);
    }
  });

  it("routes the overlay to its lightweight entry instead of the main application", () => {
    const mainSource = readFileSync(path.join(projectRoot, "src/main.ts"), "utf8");
    const containerSource = readFileSync(path.join(projectRoot, "src/infrastructure/container.ts"), "utf8");

    expect(mainSource).toContain('get("window") === "desktop-lyrics"');
    expect(mainSource).toContain("createDesktopLyricsWindowServices");
    expect(mainSource.indexOf("createDesktopLyricsWindowServices")).toBeLessThan(mainSource.indexOf('import("./App.vue")'));
    expect(containerSource).toContain('import("../desktop-lyrics")');
  });
});

function readJson(relativePath: string): unknown {
  return JSON.parse(readFileSync(path.join(projectRoot, relativePath), "utf8"));
}
