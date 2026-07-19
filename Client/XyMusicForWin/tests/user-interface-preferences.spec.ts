import { describe, expect, it } from "vitest";
import { LocalUserInterfacePreferences } from "../src/infrastructure/preferences/LocalUserInterfacePreferences";

describe("local user interface preferences", () => {
  it("normalizes malformed theme, lyrics, and offset values", () => {
    const storage = new MemoryStorage({
      "xy-music-theme": "sepia",
      "xymusic.desktop.lyrics.font-scale": "8",
      "xymusic.desktop.lyrics.translation": "sometimes",
      "xymusic.desktop.lyrics.word-lyrics-enabled": "sometimes",
      "xymusic.playback-lyrics.dark.text-color": "white",
      "xymusic.playback-lyrics.dark.highlight-color": "#xyzxyz",
      "xymusic.playback-lyrics.light.text-color": "#12345",
      "xymusic.playback-lyrics.light.highlight-color": "blue",
      "xymusic.desktop.lyrics.offsets.v1": JSON.stringify({ valid: 2.34, high: 99, invalid: "1" }),
      "xymusic.desktop-lyrics.visible": "sometimes",
      "xymusic.desktop-lyrics.locked": "sometimes",
      "xymusic.desktop-lyrics.fullscreen-behavior": "auto",
      "xymusic.desktop-lyrics.font-scale": "9",
      "xymusic.desktop-lyrics.text-color": "white",
      "xymusic.desktop-lyrics.highlight-color": "#xyzxyz",
      "xymusic.desktop-lyrics.word-lyrics-enabled": "sometimes",
    });
    const preferences = new LocalUserInterfacePreferences(storage);

    expect(preferences.readTheme()).toBe("system");
    expect(preferences.readLyrics()).toEqual({
      fontScale: 1,
      showTranslation: true,
      wordLyricsEnabled: true,
      colors: {
        dark: { textColor: "#8e98a3", highlightColor: "#d7e6f3" },
        light: { textColor: "#626a74", highlightColor: "#1b4269" },
      },
    });
    expect(preferences.readDesktopLyrics()).toEqual({
      visible: false,
      locked: false,
      fullscreenBehavior: "show",
      fontScale: 1,
      textColor: "#f4f5f7",
      highlightColor: "#cf9437",
      wordLyricsEnabled: true,
    });
    expect(preferences.readLyricsOffset("valid")).toBe(2.3);
    expect(preferences.readLyricsOffset("high")).toBe(5);
    expect(preferences.readLyricsOffset("invalid")).toBe(0);
  });

  it("writes normalized values, removes zero offsets, and bounds retained offsets", () => {
    const storage = new MemoryStorage();
    const preferences = new LocalUserInterfacePreferences(storage);
    preferences.writeTheme("light");
    preferences.writeLyricsFontScale(1.2);
    preferences.writeLyricsTranslation(false);
    preferences.writeLyricsWordLyricsEnabled(false);
    preferences.writeLyricsTextColor("dark", "#ABCDEF");
    preferences.writeLyricsHighlightColor("dark", "#FEDCBA");
    preferences.writeLyricsTextColor("light", "#123ABC");
    preferences.writeLyricsHighlightColor("light", "#654DEF");
    preferences.writeDesktopLyricsVisible(true);
    preferences.writeDesktopLyricsLocked(true);
    preferences.writeDesktopLyricsFullscreenBehavior("hide");
    preferences.writeDesktopLyricsFontScale(1.35);
    preferences.writeDesktopLyricsTextColor("#ABCDEF");
    preferences.writeDesktopLyricsHighlightColor("#123456");
    preferences.writeDesktopLyricsWordLyricsEnabled(false);
    for (let index = 0; index < 105; index += 1) preferences.writeLyricsOffset(`track-${index}`, index / 10);
    preferences.writeLyricsOffset("track-104", 0);

    expect(storage.values).toMatchObject({
      "xy-music-theme": "light",
      "xymusic.desktop.lyrics.font-scale": "1.2",
      "xymusic.desktop.lyrics.translation": "false",
      "xymusic.desktop.lyrics.word-lyrics-enabled": "false",
      "xymusic.playback-lyrics.dark.text-color": "#abcdef",
      "xymusic.playback-lyrics.dark.highlight-color": "#fedcba",
      "xymusic.playback-lyrics.light.text-color": "#123abc",
      "xymusic.playback-lyrics.light.highlight-color": "#654def",
      "xymusic.desktop-lyrics.visible": "true",
      "xymusic.desktop-lyrics.locked": "true",
      "xymusic.desktop-lyrics.fullscreen-behavior": "hide",
      "xymusic.desktop-lyrics.font-scale": "1.35",
      "xymusic.desktop-lyrics.text-color": "#abcdef",
      "xymusic.desktop-lyrics.highlight-color": "#123456",
      "xymusic.desktop-lyrics.word-lyrics-enabled": "false",
    });
    const offsets = JSON.parse(storage.values["xymusic.desktop.lyrics.offsets.v1"] ?? "{}") as Record<string, number>;
    expect(Object.keys(offsets)).toHaveLength(99);
    expect(offsets["track-0"]).toBeUndefined();
    expect(offsets["track-104"]).toBeUndefined();
    expect(Math.max(...Object.values(offsets))).toBe(5);
  });

  it("tolerates unavailable storage for every operation", () => {
    const preferences = new LocalUserInterfacePreferences({
      getItem: () => { throw new DOMException("denied"); },
      setItem: () => { throw new DOMException("denied"); },
      removeItem: () => { throw new DOMException("denied"); },
    });

    expect(preferences.readTheme()).toBe("system");
    expect(preferences.readLyrics()).toEqual({
      fontScale: 1,
      showTranslation: true,
      wordLyricsEnabled: true,
      colors: {
        dark: { textColor: "#8e98a3", highlightColor: "#d7e6f3" },
        light: { textColor: "#626a74", highlightColor: "#1b4269" },
      },
    });
    expect(preferences.readDesktopLyrics()).toEqual({
      visible: false,
      locked: false,
      fullscreenBehavior: "show",
      fontScale: 1,
      textColor: "#f4f5f7",
      highlightColor: "#cf9437",
      wordLyricsEnabled: true,
    });
    expect(preferences.readLyricsOffset("track")).toBe(0);
    expect(() => {
      preferences.writeTheme("dark");
      preferences.writeLyricsFontScale(1);
      preferences.writeLyricsTranslation(true);
      preferences.writeLyricsWordLyricsEnabled(true);
      preferences.writeLyricsTextColor("dark", "#8e98a3");
      preferences.writeLyricsHighlightColor("dark", "#d7e6f3");
      preferences.writeLyricsTextColor("light", "#626a74");
      preferences.writeLyricsHighlightColor("light", "#1b4269");
      preferences.writeDesktopLyricsVisible(true);
      preferences.writeDesktopLyricsLocked(true);
      preferences.writeDesktopLyricsFullscreenBehavior("hide");
      preferences.writeDesktopLyricsFontScale(1.1);
      preferences.writeDesktopLyricsTextColor("#ffffff");
      preferences.writeDesktopLyricsHighlightColor("#cf9437");
      preferences.writeDesktopLyricsWordLyricsEnabled(true);
      preferences.writeLyricsOffset("track", 1);
      preferences.clearLyricsOffsets();
    }).not.toThrow();
  });

  it("migrates the previous desktop lyrics defaults to the redesigned palette", () => {
    const storage = new MemoryStorage({
      "xymusic.desktop-lyrics.text-color": "#f8fafc",
      "xymusic.desktop-lyrics.highlight-color": "#8eb1d2",
    });
    const preferences = new LocalUserInterfacePreferences(storage);

    expect(preferences.readDesktopLyrics()).toMatchObject({
      textColor: "#f4f5f7",
      highlightColor: "#cf9437",
    });
    expect(storage.values).toMatchObject({
      "xymusic.desktop-lyrics.text-color": "#f4f5f7",
      "xymusic.desktop-lyrics.highlight-color": "#cf9437",
      "xymusic.desktop-lyrics.palette-version": "3",
    });

    preferences.writeDesktopLyricsHighlightColor("#8eb1d2");
    expect(preferences.readDesktopLyrics().highlightColor).toBe("#8eb1d2");
  });

  it("does not overwrite desktop lyrics colors when a migration read fails", () => {
    const values: Record<string, string> = {
      "xymusic.desktop-lyrics.text-color": "#abcdef",
      "xymusic.desktop-lyrics.highlight-color": "#123456",
    };
    const preferences = new LocalUserInterfacePreferences({
      getItem(key) {
        if (key === "xymusic.desktop-lyrics.text-color") throw new DOMException("read failed");
        return values[key] ?? null;
      },
      setItem(key, value) { values[key] = value; },
      removeItem(key) { delete values[key]; },
    });

    expect(preferences.readDesktopLyrics()).toMatchObject({
      textColor: "#f4f5f7",
      highlightColor: "#123456",
    });
    expect(values).toEqual({
      "xymusic.desktop-lyrics.text-color": "#abcdef",
      "xymusic.desktop-lyrics.highlight-color": "#123456",
    });
  });

  it("does not mark the palette migration complete when a color write fails", () => {
    const values: Record<string, string> = {
      "xymusic.desktop-lyrics.text-color": "#f8fafc",
      "xymusic.desktop-lyrics.highlight-color": "#8eb1d2",
    };
    const preferences = new LocalUserInterfacePreferences({
      getItem: (key) => values[key] ?? null,
      setItem(key, value) {
        if (key === "xymusic.desktop-lyrics.highlight-color") throw new DOMException("write failed");
        values[key] = value;
      },
      removeItem(key) { delete values[key]; },
    });

    expect(preferences.readDesktopLyrics()).toMatchObject({
      textColor: "#f4f5f7",
      highlightColor: "#cf9437",
    });
    expect(values["xymusic.desktop-lyrics.palette-version"]).toBeUndefined();
  });
});

class MemoryStorage {
  readonly values: Record<string, string>;

  constructor(initial: Record<string, string> = {}) {
    this.values = { ...initial };
  }

  getItem(key: string): string | null {
    return this.values[key] ?? null;
  }

  setItem(key: string, value: string): void {
    this.values[key] = value;
  }

  removeItem(key: string): void {
    delete this.values[key];
  }
}
