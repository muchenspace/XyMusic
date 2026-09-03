import { describe, expect, it } from "vitest";
import type { SetupCompleteInput } from "@/features/setup/domain/models";
import {
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

  it("rejects an empty administrator and always requires valid credentials", () => {
    const input = completeSetupInput();
    input.administrator = { username: "", displayName: "", password: "" };

    expect(validateSetupComplete(input).success).toBe(false);
    expect(setupCompleteSchema.safeParse(input).success).toBe(false);
  });

  it("keeps valid administrator credentials accepted in completion", () => {
    const input = completeSetupInput();

    expect(validateSetupComplete(input).success).toBe(true);
  });

  it("only allows reset as storageAction and rejects reuse", () => {
    const input = completeSetupInput();
    input.storageAction = "reset";
    expect(validateSetupComplete(input).success).toBe(true);

    // @ts-expect-error testing invalid action
    input.storageAction = "reuse";
    expect(validateSetupComplete(input).success).toBe(false);

    delete input.storageAction;
    expect(validateSetupComplete(input).success).toBe(true);
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
    }).directory).toBe("tools");
  });

  it("supports explicit paths and blank PATH-based advanced configuration", () => {
    expect(setupStepSchemas.media.parse({
      mode: "ADVANCED",
      directory: "",
      ffmpegPath: "tools\\ffmpeg.exe",
      ffprobePath: "D:\\XyMusic\\tools\\ffprobe.exe",
    })).toMatchObject({
      ffmpegPath: "tools\\ffmpeg.exe",
      ffprobePath: "D:\\XyMusic\\tools\\ffprobe.exe",
    });
    expect(setupStepSchemas.media.safeParse({
      mode: "ADVANCED",
      directory: "",
      ffmpegPath: "",
      ffprobePath: "",
    }).success).toBe(true);
    expect(setupStepSchemas.media.safeParse({
      mode: "DIRECTORY",
      directory: "",
      ffmpegPath: "",
      ffprobePath: "",
    }).success).toBe(true);
  });

  it("validates local media asset and transcode directories", () => {
    const base = {
      assetDirectory: "assets",
      transcodeDirectory: "transcode",
      maxUploadBytes: 1024,
    };
    expect(setupStepSchemas.storage.safeParse(base).success).toBe(true);
    expect(setupStepSchemas.storage.safeParse({ ...base, assetDirectory: "" }).success).toBe(false);
    expect(setupStepSchemas.storage.safeParse({ ...base, transcodeDirectory: "" }).success).toBe(false);
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
      assetDirectory: "assets",
      transcodeDirectory: "transcode",
      maxUploadBytes: 1024,
    },
    media: {
      mode: "DIRECTORY",
      directory: "tools",
      ffmpegPath: "",
      ffprobePath: "",
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
    databaseAction: "reset",
  };
}

