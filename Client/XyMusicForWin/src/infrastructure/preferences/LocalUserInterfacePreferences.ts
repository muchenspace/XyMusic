import type {
  DesktopLyricsFullscreenBehavior,
  DesktopLyricsPreferencesSnapshot,
  LyricsColorScheme,
  LyricsPreferencesSnapshot,
  ThemePreference,
  UserInterfacePreferences,
} from "../../application/ports/UserInterfacePreferences";
import {
  DEFAULT_DESKTOP_LYRICS_HIGHLIGHT_COLOR,
  DEFAULT_DESKTOP_LYRICS_TEXT_COLOR,
  DEFAULT_PLAYBACK_LYRICS_COLORS,
} from "../../application/ports/UserInterfacePreferences";

type PreferenceStorage = Pick<Storage, "getItem" | "setItem" | "removeItem">;

export class LocalUserInterfacePreferences implements UserInterfacePreferences {
  constructor(private readonly storage: PreferenceStorage = localStorage) {}

  readTheme(): ThemePreference {
    const value = this.get(THEME_KEY);
    return value === "dark" || value === "light" || value === "system" ? value : "system";
  }

  writeTheme(value: ThemePreference): void {
    this.set(THEME_KEY, value);
  }

  readLyrics(): LyricsPreferencesSnapshot {
    return {
      fontScale: normalizeFontScale(Number(this.get(FONT_SCALE_KEY))),
      showTranslation: normalizeBoolean(this.get(TRANSLATION_KEY), true),
      wordLyricsEnabled: normalizeBoolean(this.get(LYRICS_WORD_LYRICS_ENABLED_KEY), true),
      colors: {
        dark: {
          textColor: normalizeColor(this.get(LYRICS_DARK_TEXT_COLOR_KEY), DEFAULT_PLAYBACK_LYRICS_COLORS.dark.textColor),
          highlightColor: normalizeColor(this.get(LYRICS_DARK_HIGHLIGHT_COLOR_KEY), DEFAULT_PLAYBACK_LYRICS_COLORS.dark.highlightColor),
        },
        light: {
          textColor: normalizeColor(this.get(LYRICS_LIGHT_TEXT_COLOR_KEY), DEFAULT_PLAYBACK_LYRICS_COLORS.light.textColor),
          highlightColor: normalizeColor(this.get(LYRICS_LIGHT_HIGHLIGHT_COLOR_KEY), DEFAULT_PLAYBACK_LYRICS_COLORS.light.highlightColor),
        },
      },
    };
  }

  writeLyricsFontScale(value: number): void {
    this.set(FONT_SCALE_KEY, String(normalizeFontScale(value)));
  }

  writeLyricsTranslation(visible: boolean): void {
    this.set(TRANSLATION_KEY, String(visible));
  }

  writeLyricsWordLyricsEnabled(enabled: boolean): void {
    this.set(LYRICS_WORD_LYRICS_ENABLED_KEY, String(enabled));
  }

  writeLyricsTextColor(scheme: LyricsColorScheme, value: string): void {
    const fallback = DEFAULT_PLAYBACK_LYRICS_COLORS[scheme].textColor;
    this.set(scheme === "dark" ? LYRICS_DARK_TEXT_COLOR_KEY : LYRICS_LIGHT_TEXT_COLOR_KEY, normalizeColor(value, fallback));
  }

  writeLyricsHighlightColor(scheme: LyricsColorScheme, value: string): void {
    const fallback = DEFAULT_PLAYBACK_LYRICS_COLORS[scheme].highlightColor;
    this.set(scheme === "dark" ? LYRICS_DARK_HIGHLIGHT_COLOR_KEY : LYRICS_LIGHT_HIGHLIGHT_COLOR_KEY, normalizeColor(value, fallback));
  }

  readDesktopLyrics(): DesktopLyricsPreferencesSnapshot {
    const colors = this.readDesktopLyricsColors();
    return {
      visible: normalizeBoolean(this.get(DESKTOP_LYRICS_VISIBLE_KEY), false),
      locked: normalizeBoolean(this.get(DESKTOP_LYRICS_LOCKED_KEY), false),
      fullscreenBehavior: normalizeFullscreenBehavior(this.get(DESKTOP_LYRICS_FULLSCREEN_BEHAVIOR_KEY)),
      fontScale: normalizeDesktopLyricsFontScale(Number(this.get(DESKTOP_LYRICS_FONT_SCALE_KEY))),
      textColor: colors.textColor,
      highlightColor: colors.highlightColor,
      wordLyricsEnabled: normalizeBoolean(this.get(DESKTOP_LYRICS_WORD_LYRICS_ENABLED_KEY), true),
    };
  }

  writeDesktopLyricsVisible(visible: boolean): void {
    this.set(DESKTOP_LYRICS_VISIBLE_KEY, String(visible));
  }

  writeDesktopLyricsLocked(locked: boolean): void {
    this.set(DESKTOP_LYRICS_LOCKED_KEY, String(locked));
  }

  writeDesktopLyricsFullscreenBehavior(value: DesktopLyricsFullscreenBehavior): void {
    this.set(DESKTOP_LYRICS_FULLSCREEN_BEHAVIOR_KEY, normalizeFullscreenBehavior(value));
  }

  writeDesktopLyricsFontScale(value: number): void {
    this.set(DESKTOP_LYRICS_FONT_SCALE_KEY, String(normalizeDesktopLyricsFontScale(value)));
  }

  writeDesktopLyricsTextColor(value: string): void {
    this.set(DESKTOP_LYRICS_TEXT_COLOR_KEY, normalizeColor(value, DEFAULT_DESKTOP_LYRICS_TEXT_COLOR));
  }

  writeDesktopLyricsHighlightColor(value: string): void {
    this.set(DESKTOP_LYRICS_HIGHLIGHT_COLOR_KEY, normalizeColor(value, DEFAULT_DESKTOP_LYRICS_HIGHLIGHT_COLOR));
  }

  writeDesktopLyricsWordLyricsEnabled(enabled: boolean): void {
    this.set(DESKTOP_LYRICS_WORD_LYRICS_ENABLED_KEY, String(enabled));
  }

  readLyricsOffset(trackId: string): number {
    return trackId ? this.readOffsets()[trackId] ?? 0 : 0;
  }

  writeLyricsOffset(trackId: string, offset: number): void {
    if (!trackId) return;
    const offsets = this.readOffsets();
    delete offsets[trackId];
    const normalized = normalizeOffset(offset);
    if (normalized !== 0) offsets[trackId] = normalized;
    while (Object.keys(offsets).length > MAX_LYRICS_OFFSETS) delete offsets[Object.keys(offsets)[0]!];
    this.set(OFFSETS_KEY, JSON.stringify(offsets));
  }

  clearLyricsOffsets(): void {
    this.remove(OFFSETS_KEY);
  }

  private readDesktopLyricsColors(): Pick<DesktopLyricsPreferencesSnapshot, "textColor" | "highlightColor"> {
    const storedTextColor = this.tryGet(DESKTOP_LYRICS_TEXT_COLOR_KEY);
    const storedHighlightColor = this.tryGet(DESKTOP_LYRICS_HIGHLIGHT_COLOR_KEY);
    const storedPaletteVersion = this.tryGet(DESKTOP_LYRICS_PALETTE_VERSION_KEY);
    const textColor = normalizeColor(storedTextColor.value, DEFAULT_DESKTOP_LYRICS_TEXT_COLOR);
    const highlightColor = normalizeColor(storedHighlightColor.value, DEFAULT_DESKTOP_LYRICS_HIGHLIGHT_COLOR);
    if (!storedTextColor.available || !storedHighlightColor.available || !storedPaletteVersion.available) {
      return { textColor, highlightColor };
    }
    if (storedPaletteVersion.value === DESKTOP_LYRICS_PALETTE_VERSION) {
      return { textColor, highlightColor };
    }

    const migrated = {
      textColor: LEGACY_DESKTOP_LYRICS_TEXT_COLORS.includes(textColor) ? DEFAULT_DESKTOP_LYRICS_TEXT_COLOR : textColor,
      highlightColor: LEGACY_DESKTOP_LYRICS_HIGHLIGHT_COLORS.includes(highlightColor)
        ? DEFAULT_DESKTOP_LYRICS_HIGHLIGHT_COLOR
        : highlightColor,
    };
    const textColorWritten = this.set(DESKTOP_LYRICS_TEXT_COLOR_KEY, migrated.textColor);
    const highlightColorWritten = this.set(DESKTOP_LYRICS_HIGHLIGHT_COLOR_KEY, migrated.highlightColor);
    if (textColorWritten && highlightColorWritten) {
      this.set(DESKTOP_LYRICS_PALETTE_VERSION_KEY, DESKTOP_LYRICS_PALETTE_VERSION);
    }
    return migrated;
  }

  private readOffsets(): Record<string, number> {
    try {
      const value = JSON.parse(this.get(OFFSETS_KEY) ?? "{}") as Record<string, unknown>;
      if (!value || typeof value !== "object" || Array.isArray(value)) return {};
      return Object.fromEntries(
        Object.entries(value)
          .filter((entry): entry is [string, number] => typeof entry[1] === "number" && Number.isFinite(entry[1]))
          .map(([trackId, offset]) => [trackId, normalizeOffset(offset)]),
      );
    } catch {
      return {};
    }
  }

  private get(key: string): string | null {
    return this.tryGet(key).value;
  }

  private tryGet(key: string): { value: string | null; available: boolean } {
    try {
      return { value: this.storage.getItem(key), available: true };
    } catch {
      return { value: null, available: false };
    }
  }

  private set(key: string, value: string): boolean {
    try {
      this.storage.setItem(key, value);
      return true;
    } catch {
      // UI preferences are optional; storage failures must not break playback or navigation.
      return false;
    }
  }

  private remove(key: string): void {
    try {
      this.storage.removeItem(key);
    } catch {
      // Keep the in-memory UI usable when browser storage is unavailable.
    }
  }
}

function normalizeBoolean(value: string | null, fallback: boolean): boolean {
  return value === "true" ? true : value === "false" ? false : fallback;
}

function normalizeFullscreenBehavior(value: string | null): DesktopLyricsFullscreenBehavior {
  return value === "hide" ? "hide" : "show";
}

function normalizeDesktopLyricsFontScale(value: number): number {
  return Number.isFinite(value) && value >= MIN_DESKTOP_LYRICS_FONT_SCALE && value <= MAX_DESKTOP_LYRICS_FONT_SCALE
    ? Number(value.toFixed(2))
    : DEFAULT_DESKTOP_LYRICS_FONT_SCALE;
}

function normalizeColor(value: string | null | undefined, fallback: string): string {
  return typeof value === "string" && /^#[0-9a-f]{6}$/iu.test(value) ? value.toLowerCase() : fallback;
}

function normalizeFontScale(value: number): number {
  return Number.isFinite(value) && value >= MIN_FONT_SCALE && value <= MAX_FONT_SCALE ? value : DEFAULT_FONT_SCALE;
}

function normalizeOffset(value: number): number {
  if (!Number.isFinite(value)) return 0;
  return Math.max(MIN_LYRICS_OFFSET, Math.min(MAX_LYRICS_OFFSET, Number(value.toFixed(1))));
}

const THEME_KEY = "xy-music-theme";
const FONT_SCALE_KEY = "xymusic.desktop.lyrics.font-scale";
const TRANSLATION_KEY = "xymusic.desktop.lyrics.translation";
const LYRICS_WORD_LYRICS_ENABLED_KEY = "xymusic.desktop.lyrics.word-lyrics-enabled";
const LYRICS_DARK_TEXT_COLOR_KEY = "xymusic.playback-lyrics.dark.text-color";
const LYRICS_DARK_HIGHLIGHT_COLOR_KEY = "xymusic.playback-lyrics.dark.highlight-color";
const LYRICS_LIGHT_TEXT_COLOR_KEY = "xymusic.playback-lyrics.light.text-color";
const LYRICS_LIGHT_HIGHLIGHT_COLOR_KEY = "xymusic.playback-lyrics.light.highlight-color";
const DESKTOP_LYRICS_VISIBLE_KEY = "xymusic.desktop-lyrics.visible";
const DESKTOP_LYRICS_LOCKED_KEY = "xymusic.desktop-lyrics.locked";
const DESKTOP_LYRICS_FULLSCREEN_BEHAVIOR_KEY = "xymusic.desktop-lyrics.fullscreen-behavior";
const DESKTOP_LYRICS_FONT_SCALE_KEY = "xymusic.desktop-lyrics.font-scale";
const DESKTOP_LYRICS_TEXT_COLOR_KEY = "xymusic.desktop-lyrics.text-color";
const DESKTOP_LYRICS_HIGHLIGHT_COLOR_KEY = "xymusic.desktop-lyrics.highlight-color";
const DESKTOP_LYRICS_WORD_LYRICS_ENABLED_KEY = "xymusic.desktop-lyrics.word-lyrics-enabled";
const DESKTOP_LYRICS_PALETTE_VERSION_KEY = "xymusic.desktop-lyrics.palette-version";
const OFFSETS_KEY = "xymusic.desktop.lyrics.offsets.v1";
const DEFAULT_FONT_SCALE = 1;
const MIN_FONT_SCALE = 0.85;
const MAX_FONT_SCALE = 1.25;
const MIN_LYRICS_OFFSET = -5;
const MAX_LYRICS_OFFSET = 5;
const MAX_LYRICS_OFFSETS = 100;
const DEFAULT_DESKTOP_LYRICS_FONT_SCALE = 1;
const MIN_DESKTOP_LYRICS_FONT_SCALE = 0.75;
const MAX_DESKTOP_LYRICS_FONT_SCALE = 1.5;
const LEGACY_DESKTOP_LYRICS_TEXT_COLORS: readonly string[] = ["#ffffff", "#f8fafc"];
const LEGACY_DESKTOP_LYRICS_HIGHLIGHT_COLORS: readonly string[] = ["#5af5df", "#63e6be", "#8eb1d2"];
const DESKTOP_LYRICS_PALETTE_VERSION = "3";
