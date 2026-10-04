import type { RuntimeSettings, RuntimeSettingsUpdate } from "@/features/settings/domain/models";

export type SettingsDatabaseForm = {
  host: string;
  port: number;
  database: string;
  username: string;
  sslMode: "disable" | "prefer" | "require" | "verify-full";
  maximumConnections: number;
};

export type SettingsStorageForm = {
  assetDirectory: string;
  uploadTtlSeconds: number;
  streamTtlSeconds: number;
  maxUploadBytes: number;
};

export type SettingsMediaToolsForm = { directory: string; ffmpegPath: string; ffprobePath: string };

export type SettingsLocalLibraryForm = {
  name: string;
  directory: string;
  mode: "READ_ONLY" | "READ_WRITE";
  enabled: boolean;
  syncOnStartup: boolean;
  scanIntervalMinutes: number | null;
};

export type SettingsHttpForm = { ipv4Host: string; ipv4Port: number; ipv6Host: string; ipv6Port: number };

/** Reactive editor state snapshot used for dirty tracking and payload assembly. */
export interface SettingsEditorState {
  version: number;
  database: SettingsDatabaseForm;
  storage: SettingsStorageForm;
  mediaTools: SettingsMediaToolsForm;
  localLibrary: SettingsLocalLibraryForm;
  registration: { enabled: boolean };
  security: { accessTokenTtlSeconds: number; refreshTokenTtlSeconds: number };
  http: SettingsHttpForm;
  autoDetectMedia: boolean;
  proxies: string;
  includePatterns: string;
  excludePatterns: string;
  databasePassword: string;
}

export type SettingsSection =
  | "database" | "storage" | "mediaTools" | "localLibrary" | "registration" | "security"
  | "http" | "proxies" | "includePatterns" | "excludePatterns" | "autoDetectMedia";

export interface SettingsEditorSnapshot {
  version: number;
  database: SettingsDatabaseForm;
  storage: SettingsStorageForm;
  autoDetectMedia: boolean;
  mediaTools: SettingsMediaToolsForm;
  localLibrary: SettingsLocalLibraryForm;
  registration: { enabled: boolean };
  security: { accessTokenTtlSeconds: number; refreshTokenTtlSeconds: number };
  http: SettingsHttpForm;
  proxies: string;
  includePatterns: string;
  excludePatterns: string;
}

/** Maps a server settings document into editor values (null-safe defaults preserved). */
export function settingsEditorSnapshot(settings: RuntimeSettings): SettingsEditorSnapshot {
  return {
    version: settings.version,
    database: {
      host: settings.database.host ?? "",
      port: settings.database.port ?? 5432,
      database: settings.database.database ?? "",
      username: settings.database.username ?? "",
      sslMode: settings.database.sslMode ?? "prefer",
      maximumConnections: settings.database.maximumConnections ?? 10,
    },
    storage: {
      assetDirectory: settings.storage.assetDirectory ?? "",
      uploadTtlSeconds: settings.storage.uploadTtlSeconds ?? 3600,
      streamTtlSeconds: settings.storage.streamTtlSeconds ?? 900,
      maxUploadBytes: settings.storage.maxUploadBytes ?? 1_073_741_824,
    },
    autoDetectMedia: Boolean(settings.mediaTools.directory),
    mediaTools: {
      directory: settings.mediaTools.directory ?? "",
      ffmpegPath: settings.mediaTools.ffmpegPath,
      ffprobePath: settings.mediaTools.ffprobePath,
    },
    localLibrary: {
      name: settings.localLibrary.name,
      directory: settings.localLibrary.directory,
      mode: settings.localLibrary.mode,
      enabled: settings.localLibrary.enabled,
      syncOnStartup: settings.localLibrary.syncOnStartup,
      scanIntervalMinutes: settings.localLibrary.scanIntervalMinutes,
    },
    registration: { enabled: settings.registration.enabled },
    security: {
      accessTokenTtlSeconds: settings.security.accessTokenTtlSeconds,
      refreshTokenTtlSeconds: settings.security.refreshTokenTtlSeconds,
    },
    http: {
      ipv4Host: settings.http.ipv4Host,
      ipv4Port: settings.http.ipv4Port,
      ipv6Host: settings.http.ipv6Host,
      ipv6Port: settings.http.ipv6Port,
    },
    proxies: settings.http.trustedProxyAddresses.join("\n"),
    includePatterns: settings.localLibrary.includePatterns.join("\n"),
    excludePatterns: settings.localLibrary.excludePatterns.join("\n"),
  };
}

/** Snapshot compared against the saved baseline for dirty tracking. */
export function settingsEditableState(state: SettingsEditorState): object {
  return {
    database: state.database,
    storage: state.storage,
    autoDetectMedia: state.autoDetectMedia,
    mediaTools: state.mediaTools,
    localLibrary: state.localLibrary,
    registration: state.registration,
    security: state.security,
    http: state.http,
    proxies: state.proxies,
    includePatterns: state.includePatterns,
    excludePatterns: state.excludePatterns,
    databasePassword: state.databasePassword,
  };
}

export function settingsSectionChanged(state: SettingsEditorState, baseline: string, section: SettingsSection): boolean {
  const current = settingsEditableState(state) as Record<string, unknown>;
  const saved = JSON.parse(baseline || "{}") as Record<string, unknown>;
  return JSON.stringify(current[section]) !== JSON.stringify(saved[section]);
}

export function settingsMediaToolsPayload(state: SettingsEditorState): { directory: string } | { ffmpegPath: string; ffprobePath: string } {
  return state.autoDetectMedia
    ? { directory: state.mediaTools.directory.trim() }
    : { ffmpegPath: state.mediaTools.ffmpegPath.trim(), ffprobePath: state.mediaTools.ffprobePath.trim() };
}

export function settingsLines(value: string): string[] {
  return [...new Set(value.split(/[,，\r\n]+/).map((item) => item.trim()).filter(Boolean))];
}

/**
 * Assembles the update payload, sending only sections the administrator
 * actually changed relative to the saved baseline.
 */
export function buildRuntimeSettingsUpdate(state: SettingsEditorState, baseline: string): RuntimeSettingsUpdate {
  const result: RuntimeSettingsUpdate = { expectedVersion: state.version };
  if (settingsSectionChanged(state, baseline, "database") || state.databasePassword.trim()) {
    result.database = { ...state.database, password: state.databasePassword || undefined };
  }
  if (settingsSectionChanged(state, baseline, "storage")) {
    result.storage = { ...state.storage };
  }
  if (settingsSectionChanged(state, baseline, "mediaTools") || settingsSectionChanged(state, baseline, "autoDetectMedia")) {
    result.mediaTools = settingsMediaToolsPayload(state);
  }
  if (settingsSectionChanged(state, baseline, "localLibrary")
    || settingsSectionChanged(state, baseline, "includePatterns")
    || settingsSectionChanged(state, baseline, "excludePatterns")) {
    result.localLibrary = {
      ...state.localLibrary,
      includePatterns: settingsLines(state.includePatterns),
      excludePatterns: settingsLines(state.excludePatterns),
    };
  }
  if (settingsSectionChanged(state, baseline, "registration")) result.registration = { enabled: state.registration.enabled };
  if (settingsSectionChanged(state, baseline, "security")) result.security = { ...state.security };
  if (settingsSectionChanged(state, baseline, "http") || settingsSectionChanged(state, baseline, "proxies")) {
    result.http = { ...state.http, trustedProxyAddresses: settingsLines(state.proxies) };
  }
  return result;
}
