import { ApiError } from "@/shared/application/api-error";
import { randomUuid } from "@/utils/browser-crypto";
import {
  apiConnectionError,
  buildApiUrl,
  createTimeoutSignal,
  isRecord,
  parseJsonResponse,
  readResponseText,
  responseProblem,
  REQUEST_TIMEOUT_MS,
} from "@/api/http-core";

const ADMIN_REFRESH_LOCK_NAME = "xymusic-admin-session-refresh";
const ADMIN_REFRESH_KEY_STORAGE = "xymusic-admin-refresh-idempotency-key";
const IDEMPOTENCY_KEY_PATTERN = /^[A-Za-z0-9._~-]{8,128}$/;
let inMemoryCsrfToken: string | undefined;
let refreshIdempotencyKey: string | undefined;
let refreshPromise: Promise<boolean> | undefined;

export function setCsrfToken(value?: string): void {
  inMemoryCsrfToken = value || undefined;
}

export function resetApiClientAuthState(): void {
  inMemoryCsrfToken = undefined;
  clearRefreshIdempotencyKey();
}

export function csrfToken(): string | undefined {
  return csrfCookieToken() ?? inMemoryCsrfToken;
}

export function csrfCookieToken(): string | undefined {
  const match = document.cookie.match(/(?:^|;\s*)xymusic_admin_csrf=([^;]+)/);
  if (!match) return undefined;
  const value = match[1] ?? "";
  try {
    return decodeURIComponent(value) || undefined;
  } catch {
    return value || undefined;
  }
}

export async function withAdminAuthLock<T>(
  operation: () => Promise<T>,
  fallback: () => Promise<T> = operation,
): Promise<T> {
  if (typeof navigator === "undefined" || !("locks" in navigator) || !navigator.locks) return fallback();
  let lockAcquired = false;
  try {
    return await navigator.locks.request(ADMIN_REFRESH_LOCK_NAME, async () => {
      lockAcquired = true;
      return operation();
    });
  } catch (error) {
    if (!lockAcquired) return fallback();
    throw error;
  }
}

export async function refreshAdminSession(): Promise<boolean> {
  if (refreshPromise) return refreshPromise;
  refreshPromise = coordinatedAdminSessionRefresh()
    .finally(() => { refreshPromise = undefined; });
  return refreshPromise;
}

async function coordinatedAdminSessionRefresh(): Promise<boolean> {
  return withAdminAuthLock(
    async () => {
      if (await adminSessionIsCurrent()) return true;
      return performAdminSessionRefresh();
    },
    performAdminSessionRefresh,
  );
}

async function adminSessionIsCurrent(): Promise<boolean> {
  const timeout = createTimeoutSignal(undefined, REQUEST_TIMEOUT_MS);
  try {
    let response: Response;
    try {
      response = await fetch(buildApiUrl("/api/v1/admin/auth/session"), {
        method: "GET",
        headers: { Accept: "application/json, application/problem+json" },
        credentials: "include",
        signal: timeout.signal,
      });
    } catch (error) {
      throw apiConnectionError(error, timeout.signal);
    }
    if (response.status === 401 || response.status === 403) return false;
    const contentType = response.headers.get("content-type") ?? "";
    const body = await readResponseText(response, timeout.signal);
    if (!response.ok) {
      const payload: unknown = contentType.includes("json") ? parseJsonResponse(body) : body;
      throw new ApiError(responseProblem(response, payload));
    }
    if (response.status !== 200 || !contentType.includes("json")) {
      throw new ApiError({
        title: "服务器响应格式无效",
        status: 502,
        detail: "管理会话探测响应格式无效",
      });
    }
    const payload = parseJsonResponse(body);
    if (!isRecord(payload) || !isRecord(payload.user) || typeof payload.user.id !== "string") {
      throw new ApiError({
        title: "服务器响应格式无效",
        status: 502,
        detail: "管理会话探测响应结构无效",
      });
    }
    const nextToken = typeof payload.csrfToken === "string"
      ? payload.csrfToken
      : response.headers.get("X-CSRF-Token") || csrfCookieToken();
    if (!nextToken) {
      throw new ApiError({
        title: "服务器响应格式无效",
        status: 502,
        detail: "管理会话探测响应缺少 CSRF Token",
      });
    }
    setCsrfToken(nextToken);
    currentRefreshIdempotencyKey();
    return true;
  } finally {
    timeout.cleanup();
  }
}

async function performAdminSessionRefresh(): Promise<boolean> {
  const headers = new Headers({
    Accept: "application/json, application/problem+json",
    "Idempotency-Key": currentRefreshIdempotencyKey(),
  });
  const token = csrfToken();
  if (token) headers.set("X-CSRF-Token", token);
  const timeout = createTimeoutSignal(undefined, REQUEST_TIMEOUT_MS);
  try {
    let response: Response;
    try {
      response = await fetch(buildApiUrl("/api/v1/admin/auth/refresh"), {
        method: "POST",
        headers,
        credentials: "include",
        signal: timeout.signal,
      });
    } catch (error) {
      throw apiConnectionError(error, timeout.signal);
    }
    if (response.status === 401 || response.status === 403) {
      clearRefreshIdempotencyKey();
      return false;
    }
    const contentType = response.headers.get("content-type") ?? "";
    if (response.ok) {
      // A successful response means the refresh cookie was rotated when the
      // headers arrived. Future refreshes must use a new idempotency key.
      rotateRefreshIdempotencyKey();
    }
    const body = await readResponseText(response, timeout.signal);
    if (!response.ok) {
      const payload: unknown = contentType.includes("json") ? parseJsonResponse(body) : body;
      throw new ApiError(responseProblem(response, payload));
    }
    if (!contentType.includes("json")) {
      throw new ApiError({
        title: "服务器响应格式无效",
        status: 502,
        detail: "刷新会话响应不是 JSON",
      });
    }
    let payload: unknown;
    try {
      payload = JSON.parse(body);
    } catch {
      throw new ApiError({
        title: "服务器响应格式无效",
        status: 502,
        detail: "刷新会话响应包含无效 JSON",
      });
    }
    if (typeof payload !== "object" || payload === null) {
      throw new ApiError({
        title: "服务器响应格式无效",
        status: 502,
        detail: "刷新会话响应结构无效",
      });
    }
    const payloadToken = "csrfToken" in payload && typeof payload.csrfToken === "string"
      ? payload.csrfToken
      : undefined;
    const nextToken = payloadToken || response.headers.get("X-CSRF-Token") || undefined;
    if (!nextToken) {
      throw new ApiError({
        title: "服务器响应格式无效",
        status: 502,
        detail: "刷新会话响应缺少 CSRF Token",
      });
    }
    setCsrfToken(nextToken);
    return true;
  } finally {
    timeout.cleanup();
  }
}

export function currentRefreshIdempotencyKey(): string {
  try {
    const stored = window.localStorage.getItem(ADMIN_REFRESH_KEY_STORAGE);
    if (stored && IDEMPOTENCY_KEY_PATTERN.test(stored)) {
      refreshIdempotencyKey = stored;
      return stored;
    }
    const created = refreshIdempotencyKey ?? randomUuid();
    window.localStorage.setItem(ADMIN_REFRESH_KEY_STORAGE, created);
    const shared = window.localStorage.getItem(ADMIN_REFRESH_KEY_STORAGE);
    refreshIdempotencyKey = shared && IDEMPOTENCY_KEY_PATTERN.test(shared) ? shared : created;
  } catch {
    refreshIdempotencyKey ??= randomUuid();
  }
  return refreshIdempotencyKey;
}

export function rotateRefreshIdempotencyKey(): void {
  refreshIdempotencyKey = randomUuid();
  try {
    window.localStorage.setItem(ADMIN_REFRESH_KEY_STORAGE, refreshIdempotencyKey);
  } catch {
    // Storage can be unavailable in restricted browser contexts.
  }
}

export function clearRefreshIdempotencyKey(): void {
  refreshIdempotencyKey = undefined;
  try {
    window.localStorage.removeItem(ADMIN_REFRESH_KEY_STORAGE);
  } catch {
    // Storage can be unavailable in restricted browser contexts.
  }
}
