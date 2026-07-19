export interface SetupStatus {
  setupRequired: boolean;
  configured: boolean;
  configurationSource: "managed" | "setup";
  platform: string;
  runtime: {
    phase: "SETUP_REQUIRED" | "STARTING" | "READY" | "FAILED" | "CLOSED";
    generation: number;
    source: "managed" | "setup";
    lastError?: string;
  };
}

export interface SetupDatabaseConfig {
  host: string;
  port: number;
  database: string;
  username: string;
  password: string;
  sslMode: "disable" | "prefer" | "require" | "verify-full";
  maxConnections: number;
}

export interface SetupHttpConfig {
  ipv4Host: string;
  ipv4Port: number;
  ipv6Host: string;
  ipv6Port: number;
  trustedProxyAddresses: string[];
}

export interface SetupPathConfig {
  migrationsDirectory: string;
  adminWebDirectory: string;
}

export interface ObjectStorageConfig {
  endpoint: string;
  region: string;
  bucket: string;
  accessKeyId: string;
  secretAccessKey: string;
  forcePathStyle: boolean;
  publicBaseUrl?: string;
  signedUrlTtlSeconds: number;
  maxUploadBytes: number;
}

export interface SetupMediaConfig {
  mode: "DIRECTORY" | "ADVANCED";
  directory: string;
  ffmpegPath: string;
  ffprobePath: string;
  fpcalcPath: string;
  acoustIdClient: string;
}

export interface SetupSourceInput {
  name: string;
  directory: string;
  mode: "READ_ONLY" | "READ_WRITE";
  enabled: boolean;
  syncOnStartup: boolean;
  scanIntervalMinutes?: number | null;
  includePatterns: string[];
  excludePatterns: string[];
}

export interface BootstrapAdminInput {
  username: string;
  displayName: string;
  password: string;
}

export interface SetupValidationResult {
  ok: true;
  serverTimeMs?: number;
  ffmpeg?: string;
  ffprobe?: string;
  fpcalc?: string;
  paths?: { ffmpegPath: string; ffprobePath: string; fpcalcPath?: string };
  fpcalcDescription?: string;
  fingerprintConfigured?: boolean;
  directory?: string;
  resolvedPaths?: Record<string, string>;
  databaseInspection?: {
    state: "EMPTY" | "PARTIAL" | "COMPLETE";
    migrationRequired: boolean;
    hasData: boolean;
    hasAdministrator: boolean;
    hasActiveAdministrator: boolean;
    reusable: string[];
    missing: string[];
  };
  storageInspection?: {
    bucketExists: boolean;
    hasObjects: boolean;
    objectCount: number;
    countLimited: boolean;
  };
}

export interface SetupCompleteInput {
  http: SetupHttpConfig;
  paths: SetupPathConfig;
  database: SetupDatabaseConfig;
  storage: ObjectStorageConfig;
  media: SetupMediaConfig;
  source: SetupSourceInput;
  registration: { enabled: boolean };
  administrator: BootstrapAdminInput;
  databaseAction?: "reuse_partial" | "migrate" | "reset";
  storageAction?: "reuse" | "reset";
}

export interface SetupCompletion {
  configured: true;
  runtimeGeneration: number;
  actualListener: {
    ipv4: { host: string; port: number };
    ipv6: { host: string; port: number };
  };
  restartRequiredFields: Array<"http.ipv4Host" | "http.ipv4Port" | "http.ipv6Host" | "http.ipv6Port">;
}
