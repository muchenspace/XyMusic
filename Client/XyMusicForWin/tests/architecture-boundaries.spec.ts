import { readdirSync, readFileSync } from "node:fs";
import path from "node:path";
import { describe, expect, it } from "vitest";

const sourceRoot = path.resolve(import.meta.dirname, "../src");
const layerRoots = ["domain", "application", "infrastructure", "presentation", "desktop-lyrics"] as const;
const forbiddenImports: Record<(typeof layerRoots)[number], ReadonlySet<string>> = {
  domain: new Set(["application", "infrastructure", "presentation", "desktop-lyrics"]),
  application: new Set(["infrastructure", "presentation", "desktop-lyrics"]),
  infrastructure: new Set(["presentation", "desktop-lyrics"]),
  presentation: new Set(["infrastructure"]),
  "desktop-lyrics": new Set(["infrastructure", "presentation"]),
};

describe("source architecture boundaries", () => {
  it("keeps dependencies pointing inward", () => {
    const violations: string[] = [];
    for (const sourceLayer of layerRoots) {
      for (const file of sourceFiles(path.join(sourceRoot, sourceLayer))) {
        for (const specifier of relativeImports(readFileSync(file, "utf8"))) {
          const target = path.resolve(path.dirname(file), specifier);
          const targetLayer = path.relative(sourceRoot, target).split(path.sep)[0] ?? "";
          if (forbiddenImports[sourceLayer].has(targetLayer)) {
            violations.push(`${relative(file)} imports ${targetLayer} through ${specifier}`);
          }
        }
      }
    }

    expect(violations).toEqual([]);
  });

  it("keeps browser persistence and network IO in infrastructure", () => {
    const violations: string[] = [];
    for (const layer of ["domain", "application", "presentation", "desktop-lyrics"] as const) {
      for (const file of sourceFiles(path.join(sourceRoot, layer))) {
        const source = readFileSync(file, "utf8");
        if (/\b(?:localStorage|sessionStorage)\b|\bfetch\s*\(/u.test(source)) violations.push(relative(file));
      }
    }

    expect(violations).toEqual([]);
  });

  it("keeps browser exception constructors out of inner layers", () => {
    const violations: string[] = [];
    for (const layer of ["domain", "application"] as const) {
      for (const file of sourceFiles(path.join(sourceRoot, layer))) {
        if (/\bDOMException\b/u.test(readFileSync(file, "utf8"))) violations.push(relative(file));
      }
    }

    expect(violations).toEqual([]);
  });

  it("keeps browser file selection out of application contracts", () => {
    const violations: string[] = [];
    for (const layer of ["domain", "application"] as const) {
      for (const file of sourceFiles(path.join(sourceRoot, layer))) {
        if (/\b(?:Blob|File|FileList|HTMLInputElement)\b/u.test(readFileSync(file, "utf8"))) violations.push(relative(file));
      }
    }

    expect(violations).toEqual([]);
  });

  it("keeps playback orchestration out of the player store", () => {
    const playerStore = readFileSync(path.join(sourceRoot, "presentation/stores/playerStore.ts"), "utf8");

    expect(playerStore).not.toMatch(/\b(?:AudioPlayer|PlaybackUseCases|PlaybackStateUseCases|PlaybackGrantCache|PlayerPreferences|Notifier|Diagnostics|DesktopIntegration|DesktopMediaSessionCoordinator|PlaybackDesktopIntegration|onMediaAction|updateMediaMetadata|updateMediaPlayback)\b/u);
    expect(playerStore).not.toMatch(/\b(?:window|crypto)\b|services\.(?:audio|playback|playbackGrants|playbackPersistence|playbackPreferences|desktopPlayback|notifier|diagnostics)\b/u);
    expect(playerStore).not.toMatch(/\bsession\.dispose\s*\(/u);
  });

  it("keeps playback snapshots application-owned and favorite mutations in the projection", () => {
    const playbackPort = readFileSync(path.join(sourceRoot, "application/ports/PlaybackSession.ts"), "utf8");
    const playerStore = readFileSync(path.join(sourceRoot, "presentation/stores/playerStore.ts"), "utf8");
    const musicActions = readFileSync(path.join(sourceRoot, "presentation/composables/useMusicActions.ts"), "utf8");

    expect(playbackPort).toMatch(/export type PlaybackQueue = readonly ReadonlyTrack\[\]/u);
    expect(playbackPort).toMatch(/readonly queue: PlaybackQueue/u);
    expect(playerStore).toContain("favoriteOverrides");
    expect(playerStore).toContain("function setFavorite");
    expect(musicActions).not.toMatch(/\btrack\.liked\s*=/u);
  });

  it("keeps desktop-lyrics orchestration out of the Pinia store", () => {
    const desktopLyricsStore = readFileSync(path.join(sourceRoot, "presentation/stores/desktopLyricsStore.ts"), "utf8");

    expect(desktopLyricsStore).toContain("desktopLyricsController");
    expect(desktopLyricsStore).not.toMatch(/\b(?:DesktopLyrics|UserInterfacePreferences|SerialTaskQueue)\b/u);
    expect(desktopLyricsStore).not.toMatch(/\bwindow\b|services\.(?:desktopLyrics|uiPreferences)\b/u);
    expect(desktopLyricsStore).not.toMatch(/\bcontroller\.dispose\s*\(/u);
  });

  it("keeps native window observation out of presentation", () => {
    const violations: string[] = [];
    for (const layer of ["presentation", "desktop-lyrics"] as const) {
      for (const file of sourceFiles(path.join(sourceRoot, layer))) {
        const source = readFileSync(file, "utf8");
        if (/\bDesktopWindow\b|\.desktopWindow\b|\bonResized\s*\(|\.(?:isMaximized|isFullscreen)\s*\(/u.test(source)) {
          violations.push(relative(file));
        }
      }
    }

    expect(violations).toEqual([]);
  });

  it("keeps Tauri APIs behind infrastructure adapters", () => {
    const violations: string[] = [];
    for (const layer of ["domain", "application", "presentation", "desktop-lyrics"] as const) {
      for (const file of sourceFiles(path.join(sourceRoot, layer))) {
        const source = readFileSync(file, "utf8");
        if (/from\s*["']@tauri-apps\//u.test(source) || /import\s*\(\s*["']@tauri-apps\//u.test(source)) {
          violations.push(relative(file));
        }
      }
    }

    expect(violations).toEqual([]);
  });

  it("keeps runtime detection and clipboard access behind presentation-facing ports", () => {
    const violations: string[] = [];
    for (const layer of ["presentation", "desktop-lyrics"] as const) {
      for (const file of sourceFiles(path.join(sourceRoot, layer))) {
        const source = readFileSync(file, "utf8");
        if (/\b__TAURI_INTERNALS__\b|\bnavigator\.(?:clipboard|userAgent)\b/u.test(source)) {
          violations.push(relative(file));
        }
      }
    }

    expect(violations).toEqual([]);
  });

  it("exposes the playback session rather than its workflow collaborators", () => {
    const services = readFileSync(path.join(sourceRoot, "application/services.ts"), "utf8");

    expect(services).toMatch(/\bplaybackSession:\s*PlaybackSession\b/u);
    expect(services).not.toMatch(/\b(?:audio|playbackGrants|playbackPersistence|playbackPreferences|desktopPlayback|notifier):\b/u);
    expect(services).toMatch(/\bdesktopLyricsController:\s*DesktopLyricsController\b/u);
    expect(services).not.toMatch(/\bdesktopLyrics:\s*DesktopLyrics\b/u);
    expect(services).toMatch(/\bdesktopWindowController:\s*DesktopWindowController\b/u);
    expect(services).toContain('from "./ports/DesktopWindowController"');
    expect(services).not.toMatch(/\bdesktopWindow:\s*DesktopWindow\b/u);
  });

  it("disposes presentation stores before application services at the composition root", () => {
    const main = readFileSync(path.join(sourceRoot, "main.ts"), "utf8");

    expect(main).toMatch(/\bdisposePinia\(pinia\);/u);
    expect(main.indexOf("disposePinia(pinia)")).toBeLessThan(main.indexOf("services.playbackSession.dispose()"));
  });
});

function sourceFiles(directory: string): string[] {
  return readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
    const candidate = path.join(directory, entry.name);
    if (entry.isDirectory()) return sourceFiles(candidate);
    return entry.isFile() && (entry.name.endsWith(".ts") || entry.name.endsWith(".vue")) ? [candidate] : [];
  });
}

function relativeImports(source: string): string[] {
  const imports: string[] = [];
  const pattern = /(?:from\s*|import\s*)["'](\.{1,2}\/[^"']+)["']/gu;
  for (const match of source.matchAll(pattern)) imports.push(match[1]!);
  return imports;
}

function relative(file: string): string {
  return path.relative(sourceRoot, file).replaceAll(path.sep, "/");
}
