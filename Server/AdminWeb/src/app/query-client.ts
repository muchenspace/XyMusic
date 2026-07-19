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
      refetchOnWindowFocus: false,
      retry: shouldRetryQuery,
    },
    mutations: { retry: false },
  },
});

export const vueQueryPluginOptions: VueQueryPluginOptions = { queryClient };

export async function clearAdminQueryCache(): Promise<void> {
  await queryClient.cancelQueries();
  queryClient.clear();
}
