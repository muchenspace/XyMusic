/**
 * Canonical cursor/offset pagination contracts shared by every feature.
 * Feature domains keep their existing names as aliases so current imports
 * continue to work without a repository-wide rename.
 */
export interface Page<T> {
  items: T[];
  page: number;
  pageSize: number;
  total: number;
  totalPages?: number;
  nextCursor?: string;
}

export interface PageQuery {
  page?: number;
  pageSize?: number;
  search?: string;
  sort?: string;
  order?: "asc" | "desc";
  cursor?: string;
  cursorMode?: "cursor" | "offset";
}
