export type ThemePreference = "dark" | "light" | "system";
export type DesktopLyricsFullscreenBehavior = "show" | "hide";
export type LyricsColorScheme = "dark" | "light";

export const DEFAULT_DESKTOP_LYRICS_TEXT_COLOR = "#f4f5f7";
export const DEFAULT_DESKTOP_LYRICS_HIGHLIGHT_COLOR = "#cf9437";
export const DEFAULT_PLAYBACK_LYRICS_COLORS = {
  dark: { textColor: "#8e98a3", highlightColor: "#d7e6f3" },
  light: { textColor: "#626a74", highlightColor: "#1b4269" },
} as const satisfies Record<LyricsColorScheme, LyricsColors>;

export interface LyricsColors {
  textColor: string;
  highlightColor: string;
}

export interface LyricsPreferencesSnapshot {
  fontScale: number;
  showTranslation: boolean;
  wordLyricsEnabled: boolean;
  colors: Record<LyricsColorScheme, LyricsColors>;
}

export interface DesktopLyricsPreferencesSnapshot {
  visible: boolean;
  locked: boolean;
  fullscreenBehavior: DesktopLyricsFullscreenBehavior;
  fontScale: number;
  textColor: string;
  highlightColor: string;
  wordLyricsEnabled: boolean;
}

export interface UserInterfacePreferences {
  readTheme(): ThemePreference;
  writeTheme(value: ThemePreference): void;
  readLyrics(): LyricsPreferencesSnapshot;
  writeLyricsFontScale(value: number): void;
  writeLyricsTranslation(visible: boolean): void;
  writeLyricsWordLyricsEnabled(enabled: boolean): void;
  writeLyricsTextColor(scheme: LyricsColorScheme, value: string): void;
  writeLyricsHighlightColor(scheme: LyricsColorScheme, value: string): void;
  readDesktopLyrics(): DesktopLyricsPreferencesSnapshot;
  writeDesktopLyricsVisible(visible: boolean): void;
  writeDesktopLyricsLocked(locked: boolean): void;
  writeDesktopLyricsFullscreenBehavior(value: DesktopLyricsFullscreenBehavior): void;
  writeDesktopLyricsFontScale(value: number): void;
  writeDesktopLyricsTextColor(value: string): void;
  writeDesktopLyricsHighlightColor(value: string): void;
  writeDesktopLyricsWordLyricsEnabled(enabled: boolean): void;
  readLyricsOffset(trackId: string): number;
  writeLyricsOffset(trackId: string, offset: number): void;
  clearLyricsOffsets(): void;
}
