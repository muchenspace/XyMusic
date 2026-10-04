import type { SourceScan } from "@/features/sources/domain/models";

/**
 * Cache port for scan progress. The composition root injects an implementation
 * backed by the query cache so application code never imports the query
 * library directly.
 */
export interface ScanCachePort {
  /** Merges a scan snapshot into every cached scan list of that source. */
  upsertScan(sourceId: string, scan: SourceScan): void;
  /** Marks source list and dashboard queries stale. */
  refresh(): Promise<void>;
  /** Marks catalog queries stale after a scan completes. */
  refreshCatalog(): Promise<void>;
  /** Marks the scan history of one source stale. */
  invalidateScans(sourceId: string): Promise<void>;
}
