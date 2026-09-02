import type { MediaToolsConfig } from "@/shared/domain/runtime-config";

export interface DatabaseSettingsInput {
  host?: string;
  port?: number;
  database?: string;
  username?: string;
  password?: string;
  sslMode?: "disable" | "prefer" | "require" | "verify-full";
  maximumConnections?: number;
}

export interface StorageSettingsInput {
  assetDirectory?: string;
  transcodeDirectory?: string;
  uploadTtlSeconds?: number;
  streamTtlSeconds?: number;
  streamMaxConcurrent?: number;
  streamIdleTimeoutSeconds?: number;
  transcodeTimeoutSeconds?: number;
  transcodeCacheMaxBytes?: number;
  maxUploadBytes?: number;
}

export interface RuntimeSettings {
  version: number;
  environment: string;
  configurationSource: string;
  actualListener: {
    ipv4: { host: string; port: number };
    ipv6: { host: string; port: number };
  };
  restartRequiredFields: string[];
  database: DatabaseSettingsInput & { passwordConfigured: boolean; lockedFields: string[] };
  storage: {
    assetDirectory: string;
    transcodeDirectory: string;
    uploadTtlSeconds: number;
    streamTtlSeconds: number;
    streamMaxConcurrent: number;
    streamIdleTimeoutSeconds: number;
    transcodeTimeoutSeconds: number;
    transcodeCacheMaxBytes: number;
    maxUploadBytes: number;
    lockedFields: string[];
  };
  mediaTools: MediaToolsConfig & { lockedFields: string[] };
  localLibrary: {
    name: string;
    directory: string;
    mode: "READ_ONLY" | "READ_WRITE";
    enabled: boolean;
    syncOnStartup: boolean;
    scanIntervalMinutes: number | null;
    includePatterns: string[];
    excludePatterns: string[];
    lockedFields: string[];
  };
  registration: { enabled: boolean; lockedFields: string[] };
  security: {
    accessTokenTtlSeconds: number;
    refreshTokenTtlSeconds: number;
    lockedFields: string[];
  };
  http: { ipv4Host: string; ipv4Port: number; ipv6Host: string; ipv6Port: number; trustedProxyAddresses: string[]; lockedFields: string[] };
}

export interface SettingsValidationResult {
  ok: true;
  message: string;
  latencyMs?: number;
  details?: string[];
  normalizedPath?: string;
}

export interface RuntimeSettingsUpdate {
  expectedVersion: number;
  database?: DatabaseSettingsInput;
  storage?: StorageSettingsInput;
  mediaTools?: Partial<MediaToolsConfig>;
  localLibrary?: {
    name?: string;
    directory?: string;
    mode?: "READ_ONLY" | "READ_WRITE";
    enabled?: boolean;
    syncOnStartup?: boolean;
    scanIntervalMinutes?: number | null;
    includePatterns?: string[];
    excludePatterns?: string[];
  };
  registration?: { enabled: boolean };
  security?: { accessTokenTtlSeconds?: number; refreshTokenTtlSeconds?: number };
  http?: { ipv4Host?: string; ipv4Port?: number; ipv6Host?: string; ipv6Port?: number; trustedProxyAddresses?: string[] };
}

export interface SystemInformation {
  applicationVersion: string;
  runtimeVersion: string;
  platform: string;
  architecture: string;
  uptimeSeconds: number;
  databaseVersion: string;
  migrationVersion: string;
  ffmpegVersion: string | null;
  dataDirectory: string;
  configurationFile: string;
  configurationSource: string;
  worker: {
    mode: "inline" | "external";
    state: string;
    responsive: boolean;
    synchronized: boolean;
    available: boolean;
    updatedAt: string | null;
  };
  metrics: {
    collectedSince: string;
    requests: {
      total: number;
      inFlight: number;
      errors: number;
      errorRate: number;
      slow: number;
      averageLatencyMs: number;
      p95LatencyMs: number;
      maximumLatencyMs: number;
      sampled: number;
    };
    eventLoop: { lagMs: number; maximumLagMs: number };
    memory: { rssBytes: number; heapUsedBytes: number; heapTotalBytes: number; externalBytes: number };
  } | null;
  queues: {
    media: number;
    scans: number;
    cleanup: number;
    writeback: number;
    scraping: number;
    total: number;
  };
}
