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
  });

  it("routes the overlay to its lightweight entry instead of the main application", () => {
    const mainSource = readFileSync(path.join(projectRoot, "src/main.ts"), "utf8");

    expect(mainSource).toContain('get("window") === "desktop-lyrics"');
    expect(mainSource).toContain('import("./desktop-lyrics")');
    expect(mainSource.indexOf('import("./desktop-lyrics")')).toBeLessThan(mainSource.indexOf('import("./App.vue")'));
  });
});

function readJson(relativePath: string): unknown {
  return JSON.parse(readFileSync(path.join(projectRoot, relativePath), "utf8"));
}
