import { ApiError } from "@/shared/application/api-error";
import { randomUuid } from "@/utils/browser-crypto";
import {
  apiConnectionError,
  buildApiUrl,
  createTimeoutSignal,
  parseJsonResponse,
  readResponseText,
  responseProblem,
  REQUEST_TIMEOUT_MS,
  type QueryValue,
} from "@/api/http-core";
import {
  clearRefreshIdempotencyKey,
  csrfCookieToken,
  csrfToken,
  currentRefreshIdempotencyKey,
  refreshAdminSession,
  rotateRefreshIdempotencyKey,
  setCsrfToken,
  withAdminAuthLock,
} from "@/api/auth-session";
import { publishAdminAuthChange } from "@/api/auth-sync";

const ADMIN_LOGIN_PATH = "/api/v1/admin/auth/login";
const ADMIN_SESSION_PATH = "/api/v1/admin/auth/session";
const ADMIN_LOGOUT_PATH = "/api/v1/admin/auth/logout";

export interface RequestOptions extends Omit<RequestInit, "body"> {
  query?: Record<string, QueryValue>;
  body?: unknown;
  timeoutMs?: number;
  skipAuthRefresh?: boolean;
  skipAuthCoordination?: boolean;
}

export async function apiRequest<T>(path: string, options: RequestOptions = {}): Promise<T> {
  if (!options.skipAuthCoordination && (path === ADMIN_LOGIN_PATH || path === ADMIN_LOGOUT_PATH)) {
    return withAdminAuthLock(() => {
      const cookieToken = csrfCookieToken();
      if (cookieToken) setCsrfToken(cookieToken);
      return apiRequest<T>(path, { ...options, skipAuthCoordination: true });
    });
  }
  const headers = new Headers(options.headers);
  headers.set("Accept", "application/json, application/problem+json");
  const method = (options.method ?? "GET").toUpperCase();
  const hasBody = options.body !== undefined;
  if (hasBody && !(options.body instanceof FormData)) headers.set("Content-Type", "application/json");
  if (!["GET", "HEAD", "OPTIONS"].includes(method)) {
    const token = csrfToken();
    if (token) headers.set("X-CSRF-Token", token);
    if (!headers.has("Idempotency-Key")) headers.set("Idempotency-Key", randomUuid());
  }

  const {
    query,
    body,
    timeoutMs = REQUEST_TIMEOUT_MS,
    skipAuthRefresh,
    skipAuthCoordination: _skipAuthCoordination,
    ...requestInit
  } = options;
  const timeout = createTimeoutSignal(requestInit.signal, timeoutMs);
  try {
    let response: Response;
    try {
      response = await fetch(buildApiUrl(path, query), {
        ...requestInit,
        signal: timeout.signal,
        method,
        headers,
        credentials: "include",
        body: hasBody
          ? body instanceof FormData
            ? body
            : JSON.stringify(body)
          : undefined,
      });
    } catch (error) {
      throw apiConnectionError(error, timeout.signal);
    }

    const responseCsrfToken = response.headers.get("X-CSRF-Token");
    if (responseCsrfToken) setCsrfToken(responseCsrfToken);
    if (response.ok && path === ADMIN_LOGIN_PATH) {
      rotateRefreshIdempotencyKey();
      publishAdminAuthChange();
    }
    else if (response.ok && path === ADMIN_LOGOUT_PATH) {
      clearRefreshIdempotencyKey();
      publishAdminAuthChange();
    }
    else if (response.ok && path === ADMIN_SESSION_PATH) currentRefreshIdempotencyKey();

    if (response.status === 204) return undefined as T;
    const contentType = response.headers.get("content-type") ?? "";
    const isJson = contentType.includes("json");
    const protectedUnauthorized = response.status === 401 && path.startsWith("/api/v1/admin/") &&
      ![ADMIN_LOGIN_PATH, ADMIN_SESSION_PATH, "/api/v1/admin/auth/refresh", ADMIN_LOGOUT_PATH].includes(path);
    if (protectedUnauthorized && !skipAuthRefresh && await refreshAdminSession()) {
      void response.body?.cancel();
      const retryHeaders = new Headers(options.headers);
      const idempotencyKey = headers.get("Idempotency-Key");
      if (idempotencyKey) retryHeaders.set("Idempotency-Key", idempotencyKey);
      return apiRequest<T>(path, { ...options, headers: retryHeaders, skipAuthRefresh: true });
    }
    if (protectedUnauthorized) {
      setCsrfToken();
      window.dispatchEvent(new CustomEvent("xymusic:unauthorized"));
    }

    const responseBody = await readResponseText(response, timeout.signal);
    const payload: unknown = isJson ? parseJsonResponse(responseBody) : responseBody;
    if (!response.ok) {
      const problem = responseProblem(response, payload);
      throw new ApiError({ ...problem, status: response.status });
    }
    return resolveApiResourceUrls(payload) as T;
  } finally {
    timeout.cleanup();
  }
}

const API_BASE = (import.meta.env.VITE_API_BASE_URL ?? "").replace(/\/$/, "");

function apiResourceOrigin(): string {
  try {
    return new URL(API_BASE || window.location.origin, window.location.origin).origin;
  } catch {
    return window.location.origin;
  }
}

/**
 * Recursively resolves server-relative `/api/v1/...` strings in a response
 * payload to absolute URLs on the API origin.
 *
 * This is an intentional, explicit transport contract: every admin response is
 * normalized once here so artwork, stream and download URLs embedded at any
 * depth work regardless of the admin console's own origin. The same fields
 * that were rewritten before continue to be rewritten; only relative API
 * paths are affected and other strings pass through unchanged.
 */
export function resolveApiResourceUrls<T>(value: T): T {
  if (typeof value === "string") {
    return (value.startsWith("/api/v1/")
      ? new URL(value, apiResourceOrigin()).toString()
      : value) as T;
  }
  if (Array.isArray(value)) {
    return value.map((item) => resolveApiResourceUrls(item)) as T;
  }
  if (value && typeof value === "object") {
    return Object.fromEntries(
      Object.entries(value).map(([key, item]) => [key, resolveApiResourceUrls(item)]),
    ) as T;
  }
  return value;
}
