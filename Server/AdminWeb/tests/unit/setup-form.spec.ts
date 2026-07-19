import { describe, expect, it } from "vitest";
import type { SetupCompleteInput } from "@/features/setup/domain/models";
import {
  isReachableStorageHost,
  setupCompleteSchema,
  setupStepSchemas,
  validateSetupComplete,
} from "@/features/setup/application/setup-form";

describe("setup administrator form", () => {
  it("validates IPv4 and IPv6 listeners independently", () => {
    const listeners = {
      ipv4Host: "0.0.0.0",
      ipv4Port: 3000,
      ipv6Host: "::",
      ipv6Port: 3000,
      trustedProxyAddresses: [],
    };
    expect(setupStepSchemas.http.safeParse(listeners).success).toBe(true);
    expect(setupStepSchemas.http.safeParse({ ...listeners, ipv4Host: "::" }).success).toBe(false);
    expect(setupStepSchemas.http.safeParse({ ...listeners, ipv6Host: "0.0.0.0" }).success).toBe(false);
    expect(setupStepSchemas.http.safeParse({ ...listeners, ipv6Port: 65_536 }).success).toBe(false);
  });

  it("uses one host policy for storage form and URL validation", () => {
    expect(isReachableStorageHost("storage.internal")).toBe(true);
    expect(isReachableStorageHost("192.168.1.20")).toBe(true);
    for (const host of [
      "localhost",
      "localhost.",
      "127.0.0.1",
      "127.1",
      "2130706433",
      "0.0.0.0",
      "::1",
      "[::1]",
    ]) {
      expect(isReachableStorageHost(host)).toBe(false);
    }
  });

  it("accepts the required administrator credentials", () => {
    expect(setupStepSchemas.administrator.parse({
      username: "admin",
      displayName: "管理员",
      password: "a-secure-password",
    })).toEqual({
      username: "admin",
      displayName: "管理员",
      password: "a-secure-password",
    });
  });

  it("accepts six-character administrator passwords and rejects shorter values", () => {
    const base = { username: "admin", displayName: "管理员" };
    expect(setupStepSchemas.administrator.safeParse({ ...base, password: "123456" }).success).toBe(true);
    expect(setupStepSchemas.administrator.safeParse({ ...base, password: "12345" }).success).toBe(false);
  });

  it("allows an empty administrator only when reusing an active administrator", () => {
    const input = completeSetupInput();
    input.administrator = { username: "", displayName: "", password: "" };

    expect(validateSetupComplete(input, { reusesActiveAdministrator: true }).success).toBe(true);
    expect(validateSetupComplete(input, { reusesActiveAdministrator: false }).success).toBe(false);
    expect(setupCompleteSchema.safeParse(input).success).toBe(false);
  });

  it("rejects partially filled fallback credentials while reusing an active administrator", () => {
    const input = completeSetupInput();
    input.administrator = { username: "admin", displayName: "", password: "" };

    expect(validateSetupComplete(input, { reusesActiveAdministrator: true }).success).toBe(false);
  });

  it("keeps valid administrator credentials accepted in every completion scenario", () => {
    const input = completeSetupInput();

    expect(validateSetupComplete(input, { reusesActiveAdministrator: true }).success).toBe(true);
    expect(validateSetupComplete(input, { reusesActiveAdministrator: false }).success).toBe(true);
  });

  it("accepts relative and absolute server directories", () => {
    expect(setupStepSchemas.paths.parse({
      migrationsDirectory: "migrations",
      adminWebDirectory: "D:\\XyMusic\\admin",
    })).toEqual({
      migrationsDirectory: "migrations",
      adminWebDirectory: "D:\\XyMusic\\admin",
    });
    expect(setupStepSchemas.media.parse({
      mode: "DIRECTORY",
      directory: "tools",
      ffmpegPath: "",
      ffprobePath: "",
      fpcalcPath: "",
      acoustIdClient: "",
    }).directory).toBe("tools");
  });

  it("supports explicit paths and blank PATH-based advanced configuration", () => {
    expect(setupStepSchemas.media.parse({
      mode: "ADVANCED",
      directory: "",
      ffmpegPath: "tools\\ffmpeg.exe",
      ffprobePath: "D:\\XyMusic\\tools\\ffprobe.exe",
      fpcalcPath: "",
      acoustIdClient: "",
    })).toMatchObject({
      ffmpegPath: "tools\\ffmpeg.exe",
      ffprobePath: "D:\\XyMusic\\tools\\ffprobe.exe",
      fpcalcPath: "",
      acoustIdClient: "",
    });
    expect(setupStepSchemas.media.safeParse({
      mode: "ADVANCED",
      directory: "",
      ffmpegPath: "",
      ffprobePath: "",
      fpcalcPath: "",
      acoustIdClient: "",
    }).success).toBe(true);
    expect(setupStepSchemas.media.safeParse({
      mode: "DIRECTORY",
      directory: "",
      ffmpegPath: "",
      ffprobePath: "",
      fpcalcPath: "",
      acoustIdClient: "",
    }).success).toBe(true);
  });

  it("keeps audio fingerprinting optional but requires both values when enabled", () => {
    const base = {
      mode: "DIRECTORY" as const,
      directory: "tools",
      ffmpegPath: "",
      ffprobePath: "",
    };
    expect(setupStepSchemas.media.safeParse({ ...base, fpcalcPath: "", acoustIdClient: "" }).success).toBe(true);
    expect(setupStepSchemas.media.safeParse({ ...base, fpcalcPath: "tools\\fpcalc.exe", acoustIdClient: "" }).success).toBe(false);
    expect(setupStepSchemas.media.safeParse({ ...base, fpcalcPath: "", acoustIdClient: "xymusic" }).success).toBe(false);
    expect(setupStepSchemas.media.safeParse({
      ...base,
      fpcalcPath: "tools\\fpcalc.exe",
      acoustIdClient: "xymusic",
    }).success).toBe(true);
  });

  it("rejects loopback addresses for both object storage URLs", () => {
    const base = {
      endpoint: "http://minio.example.com:9000",
      region: "us-east-1",
      bucket: "xymusic",
      accessKeyId: "access-key",
      secretAccessKey: "secret-key",
      forcePathStyle: true,
      signedUrlTtlSeconds: 300,
      maxUploadBytes: 1024,
    };
    expect(setupStepSchemas.storage.safeParse(base).success).toBe(true);
    for (const endpoint of ["http://127.0.0.1:9000", "http://127.1.2.3:9000", "http://localhost:9000", "http://[::1]:9000"]) {
      expect(setupStepSchemas.storage.safeParse({ ...base, endpoint }).success).toBe(false);
      expect(setupStepSchemas.storage.safeParse({ ...base, publicBaseUrl: endpoint }).success).toBe(false);
    }
  });
});

function completeSetupInput(): SetupCompleteInput {
  return {
    http: {
      ipv4Host: "0.0.0.0",
      ipv4Port: 3000,
      ipv6Host: "::",
      ipv6Port: 3000,
      trustedProxyAddresses: [],
    },
    paths: {
      migrationsDirectory: "migrations",
      adminWebDirectory: "admin",
    },
    database: {
      host: "db.example.com",
      port: 5432,
      database: "xymusic",
      username: "xymusic",
      password: "secret",
      sslMode: "prefer",
      maxConnections: 10,
    },
    storage: {
      endpoint: "http://minio.example.com:9000",
      region: "us-east-1",
      bucket: "xymusic",
      accessKeyId: "access-key",
      secretAccessKey: "secret-key",
      forcePathStyle: true,
      signedUrlTtlSeconds: 300,
      maxUploadBytes: 1024,
    },
    media: {
      mode: "DIRECTORY",
      directory: "tools",
      ffmpegPath: "",
      ffprobePath: "",
      fpcalcPath: "",
      acoustIdClient: "",
    },
    source: {
      name: "Music",
      directory: "music",
      mode: "READ_ONLY",
      enabled: true,
      syncOnStartup: true,
      scanIntervalMinutes: null,
      includePatterns: [],
      excludePatterns: [],
    },
    registration: { enabled: false },
    administrator: {
      username: "admin",
      displayName: "管理员",
      password: "a-secure-password",
    },
    databaseAction: "migrate",
  };
}
