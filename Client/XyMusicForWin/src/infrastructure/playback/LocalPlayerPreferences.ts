import type { PlayerPreferences, PlayerPreferencesSnapshot } from "../../application/ports/PlayerPreferences";
import type { PlaybackQuality } from "../../domain/music";

export class LocalPlayerPreferences implements PlayerPreferences {
  constructor(private readonly storage: Pick<Storage, "getItem" | "setItem"> = localStorage) {}

  read(): PlayerPreferencesSnapshot {
    const volume = this.get(VOLUME_KEY);
    const crossfade = this.get(CROSSFADE_KEY);
    return {
      volume: volume === null ? DEFAULT_VOLUME : normalizeVolume(Number(volume)),
      quality: normalizeQuality(this.get(QUALITY_KEY)),
      crossfadeSeconds: normalizeCrossfade(Number(crossfade)),
      notificationsEnabled: this.get(NOTIFICATIONS_KEY) === "true",
      hasCrossfadePreference: crossfade !== null,
    };
  }

  writeVolume(value: number): void {
    this.set(VOLUME_KEY, String(normalizeVolume(value)));
  }

  writeQuality(value: PlaybackQuality): void {
    this.set(QUALITY_KEY, normalizeQuality(value));
  }

  writeCrossfadeSeconds(value: number): void {
    this.set(CROSSFADE_KEY, String(normalizeCrossfade(value)));
  }

  writeNotificationsEnabled(value: boolean): void {
    this.set(NOTIFICATIONS_KEY, String(value));
  }

  private get(key: string): string | null {
    try {
      return this.storage.getItem(key);
    } catch {
      return null;
    }
  }

  private set(key: string, value: string): void {
    try {
      this.storage.setItem(key, value);
    } catch {
      // Playback remains usable when browser preference storage is unavailable.
    }
  }
}

function normalizeQuality(value: unknown): PlaybackQuality {
  return value === "DATA_SAVER" || value === "STANDARD" || value === "HIGH" || value === "LOSSLESS" ? value : "AUTO";
}

function normalizeVolume(value: number): number {
  return Number.isFinite(value) ? Math.max(0, Math.min(100, value)) : DEFAULT_VOLUME;
}

function normalizeCrossfade(value: number): number {
  return Number.isFinite(value) ? Math.max(0, Math.min(5, Math.round(value))) : 0;
}

const VOLUME_KEY = "xymusic.desktop.volume";
const QUALITY_KEY = "xymusic.desktop.quality";
const CROSSFADE_KEY = "xymusic.desktop.crossfade-seconds";
const NOTIFICATIONS_KEY = "xymusic.desktop.playback-notifications";
const DEFAULT_VOLUME = 72;
