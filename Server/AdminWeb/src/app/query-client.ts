import { QueryCache, QueryClient, type VueQueryPluginOptions } from "@tanstack/vue-query";
import { ApiConnectionError, ApiError, apiErrorMessage } from "@/shared/application/api-error";

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
