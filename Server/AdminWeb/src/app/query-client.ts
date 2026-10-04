import { QueryCache, QueryClient, type VueQueryPluginOptions } from "@tanstack/vue-query";
import { appQueryKeys } from "@/app/query-keys";
import { ApiConnectionError, ApiError, apiErrorMessage } from "@/shared/application/api-error";
import type { RuntimeSettings } from "@/features/settings/domain/models";
import type { TrackCachePort } from "@/features/music/application/track-cache-port";
import type { ScanCachePort } from "@/features/sources/application/scan-cache-port";
import type { PermanentDeleteTracksJob, TrackMetadataRecord, TrackSummary } from "@/features/music/domain/models";
import type { SourceScan, SourceScanPage } from "@/features/sources/domain/models";

export const ADMIN_QUERY_ERROR_EVENT = "xymusic:query-error";

export function shouldRetryQuery(failureCount: number, error: unknown): boolean {
  if (error instanceof ApiConnectionError && error.kind === "aborted") return false;
  if (error instanceof ApiError && error.status < 500) return false;
  return failureCount < 2;
}

export function shouldNotifyQueryError(cachedData: unknown): boolean {
  return cachedData !== undefined;
}

export const queryClient = new QueryClient({
  queryCache: new QueryCache({
    onError: (error, query) => {
      if (!shouldNotifyQueryError(query.state.data)) return;
      window.dispatchEvent(new CustomEvent(ADMIN_QUERY_ERROR_EVENT, {
        detail: apiErrorMessage(error, "数据加载失败，请稍后重试。"),
      }));
    },
  }),
  defaultOptions: {
    queries: {
      staleTime: 60_000,
      // Large cursor pages are intentionally discarded quickly when no page
      // is visible; retaining many 10k/100k-row responses for five minutes
      // can otherwise consume hundreds of MB in the browser.
      gcTime: 60_000,
      refetchOnWindowFocus: false,
      retry: shouldRetryQuery,
    },
    mutations: { retry: false },
  },
});

export const vueQueryPluginOptions: VueQueryPluginOptions = { queryClient };

/**
 * Music mutations can change the membership and counts of several catalog
 * views at once (for example archiving the last track of an album). Keep the
 * invalidation scope in one place so every mutation observes the same data
 * contract.
 */
const ADMIN_MUSIC_LIST_QUERY_PREFIXES = [
  ["admin", "tracks"],
  ["admin", "albums"],
  ["admin", "artists"],
] as const;

const ADMIN_MUSIC_ACTIVE_QUERY_PREFIXES = [
  ["admin", "track"],
  ["admin", "album"],
  ["admin", "dashboard"],
] as const;

export async function invalidateAdminMusicQueries(): Promise<void> {
  await Promise.all([
    // Mark inactive cursor pages stale without refetching every page the
    // operator has visited. Only the visible page needs an immediate refresh.
    ...ADMIN_MUSIC_LIST_QUERY_PREFIXES.map((queryKey) =>
      queryClient.invalidateQueries({ queryKey, refetchType: "active" }),
    ),
    // A detail can legitimately disappear after archiving its last track; mark inactive details stale without fetching them.
    ...ADMIN_MUSIC_ACTIVE_QUERY_PREFIXES.map((queryKey) =>
      queryClient.invalidateQueries({ queryKey, refetchType: "active" }),
    ),
  ]);
}

export async function clearAdminQueryCache(): Promise<void> {
  await queryClient.cancelQueries();
  queryClient.clear();
}

/**
 * Semantically named cache writes used after a scrape applies a new metadata
 * version: advance the cached version on the metadata record and on every
 * cached track-list page, without touching entries whose version is newer.
 */
export function applyTrackMetadataVersion(trackId: string, version: number): void {
  const cachedMetadataKey = ["admin", "track", trackId, "metadata"];
  queryClient.setQueryData<TrackMetadataRecord>(cachedMetadataKey, (current) => {
    if (!current || current.trackId !== trackId || current.version >= version) return current;
    return { ...current, version };
  });

  for (const [queryKey, current] of queryClient.getQueriesData<{ items: TrackSummary[] }>({ queryKey: ["admin", "tracks"] })) {
    const isTrackList = queryKey[0] === "admin" && queryKey[1] === "tracks"
      && typeof queryKey[2] === "object" && queryKey[2] !== null && !Array.isArray(queryKey[2]);
    if (!isTrackList || !current || !Array.isArray(current.items)) continue;
    queryClient.setQueryData(queryKey, {
      ...current,
      items: current.items.map((track) => track.id === trackId && (track.metadataVersion === null || track.metadataVersion < version)
        ? { ...track, metadataVersion: version }
        : track),
    });
  }
}

/** Query-cache implementation of the music application cache port. */
export const trackCachePort: TrackCachePort = {
  getMetadataRecord(trackId: string): TrackMetadataRecord | undefined {
    return queryClient.getQueryData<TrackMetadataRecord>(["admin", "track", trackId, "metadata"]);
  },
  setMetadataRecord(record: TrackMetadataRecord): void {
    queryClient.setQueryData(["admin", "track", record.trackId, "metadata"], record);
  },
  applyMetadataVersion(trackId: string, version: number): void {
    applyTrackMetadataVersion(trackId, version);
  },
  setPermanentDeleteJob(job: PermanentDeleteTracksJob): void {
    queryClient.setQueryData(["admin", "tracks", "permanent-delete", job.id], job);
  },
  invalidateMusicLists(): Promise<void> {
    return invalidateAdminMusicQueries();
  },
};

/** Writes the freshly saved settings snapshot into the settings query cache. */
export function applyAdminSettings(settings: RuntimeSettings): void {
  queryClient.setQueryData(appQueryKeys.adminSettings, settings);
}

/** Marks queries that depend on applied runtime settings stale, then refreshes service readiness. */
export async function invalidateAdminSettingsDependents(): Promise<void> {
  await Promise.all([
    queryClient.invalidateQueries({ predicate: (query) => query.queryKey[0] === "admin" && query.queryKey[1] !== "settings" }),
    queryClient.invalidateQueries({ queryKey: ["service", "readiness"] }),
  ]);
}

/** Marks user list and dashboard queries stale. */
export async function invalidateAdminUserQueries(): Promise<void> {
  await Promise.all([
    queryClient.invalidateQueries({ queryKey: ["admin", "users"] }),
    queryClient.invalidateQueries({ queryKey: ["admin", "dashboard"] }),
  ]);
}

/** Marks job list and dashboard queries stale after job mutations. */
export async function invalidateAdminJobQueries(): Promise<void> {
  await Promise.all([
    queryClient.invalidateQueries({ queryKey: ["admin", "jobs"] }),
    queryClient.invalidateQueries({ queryKey: ["admin", "dashboard"] }),
  ]);
}

/** Marks job list, writeback list and dashboard queries stale (SSE refresh path). */
export async function invalidateAdminJobEventQueries(): Promise<void> {
  await Promise.all([
    queryClient.invalidateQueries({ queryKey: ["admin", "jobs"] }),
    queryClient.invalidateQueries({ queryKey: ["admin", "metadata-writeback-jobs"] }),
    queryClient.invalidateQueries({ queryKey: ["admin", "dashboard"] }),
  ]);
}

/** Merges an SSE scan snapshot into every cached scan list of that source. */
export function upsertCachedScan(sourceId: string, scan: SourceScan): void {
  queryClient.setQueriesData<SourceScanPage>({ queryKey: ["admin", "sources", sourceId, "scans"] }, (current) => {
    if (!current) return current;
    const found = current.items.some((item) => item.id === scan.id);
    return {
      ...current,
      items: found
        ? current.items.map((item) => item.id === scan.id ? scan : item)
        : current.page === 1
          ? [scan, ...current.items].slice(0, current.pageSize)
          : current.items,
      total: found || current.page !== 1 ? current.total : current.total + 1,
    };
  });
}

/** Marks source list and dashboard queries stale. */
export async function invalidateAdminSourceQueries(): Promise<void> {
  await Promise.all([
    queryClient.invalidateQueries({ queryKey: ["admin", "sources"] }),
    queryClient.invalidateQueries({ queryKey: ["admin", "dashboard"] }),
  ]);
}

/** Marks catalog queries stale after a scan finishes. */
export async function invalidateAdminCatalogQueries(): Promise<void> {
  await Promise.all([
    invalidateAdminSourceQueries(),
    queryClient.invalidateQueries({ queryKey: ["admin", "tracks"] }),
    queryClient.invalidateQueries({ queryKey: ["admin", "track"] }),
    queryClient.invalidateQueries({ queryKey: ["admin", "albums"] }),
    queryClient.invalidateQueries({ queryKey: ["admin", "artists"] }),
    queryClient.invalidateQueries({ queryKey: ["admin", "jobs"] }),
  ]);
}

/** Query-cache implementation of the sources application scan cache port. */
export const scanCachePort: ScanCachePort = {
  upsertScan(sourceId: string, scan: SourceScan): void {
    upsertCachedScan(sourceId, scan);
  },
  refresh(): Promise<void> {
    return invalidateAdminSourceQueries();
  },
  refreshCatalog(): Promise<void> {
    return invalidateAdminCatalogQueries();
  },
  invalidateScans(sourceId: string): Promise<void> {
    return queryClient.invalidateQueries({ queryKey: ["admin", "sources", sourceId, "scans"] });
  },
};
