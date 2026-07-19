import { describe, expect, it, vi } from "vitest";
import type { MusicAdminGateway } from "@/features/music/application/music-admin-gateway";
import { MusicAdminUseCases } from "@/features/music/application/music-admin-use-cases";
import type { PermanentDeleteTracksJob, TrackSummary } from "@/features/music/domain/models";

function track(id: string, status: TrackSummary["status"] = "ARCHIVED", version = 1): TrackSummary {
  return {
    id,
    title: id,
    artistCredits: [],
    artists: [],
    album: null,
    artwork: null,
    durationMs: 1,
    trackNumber: null,
    discNumber: 1,
    status,
    audioStatus: status,
    metadataStatus: "ORIGINAL",
    metadataVersion: 1,
    source: null,
    mediaProcessing: null,
    variantSummary: [],
    activeWritebackJobId: null,
    publishedAt: null,
    createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-01T00:00:00Z",
    version,
  };
}

function job(): PermanentDeleteTracksJob {
  return {
    id: "job-1",
    status: "PENDING",
    total: 1,
    processed: 0,
    succeeded: 0,
    failed: 0,
    createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-01T00:00:00Z",
    startedAt: null,
    completedAt: null,
    items: [],
  };
}

describe("track batch actions", () => {
  it("maps archived track snapshots to optimistic-lock targets", async () => {
    const restore = vi.fn().mockResolvedValue({ restored: 2, items: [] });
    const createDelete = vi.fn().mockResolvedValue(job());
    const gateway = { batchRestoreTracks: restore, createPermanentDeleteTracksJob: createDelete } as unknown as MusicAdminGateway;
    const useCases = new MusicAdminUseCases(gateway);
    const tracks = [track("track-1", "ARCHIVED", 3), track("track-2", "ARCHIVED", 5)];

    await useCases.batchRestoreTracks(tracks);
    await useCases.createPermanentDeleteTracksJob(tracks);

    const targets = [{ trackId: "track-1", expectedVersion: 3 }, { trackId: "track-2", expectedVersion: 5 }];
    expect(restore).toHaveBeenCalledWith(targets);
    expect(createDelete).toHaveBeenCalledWith(targets);
  });

  it("rejects empty, mixed, duplicate, or oversized destructive selections before HTTP", () => {
    const gateway = { batchRestoreTracks: vi.fn(), createPermanentDeleteTracksJob: vi.fn() } as unknown as MusicAdminGateway;
    const useCases = new MusicAdminUseCases(gateway);

    expect(() => useCases.batchRestoreTracks([])).toThrow("请先选择曲目");
    expect(() => useCases.batchRestoreTracks([track("active", "READY")])).toThrow("不是已归档状态");
    expect(() => useCases.createPermanentDeleteTracksJob([track("same"), track("same")])).toThrow("被重复选择");
    expect(() => useCases.createPermanentDeleteTracksJob(Array.from({ length: 201 }, (_, index) => track(`track-${index}`)))).toThrow("一次最多处理 200 首曲目");
  });

  it("loads a persisted delete job by id", async () => {
    const load = vi.fn().mockResolvedValue(job());
    const useCases = new MusicAdminUseCases({ getPermanentDeleteTracksJob: load } as unknown as MusicAdminGateway);

    await expect(useCases.getPermanentDeleteTracksJob("job-1")).resolves.toEqual(job());
    expect(load).toHaveBeenCalledWith("job-1", undefined);
    expect(() => useCases.getPermanentDeleteTracksJob(" ")).toThrow("任务 ID 无效");
  });
});
