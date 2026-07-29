import { afterEach, describe, expect, it, vi } from "vitest";

import { adminApi } from "@/api/admin";
import type { TrackMetadataRecord } from "@/features/music/domain/models";
import { HttpMusicAdminGateway } from "@/features/music/infrastructure/http-music-admin-gateway";

describe("music metadata timing contract", () => {
  afterEach(() => vi.restoreAllMocks());

  it("rejects a lyric resource whose timing marker is missing", async () => {
    const lyrics = { content: "[00:01.00]line", format: "LRC", language: "und" };
    const response = {
      raw: { lyrics },
      overrides: {},
      effective: { lyrics },
    } as unknown as TrackMetadataRecord;
    vi.spyOn(adminApi, "trackMetadata").mockResolvedValue(response);

    await expect(new HttpMusicAdminGateway().getTrackMetadata("track-1"))
      .rejects.toThrow("lyrics timing is invalid");
  });

  it("rejects an invalid timing marker returned after an update", async () => {
    const response = metadataResponseWithLyricsTiming("UNKNOWN");
    vi.spyOn(adminApi, "updateTrackMetadata").mockResolvedValue(response);

    await expect(new HttpMusicAdminGateway().updateTrackMetadata("track-1", {
      expectedVersion: 1,
      patch: {},
      reason: "test",
    })).rejects.toThrow("lyrics timing is invalid");
  });

  it("rejects an invalid timing marker returned after restoring a revision", async () => {
    const response = metadataResponseWithLyricsTiming("UNKNOWN");
    vi.spyOn(adminApi, "restoreTagRevision").mockResolvedValue(response);

    await expect(new HttpMusicAdminGateway().restoreTagRevision("track-1", "revision-1", 1, "test"))
      .rejects.toThrow("lyrics timing is invalid");
  });
});

function metadataResponseWithLyricsTiming(timing: string): TrackMetadataRecord {
  const lyrics = { content: "[00:01.00]line", format: "LRC", language: "und", timing };
  return {
    raw: { lyrics },
    overrides: {},
    effective: { lyrics },
  } as unknown as TrackMetadataRecord;
}
