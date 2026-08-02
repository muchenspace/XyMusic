import { afterEach, describe, expect, it, vi } from "vitest";
import {
  LyricsPreferencePersistence,
  LYRICS_PREFERENCE_PERSIST_DEBOUNCE_MS,
} from "../src/presentation/stores/LyricsPreferencePersistence";

describe("lyrics preference persistence", () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  it("debounces rapid changes and persists only the latest value for each display preference", () => {
    vi.useFakeTimers();
    const preferences = createPreferences();
    const persistence = new LyricsPreferencePersistence(preferences);

    for (let index = 0; index <= 40; index += 1) {
      persistence.queueFontScale(Number((0.85 + index * 0.01).toFixed(2)));
    }
    persistence.queueTextColor("dark", "#111111");
    persistence.queueTextColor("dark", "#222222");
    persistence.queueHighlightColor("light", "#333333");
    persistence.queueHighlightColor("light", "#444444");

    expect(preferences.writeLyricsFontScale).not.toHaveBeenCalled();
    expect(preferences.writeLyricsTextColor).not.toHaveBeenCalled();
    expect(preferences.writeLyricsHighlightColor).not.toHaveBeenCalled();

    vi.advanceTimersByTime(LYRICS_PREFERENCE_PERSIST_DEBOUNCE_MS - 1);
    expect(preferences.writeLyricsFontScale).not.toHaveBeenCalled();

    vi.advanceTimersByTime(1);
    expect(preferences.writeLyricsFontScale).toHaveBeenCalledTimes(1);
    expect(preferences.writeLyricsFontScale).toHaveBeenLastCalledWith(1.25);
    expect(preferences.writeLyricsTextColor).toHaveBeenCalledExactlyOnceWith("dark", "#222222");
    expect(preferences.writeLyricsHighlightColor).toHaveBeenCalledExactlyOnceWith("light", "#444444");
  });

  it("flushes pending writes synchronously without a later duplicate", () => {
    vi.useFakeTimers();
    const preferences = createPreferences();
    const persistence = new LyricsPreferencePersistence(preferences);

    persistence.queueFontScale(1.1);
    persistence.flush();

    expect(preferences.writeLyricsFontScale).toHaveBeenCalledExactlyOnceWith(1.1);
    vi.advanceTimersByTime(LYRICS_PREFERENCE_PERSIST_DEBOUNCE_MS);
    expect(preferences.writeLyricsFontScale).toHaveBeenCalledTimes(1);
  });
});

function createPreferences() {
  return {
    writeLyricsFontScale: vi.fn(),
    writeLyricsTextColor: vi.fn(),
    writeLyricsHighlightColor: vi.fn(),
  };
}
