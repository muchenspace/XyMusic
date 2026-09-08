export interface PlayerPreferencesSnapshot {
  volume: number;
  crossfadeSeconds: number;
  notificationsEnabled: boolean;
  hasCrossfadePreference: boolean;
}

export interface PlayerPreferences {
  read(): PlayerPreferencesSnapshot;
  writeVolume(value: number): void;
  writeCrossfadeSeconds(value: number): void;
  writeNotificationsEnabled(value: boolean): void;
}
