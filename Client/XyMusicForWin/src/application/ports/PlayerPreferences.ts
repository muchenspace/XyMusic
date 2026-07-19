import type { PlaybackQuality } from "../../domain/music";

export interface PlayerPreferencesSnapshot {
  volume: number;
  quality: PlaybackQuality;
  crossfadeSeconds: number;
  notificationsEnabled: boolean;
  hasCrossfadePreference: boolean;
}

export interface PlayerPreferences {
  read(): PlayerPreferencesSnapshot;
  writeVolume(value: number): void;
  writeQuality(value: PlaybackQuality): void;
  writeCrossfadeSeconds(value: number): void;
  writeNotificationsEnabled(value: boolean): void;
}
