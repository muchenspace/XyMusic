import type {
  LyricsColorScheme,
  UserInterfacePreferences,
} from "../../application/ports/UserInterfacePreferences";

type LyricsPreferenceWriter = Pick<
  UserInterfacePreferences,
  "writeLyricsFontScale" | "writeLyricsTextColor" | "writeLyricsHighlightColor"
>;

type PendingWrite =
  | { kind: "font-scale"; value: number }
  | { kind: "text-color"; scheme: LyricsColorScheme; value: string }
  | { kind: "highlight-color"; scheme: LyricsColorScheme; value: string };

type PendingWriteKey = "font-scale"
  | "text-color:dark"
  | "text-color:light"
  | "highlight-color:dark"
  | "highlight-color:light";

/**
 * Keeps rapid display-preference changes responsive without turning every input
 * event into a synchronous persistence write. The queue has at most five keys.
 */
export class LyricsPreferencePersistence {
  private readonly pendingWrites = new Map<PendingWriteKey, PendingWrite>();
  private timer: number | null = null;

  constructor(
    private readonly preferences: LyricsPreferenceWriter,
    private readonly debounceMs = LYRICS_PREFERENCE_PERSIST_DEBOUNCE_MS,
  ) {}

  queueFontScale(value: number): void {
    this.queue("font-scale", { kind: "font-scale", value });
  }

  queueTextColor(scheme: LyricsColorScheme, value: string): void {
    this.queue(`text-color:${scheme}`, { kind: "text-color", scheme, value });
  }

  queueHighlightColor(scheme: LyricsColorScheme, value: string): void {
    this.queue(`highlight-color:${scheme}`, { kind: "highlight-color", scheme, value });
  }

  flush(): void {
    if (this.timer !== null) window.clearTimeout(this.timer);
    this.timer = null;

    const writes = [...this.pendingWrites.values()];
    this.pendingWrites.clear();
    for (const write of writes) this.write(write);
  }

  private queue(key: PendingWriteKey, write: PendingWrite): void {
    this.pendingWrites.set(key, write);
    if (this.timer !== null) window.clearTimeout(this.timer);
    this.timer = window.setTimeout(() => this.flush(), this.debounceMs);
  }

  private write(write: PendingWrite): void {
    if (write.kind === "font-scale") {
      this.preferences.writeLyricsFontScale(write.value);
      return;
    }
    if (write.kind === "text-color") {
      this.preferences.writeLyricsTextColor(write.scheme, write.value);
      return;
    }
    this.preferences.writeLyricsHighlightColor(write.scheme, write.value);
  }
}

export const LYRICS_PREFERENCE_PERSIST_DEBOUNCE_MS = 180;
