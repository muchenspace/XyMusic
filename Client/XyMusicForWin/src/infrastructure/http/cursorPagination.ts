import { ApiError } from "./ApiClient";
import type { CursorPage } from "../../domain/pagination";
import type { PageDto } from "./musicDtos";

type PageLoader<T> = (cursor: string) => Promise<PageDto<T>>;

export async function collectPages<T>(
  firstPath: string,
  request: (path: string) => Promise<PageDto<T>>,
  signal?: AbortSignal,
): Promise<T[]> {
  const first = await request(firstPath);
  return collectContinuation(first, (cursor) => request(withCursor(firstPath, cursor)), signal);
}

export async function collectContinuation<T>(
  firstPage: PageDto<T> | null | undefined,
  loadPage: PageLoader<T>,
  signal?: AbortSignal,
  invalidMessage = "分页响应缺少数据列表",
): Promise<T[]> {
  const first = validPage(firstPage, invalidMessage);
  const items = [...first.items];
  const seenCursors = new Set<string>();
  let cursor = validCursor(first.nextCursor);
  let requests = 1;
  guardItemCount(items.length);

  while (cursor) {
    throwIfAborted(signal);
    guardCursor(cursor, seenCursors, requests, items.length);
    const page = validPage(await loadPage(cursor), invalidMessage);
    items.push(...page.items);
    requests += 1;
    guardItemCount(items.length);
    cursor = validCursor(page.nextCursor);
  }
  return items;
}

export function withCursor(path: string, cursor: string): string {
  const url = new URL(path, PAGINATION_BASE_URL);
  url.searchParams.set("cursor", cursor);
  return `${url.pathname.replace(/^\//, "")}${url.search}`;
}

export function paginationError(message: string): ApiError {
  return new ApiError(message, 0, "INVALID_PAGINATION");
}

export function mapCursorPage<TDto, TDomain>(
  page: PageDto<TDto> | null | undefined,
  mapper: (value: TDto, index: number) => TDomain,
  invalidMessage = "分页响应缺少数据列表",
): CursorPage<TDomain> {
  const valid = validPage(page, invalidMessage);
  return { items: valid.items.map(mapper), nextCursor: validCursor(valid.nextCursor) };
}

export function normalizePageLimit(value: number, fallback = 50, maximum = 100): number {
  if (!Number.isFinite(value)) return fallback;
  return Math.max(1, Math.min(maximum, Math.round(value)));
}

function validPage<T>(page: PageDto<T> | null | undefined, message: string): PageDto<T> {
  if (!page || !Array.isArray(page.items)) throw paginationError(message);
  return page;
}

function validCursor(value: unknown): string | null {
  if (value == null || value === "") return null;
  if (typeof value !== "string") throw paginationError("分页游标格式无效");
  return value;
}

function guardCursor(cursor: string, seen: Set<string>, requests: number, itemCount: number): void {
  if (seen.has(cursor)) throw paginationError("服务器返回了重复的分页游标");
  if (requests >= MAX_PAGE_REQUESTS || itemCount >= MAX_PAGINATED_ITEMS) throw paginationError("分页数据超过客户端安全上限");
  seen.add(cursor);
}

function guardItemCount(itemCount: number): void {
  if (itemCount > MAX_PAGINATED_ITEMS) throw paginationError("分页数据超过客户端安全上限");
}

function throwIfAborted(signal?: AbortSignal): void {
  if (signal?.aborted) throw signal.reason ?? new DOMException("请求已取消", "AbortError");
}

const PAGINATION_BASE_URL = "https://pagination.invalid/";
const MAX_PAGE_REQUESTS = 100;
const MAX_PAGINATED_ITEMS = 10_000;
