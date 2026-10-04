import type { PermanentDeleteTracksJob, TrackMetadataRecord } from "@/features/music/domain/models";

/**
 * Cache port used by music application use-cases to read and update cached
 * track data. The composition root injects an implementation backed by the
 * query cache; application code never imports the query library directly.
 */
export interface TrackCachePort {
  /** Reads a cached metadata record for the given track, if present. */
  getMetadataRecord(trackId: string): TrackMetadataRecord | undefined;
  /** Replaces the cached metadata record. */
  setMetadataRecord(record: TrackMetadataRecord): void;
  /**
   * Optimistically advances the cached metadata version of a track: the
   * metadata record and every cached track-list page are updated only when
   * their current version is older than the applied version.
   */
  applyMetadataVersion(trackId: string, version: number): void;
  /** Caches a permanent-delete job snapshot. */
  setPermanentDeleteJob(job: PermanentDeleteTracksJob): void;
  /** Marks music list/detail queries stale after a mutation. */
  invalidateMusicLists(): Promise<void>;
}
