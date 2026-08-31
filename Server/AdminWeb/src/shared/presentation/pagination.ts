export const PAGE_SIZE_OPTIONS = [50, 100, 200, 500, 1_000, 5_000, 10_000, 100_000] as const;

export const DEFAULT_PAGE_SIZE: number = PAGE_SIZE_OPTIONS[0];
export const DEFAULT_CATALOG_PAGE_SIZE: number = PAGE_SIZE_OPTIONS[1];
