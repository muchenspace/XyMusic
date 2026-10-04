import type { MusicAdminGateway } from "@/features/music/application/music-admin-gateway";
import type { TrackCachePort } from "@/features/music/application/track-cache-port";
import type { TrackMetadataRecord, TrackTagPatch } from "@/features/music/domain/models";
import { assertWritebackAllowed, sourceWritebackCapability } from "@/features/music/domain/writeback-capability";

export interface SaveTrackMetadataInput {
  record: TrackMetadataRecord;
  patch: TrackTagPatch;
  requestedWriteBack: boolean;
}

export interface SaveTrackMetadataResult {
  record: TrackMetadataRecord;
  writeBackRequested: boolean;
  /** Music list/detail invalidation started after the cache write; callers may await it. */
  refresh: Promise<void>;
}

/**
 * Raised when the Tag update was persisted but creating the writeback job
 * failed. The saved record and the refresh promise are exposed so the page can
 * restore its editor state without re-reading the gateway.
 */
export class TrackWritebackFailedError extends Error {
  readonly saved: TrackMetadataRecord;
  readonly refresh: Promise<void>;

  constructor(saved: TrackMetadataRecord, refresh: Promise<void>, cause: unknown) {
    super(`Tag 已保存，但写回任务创建失败：${cause instanceof Error ? cause.message : "未知错误"}`);
    this.name = "TrackWritebackFailedError";
    this.saved = saved;
    this.refresh = refresh;
  }
}

/**
 * Persists a Track metadata patch and optionally creates the writeback job.
 * When the writeback request fails after a successful save, the saved record
 * is kept in the cache and the failure is surfaced as TrackWritebackFailedError.
 */
export class SaveTrackMetadataUseCase {
  constructor(
    private readonly gateway: MusicAdminGateway,
    private readonly cache: TrackCachePort,
  ) {}

  async execute(input: SaveTrackMetadataInput): Promise<SaveTrackMetadataResult> {
    const { record, patch, requestedWriteBack } = input;
    assertWritebackAllowed(requestedWriteBack, sourceWritebackCapability(record.source));
    const changed = Object.keys(patch).length > 0;
    if (!changed && !requestedWriteBack) throw new Error("没有需要保存的 Tag 变化");
    const saved = changed
      ? await this.gateway.updateTrackMetadata(record.trackId, { expectedVersion: record.version, patch })
      : record;
    if (requestedWriteBack) {
      try {
        assertWritebackAllowed(true, sourceWritebackCapability(saved.source));
        await this.gateway.writeTrackMetadata(saved.trackId, saved.version);
      } catch (error) {
        this.cache.setMetadataRecord(saved);
        throw new TrackWritebackFailedError(saved, this.cache.invalidateMusicLists(), error);
      }
    }
    this.cache.setMetadataRecord(saved);
    return { record: saved, writeBackRequested: requestedWriteBack, refresh: this.cache.invalidateMusicLists() };
  }
}
