import { describe, expect, it, vi } from "vitest";
import { SaveTrackMetadataUseCase, TrackWritebackFailedError } from "@/features/music/application/track-metadata-save";
import type { MusicAdminGateway } from "@/features/music/application/music-admin-gateway";
import type { TrackCachePort } from "@/features/music/application/track-cache-port";
import type { TrackMetadataRecord } from "@/features/music/domain/models";

function record(overrides: Partial<TrackMetadataRecord> = {}): TrackMetadataRecord {
  return {
    trackId: "track-1",
    raw: {} as TrackMetadataRecord["raw"],
    overrides: {},
    effective: { title: "Track", lyrics: null, hasArtwork: false } as TrackMetadataRecord["effective"],
    overriddenFields: [],
    source: { id: "source-1", rootId: "root-1", relativePath: "a.flac", status: "READY", checksumSha256: null, mode: "READ_WRITE", canWriteBack: true, writebackBlockReason: null },
    version: 3,
    lastScannedAt: null,
    updatedBy: null,
    createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

function cache(): TrackCachePort & { saved: TrackMetadataRecord[]; refreshes: number } {
  const saved: TrackMetadataRecord[] = [];
  return {
    saved,
    refreshes: 0,
    getMetadataRecord: () => undefined,
    setMetadataRecord(value) { saved.push(value); },
    applyMetadataVersion: vi.fn(),
    setPermanentDeleteJob: vi.fn(),
    invalidateMusicLists() { this.refreshes += 1; return Promise.resolve(); },
  };
}

describe("SaveTrackMetadataUseCase", () => {
  it("saves the patch, caches the record and refreshes lists", async () => {
    const updateTrackMetadata = vi.fn().mockResolvedValue(record({ version: 4 }));
    const writeTrackMetadata = vi.fn();
    const port = cache();
    const useCase = new SaveTrackMetadataUseCase(
      { updateTrackMetadata, writeTrackMetadata } as unknown as MusicAdminGateway,
      port,
    );

    const result = await useCase.execute({ record: record(), patch: { title: "New" }, requestedWriteBack: false });

    expect(updateTrackMetadata).toHaveBeenCalledWith("track-1", { expectedVersion: 3, patch: { title: "New" } });
    expect(writeTrackMetadata).not.toHaveBeenCalled();
    expect(port.saved).toEqual([result.record]);
    expect(port.refreshes).toBe(1);
  });

  it("keeps the saved record and reports a writeback failure", async () => {
    const updateTrackMetadata = vi.fn().mockResolvedValue(record({ version: 4 }));
    const writeTrackMetadata = vi.fn().mockRejectedValue(new Error("boom"));
    const port = cache();
    const useCase = new SaveTrackMetadataUseCase(
      { updateTrackMetadata, writeTrackMetadata } as unknown as MusicAdminGateway,
      port,
    );

    const error = await useCase.execute({ record: record(), patch: { title: "New" }, requestedWriteBack: true }).catch((cause: unknown) => cause);

    expect(error).toBeInstanceOf(TrackWritebackFailedError);
    expect((error as Error).message).toBe("Tag 已保存，但写回任务创建失败：boom");
    expect(port.saved).toHaveLength(1);
    expect(port.refreshes).toBe(1);
  });

  it("requires a change or a requested writeback", async () => {
    const updateTrackMetadata = vi.fn();
    const useCase = new SaveTrackMetadataUseCase(
      { updateTrackMetadata } as unknown as MusicAdminGateway,
      cache(),
    );

    await expect(useCase.execute({ record: record(), patch: {}, requestedWriteBack: false }))
      .rejects.toThrow("没有需要保存的 Tag 变化");
    expect(updateTrackMetadata).not.toHaveBeenCalled();
  });

  it("blocks a requested writeback on a read-only source", async () => {
    const updateTrackMetadata = vi.fn();
    const readOnly = record({ source: { id: "s", rootId: null, relativePath: "a.flac", status: "READY", checksumSha256: null, mode: "READ_ONLY", canWriteBack: false, writebackBlockReason: "只读" } });
    const useCase = new SaveTrackMetadataUseCase(
      { updateTrackMetadata } as unknown as MusicAdminGateway,
      cache(),
    );

    await expect(useCase.execute({ record: readOnly, patch: { title: "New" }, requestedWriteBack: true }))
      .rejects.toThrow("只读");
    expect(updateTrackMetadata).not.toHaveBeenCalled();
  });
});
