import type { PageLifecycle } from "../../src/application/ports/PageLifecycle";
import type { TaskScheduler } from "../../src/application/ports/TaskScheduler";
import type { UserInterfacePreferences } from "../../src/application/ports/UserInterfacePreferences";
import { LyricsPreferencePersistence } from "../../src/application/services/LyricsPreferencePersistence";

type LyricsPreferenceWriter = Pick<
  UserInterfacePreferences,
  "writeLyricsFontScale" | "writeLyricsTextColor" | "writeLyricsHighlightColor"
>;

/** Wires the application debounce service to the jsdom timer and pagehide APIs. */
export function createTestLyricsPreferencePersistence(
  preferences: LyricsPreferenceWriter,
): LyricsPreferencePersistence {
  return new LyricsPreferencePersistence(preferences, browserTaskScheduler, browserPageLifecycle);
}

const browserTaskScheduler: TaskScheduler = {
  delay(callback, milliseconds) {
    const handle = window.setTimeout(callback, milliseconds);
    return () => window.clearTimeout(handle);
  },
  whenIdle(callback, _timeoutMilliseconds) {
    const handle = window.setTimeout(callback, 0);
    return () => window.clearTimeout(handle);
  },
};

const browserPageLifecycle: PageLifecycle = {
  onPageHide(listener) {
    window.addEventListener("pagehide", listener);
    return () => window.removeEventListener("pagehide", listener);
  },
};
