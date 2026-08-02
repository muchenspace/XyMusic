import { describe, expect, it, vi } from "vitest";
import type { AudioPlayer, AudioSnapshot } from "../src/application/ports/AudioPlayer";
import type { PlayerPreferences } from "../src/application/ports/PlayerPreferences";
import type { TaskScheduler } from "../src/application/ports/TaskScheduler";
import { PlaybackPreferences, VOLUME_PERSIST_DEBOUNCE_MS } from "../src/application/services/PlaybackPreferences";

describe("playback preferences", () => {
  it("updates audio immediately while coalescing durable volume writes", () => {
    const audio = new FakeAudioPlayer();
    const preferences = createPreferences();
    const scheduler = new ManualTaskScheduler();
    const service = new PlaybackPreferences(audio, preferences, scheduler);

    expect(service.initializeVolume(72)).toBe(72);
    service.setVolume(18);
    service.setVolume(41);

    expect(audio.volumes).toEqual([0.72, 0.18, 0.41]);
    expect(preferences.writeVolume).not.toHaveBeenCalled();
    scheduler.runDelayed();
    expect(preferences.writeVolume).toHaveBeenCalledExactlyOnceWith(41);
  });

  it("normalizes inputs and flushes pending volume before disposal", () => {
    const audio = new FakeAudioPlayer();
    const preferences = createPreferences();
    const service = new PlaybackPreferences(audio, preferences, new ManualTaskScheduler());

    expect(service.setVolume(Number.NaN)).toBe(0);
    expect(service.setCrossfadeSeconds(9.6)).toBe(5);
    service.setQuality("LOSSLESS");
    service.setNotificationsEnabled(true);
    service.dispose();

    expect(preferences.writeVolume).toHaveBeenCalledExactlyOnceWith(0);
    expect(preferences.writeCrossfadeSeconds).toHaveBeenCalledExactlyOnceWith(5);
    expect(preferences.writeQuality).toHaveBeenCalledExactlyOnceWith("LOSSLESS");
    expect(preferences.writeNotificationsEnabled).toHaveBeenCalledExactlyOnceWith(true);
    expect(VOLUME_PERSIST_DEBOUNCE_MS).toBeGreaterThan(0);
  });
});

function createPreferences(): PlayerPreferences & {
  writeVolume: ReturnType<typeof vi.fn>;
  writeQuality: ReturnType<typeof vi.fn>;
  writeCrossfadeSeconds: ReturnType<typeof vi.fn>;
  writeNotificationsEnabled: ReturnType<typeof vi.fn>;
} {
  return {
    read: () => ({ volume: 72, quality: "AUTO", crossfadeSeconds: 0, notificationsEnabled: false, hasCrossfadePreference: false }),
    writeVolume: vi.fn(),
    writeQuality: vi.fn(),
    writeCrossfadeSeconds: vi.fn(),
    writeNotificationsEnabled: vi.fn(),
  };
}

class FakeAudioPlayer implements AudioPlayer {
  readonly volumes: number[] = [];

  load(): Promise<void> { return Promise.resolve(); }
  play(): Promise<void> { return Promise.resolve(); }
  pause(): void {}
  stop(): void {}
  seek(): void {}
  setVolume(volume: number): void { this.volumes.push(volume); }
  snapshot(): AudioSnapshot { return { currentTime: 0, duration: 0, paused: true }; }
  onUpdate(): () => void { return () => undefined; }
  onEnded(): () => void { return () => undefined; }
  onError(): () => void { return () => undefined; }
}

class ManualTaskScheduler implements TaskScheduler {
  private readonly delayed = new Set<() => void>();

  delay(callback: () => void): () => void {
    this.delayed.add(callback);
    return () => this.delayed.delete(callback);
  }

  whenIdle(callback: () => void): () => void {
    callback();
    return () => undefined;
  }

  runDelayed(): void {
    const callbacks = [...this.delayed];
    this.delayed.clear();
    for (const callback of callbacks) callback();
  }
}
