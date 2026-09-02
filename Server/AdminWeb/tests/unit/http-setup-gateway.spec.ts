import { beforeEach, describe, expect, it, vi } from "vitest";
import type { SetupCompleteInput } from "@/features/setup/domain/models";

const apiRequest = vi.hoisted(() => vi.fn());
vi.mock("@/api/client", () => ({ apiRequest }));

import {
  HttpSetupGateway,
  normalizeMediaConfig,
  SETUP_COMPLETION_TIMEOUT_MS,
} from "@/features/setup/infrastructure/http-setup-gateway";

describe("HTTP setup gateway", () => {
  beforeEach(() => apiRequest.mockReset());

  it("normalizes directory mode media config", () => {
    expect(normalizeMediaConfig({
      mode: "DIRECTORY",
      directory: " tools ",
      ffmpegPath: "",
      ffprobePath: "",
    })).toEqual({ directory: "tools" });
  });

  it("normalizes advanced FFmpeg paths", () => {
    expect(normalizeMediaConfig({
      mode: "ADVANCED",
      directory: "",
      ffmpegPath: " tools\\ffmpeg.exe ",
      ffprobePath: " D:\\XyMusic\\tools\\ffprobe.exe ",
    })).toEqual({
      ffmpegPath: "tools\\ffmpeg.exe",
      ffprobePath: "D:\\XyMusic\\tools\\ffprobe.exe",
    });
  });

  it("uses the extended timeout for initialization completion", async () => {
    apiRequest.mockResolvedValue({ configured: true });
    const input = setupInput();

    await new HttpSetupGateway().complete(input);

    expect(apiRequest).toHaveBeenCalledWith("/api/setup/complete", expect.objectContaining({
      method: "POST",
      timeoutMs: SETUP_COMPLETION_TIMEOUT_MS,
    }));
    expect(SETUP_COMPLETION_TIMEOUT_MS).toBe(180_000);
  });
});

function setupInput(): SetupCompleteInput {
  return {
    http: { ipv4Host: "0.0.0.0", ipv4Port: 3000, ipv6Host: "::", ipv6Port: 3000, trustedProxyAddresses: [] },
    paths: { migrationsDirectory: "migrations", adminWebDirectory: "admin" },
    database: {
      host: "127.0.0.1",
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
    administrator: { username: "admin", displayName: "Admin", password: "a-secure-password" },
  };
}
