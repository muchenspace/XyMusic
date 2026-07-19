import { describe, expect, it } from "vitest";
import { LocalPlayerPreferences } from "../src/infrastructure/playback/LocalPlayerPreferences";

describe("local player preferences", () => {
  it("normalizes missing, invalid, and out-of-range stored values", () => {
    const storage = new MemoryStorage({
      "xymusic.desktop.volume": "999",
      "xymusic.desktop.quality": "INVALID",
      "xymusic.desktop.crossfade-seconds": "9.7",
      "xymusic.desktop.playback-notifications": "yes",
    });

    expect(new LocalPlayerPreferences(storage).read()).toEqual({
      volume: 100,
      quality: "AUTO",
      crossfadeSeconds: 5,
      notificationsEnabled: false,
      hasCrossfadePreference: true,
    });
  });

  it("writes normalized values and tolerates unavailable storage", () => {
    const storage = new MemoryStorage();
    const preferences = new LocalPlayerPreferences(storage);
    preferences.writeVolume(-10);
    preferences.writeQuality("LOSSLESS");
    preferences.writeCrossfadeSeconds(2.6);
    preferences.writeNotificationsEnabled(true);

    expect(storage.values).toMatchObject({
      "xymusic.desktop.volume": "0",
      "xymusic.desktop.quality": "LOSSLESS",
      "xymusic.desktop.crossfade-seconds": "3",
      "xymusic.desktop.playback-notifications": "true",
    });

    const unavailable = new LocalPlayerPreferences({
      getItem: () => { throw new DOMException("denied"); },
      setItem: () => { throw new DOMException("denied"); },
    });
    expect(unavailable.read()).toMatchObject({ volume: 72, quality: "AUTO", crossfadeSeconds: 0 });
    expect(() => unavailable.writeVolume(50)).not.toThrow();
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
}
