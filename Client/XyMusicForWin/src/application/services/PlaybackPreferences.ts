import type { AudioPlayer } from "../ports/AudioPlayer";
import type { PlayerPreferences, PlayerPreferencesSnapshot } from "../ports/PlayerPreferences";
import type { TaskScheduler } from "../ports/TaskScheduler";
import type { PlaybackQuality } from "../../domain/music";

/**
 * Applies player preferences immediately where playback needs them and defers
 * only the durable volume write while a slider is moving.
 */
export class PlaybackPreferences {
  private pendingVolume: number | null = null;
  private cancelVolumeWrite: (() => void) | undefined;

  constructor(
    private readonly audio: AudioPlayer,
    private readonly preferences: PlayerPreferences,
    private readonly scheduler: TaskScheduler,
  ) {}

  read(): PlayerPreferencesSnapshot {
    return this.preferences.read();
  }

  initializeVolume(value: number): number {
    const normalized = normalizeVolume(value);
    this.audio.setVolume(normalized / 100);
    return normalized;
  }

  setVolume(value: number): number {
    const normalized = this.initializeVolume(value);
    this.pendingVolume = normalized;
    this.cancelVolumeWrite?.();
    this.cancelVolumeWrite = this.scheduler.delay(() => this.flush(), VOLUME_PERSIST_DEBOUNCE_MS);
    return normalized;
  }

  setQuality(value: PlaybackQuality): void {
    this.preferences.writeQuality(value);
  }

  setCrossfadeSeconds(value: number): number {
    const normalized = normalizeCrossfade(value);
    this.preferences.writeCrossfadeSeconds(normalized);
    return normalized;
  }

  setNotificationsEnabled(value: boolean): void {
    this.preferences.writeNotificationsEnabled(value);
  }

  flush(): void {
    this.cancelVolumeWrite?.();
    this.cancelVolumeWrite = undefined;
    const volume = this.pendingVolume;
    this.pendingVolume = null;
    if (volume !== null) this.preferences.writeVolume(volume);
  }

  dispose(): void {
    this.flush();
  }
}

export const VOLUME_PERSIST_DEBOUNCE_MS = 180;

function normalizeVolume(value: number): number {
  return Number.isFinite(value) ? Math.max(0, Math.min(100, Number(value))) : 0;
}

function normalizeCrossfade(value: number): number {
  return Number.isFinite(value) ? Math.max(0, Math.min(5, Math.round(value))) : 0;
}
