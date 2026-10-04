/**
 * Central query-key factories for the sources feature. Values must stay
 * identical to the keys previously inlined in pages.
 */

export interface SourceListQueryKeyParams {
  page: number;
  pageSize: number;
  cursor: string;
}

export interface SourceScansQueryKeyParams {
  page: number;
  pageSize: number;
  cursor: string;
}

export interface SourceBrowseQueryKeyParams {
  page: number;
  pageSize: number;
  cursor: string;
}

export const sourceQueryKeys = {
  list: (params: SourceListQueryKeyParams) => ["admin", "sources", "list", params] as const,
  sourcesPrefix: ["admin", "sources"] as const,
  scans: (sourceId: string, params: SourceScansQueryKeyParams) => ["admin", "sources", sourceId, "scans", params] as const,
  scansPrefix: (sourceId: string) => ["admin", "sources", sourceId, "scans"] as const,
  browse: (path: string, params: SourceBrowseQueryKeyParams) => ["admin", "sources", "browse", path, params] as const,
};
