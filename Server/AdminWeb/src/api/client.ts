import {
  ApiConnectionError,
  ApiError,
  type ProblemDetails,
} from "@/shared/application/api-error";
import { randomUuid } from "@/utils/browser-crypto";

export {
  ApiConnectionError,
  ApiError,
  apiErrorMessage,
  type ApiConnectionFailure,
} from "@/shared/application/api-error";

const API_BASE = (import.meta.env.VITE_API_BASE_URL ?? "").replace(/\/$/, "");
const REQUEST_TIMEOUT_MS = 20_000;
const UPLOAD_TIMEOUT_MS = 120_000;
const ADMIN_REFRESH_LOCK_NAME = "xymusic-admin-session-refresh";
const ADMIN_REFRESH_KEY_STORAGE = "xymusic-admin-refresh-idempotency-key";
export const ADMIN_AUTH_SYNC_STORAGE_KEY = "xymusic-admin-auth-sync";
const IDEMPOTENCY_KEY_PATTERN = /^[A-Za-z0-9._~-]{8,128}$/;
let inMemoryCsrfToken: string | undefined;
let refreshIdempotencyKey: string | undefined;

export function setCsrfToken(value?: string): void {
  inMemoryCsrfToken = value || undefined;
}

export function resetApiClientAuthState(): void {
  inMemoryCsrfToken = undefined;
  clearRefreshIdempotencyKey();
}

type QueryValue = string | number | boolean | null | undefined;

export interface RequestOptions extends Omit<RequestInit, "body"> {
  query?: Record<string, QueryValue>;
  body?: unknown;
  timeoutMs?: number;
  skipAuthRefresh?: boolean;
  skipAuthCoordination?: boolean;
}

let refreshPromise: Promise<boolean> | undefined;
const ADMIN_LOGIN_PATH = "/api/v1/admin/auth/login";
const ADMIN_SESSION_PATH = "/api/v1/admin/auth/session";
const ADMIN_LOGOUT_PATH = "/api/v1/admin/auth/logout";

async function refreshAdminSession(): Promise<boolean> {
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

async function withAdminAuthLock<T>(
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

function currentRefreshIdempotencyKey(): string {
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

function rotateRefreshIdempotencyKey(): void {
  refreshIdempotencyKey = randomUuid();
  try {
    window.localStorage.setItem(ADMIN_REFRESH_KEY_STORAGE, refreshIdempotencyKey);
  } catch {
    // Storage can be unavailable in restricted browser contexts.
  }
}

function clearRefreshIdempotencyKey(): void {
  refreshIdempotencyKey = undefined;
  try {
    window.localStorage.removeItem(ADMIN_REFRESH_KEY_STORAGE);
  } catch {
    // Storage can be unavailable in restricted browser contexts.
  }
}

function csrfToken(): string | undefined {
  return csrfCookieToken() ?? inMemoryCsrfToken;
}

function csrfCookieToken(): string | undefined {
  const match = document.cookie.match(/(?:^|;\s*)xymusic_admin_csrf=([^;]+)/);
  if (!match) return undefined;
  const value = match[1] ?? "";
  try {
    return decodeURIComponent(value) || undefined;
  } catch {
    return value || undefined;
  }
}

export function buildApiUrl(path: string, query?: Record<string, QueryValue>): string {
  const url = new URL(`${API_BASE}${path}`, window.location.origin);
  for (const [key, value] of Object.entries(query ?? {})) {
    if (value !== undefined && value !== null && value !== "") url.searchParams.set(key, String(value));
  }
  return url.toString();
}

export interface ServiceReadiness {
  status: "ready" | "unavailable";
  reason: "runtime_unavailable" | "worker_unavailable" | null;
  runtime: {
    phase: string;
    source: string;
    generation: number;
    startedAt: string | null;
  };
  worker: {
    mode: "inline" | "external";
    state: string;
    responsive: boolean;
    synchronized: boolean;
    available: boolean;
    updatedAt: string | null;
  } | null;
}

export async function serviceReadiness(signal?: AbortSignal): Promise<ServiceReadiness> {
  const timeout = createTimeoutSignal(signal, REQUEST_TIMEOUT_MS);
  try {
    let response: Response;
    try {
      response = await fetch(buildApiUrl("/health/ready"), {
        method: "GET",
        headers: { Accept: "application/json" },
        credentials: "include",
        signal: timeout.signal,
      });
    } catch (error) {
      throw apiConnectionError(error, timeout.signal);
    }
    if (response.status !== 200 && response.status !== 503) {
      throw new Error(`服务状态检查失败（HTTP ${response.status}）`);
    }
    const body = await readResponseText(response, timeout.signal);
    let payload: unknown;
    try {
      payload = JSON.parse(body);
    } catch {
      throw new Error("服务状态响应格式无效");
    }
    if (!isServiceReadiness(payload)) throw new Error("服务状态响应格式无效");
    return payload;
  } finally {
    timeout.cleanup();
  }
}

function isServiceReadiness(value: unknown): value is ServiceReadiness {
  if (!isRecord(value) || (value.status !== "ready" && value.status !== "unavailable")) return false;
  if (value.reason !== null && value.reason !== "runtime_unavailable" && value.reason !== "worker_unavailable") return false;
  if (!isRecord(value.runtime) || typeof value.runtime.phase !== "string" || typeof value.runtime.source !== "string" ||
    typeof value.runtime.generation !== "number" || !Number.isFinite(value.runtime.generation) ||
    (value.runtime.startedAt !== null && typeof value.runtime.startedAt !== "string")) return false;
  if (value.worker === null) return true;
  return isRecord(value.worker) && (value.worker.mode === "inline" || value.worker.mode === "external") &&
    typeof value.worker.state === "string" && typeof value.worker.responsive === "boolean" &&
    typeof value.worker.synchronized === "boolean" && typeof value.worker.available === "boolean" &&
    (value.worker.updatedAt === null || typeof value.worker.updatedAt === "string");
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}

function createTimeoutSignal(parent: AbortSignal | null | undefined, timeoutMs: number): {
  signal: AbortSignal;
  cleanup: () => void;
} {
  const controller = new AbortController();
  const abortFromParent = () => controller.abort(parent?.reason);
  if (parent?.aborted) abortFromParent();
  else parent?.addEventListener("abort", abortFromParent, { once: true });
  const timer = window.setTimeout(() => controller.abort(new DOMException("Request timed out", "TimeoutError")), timeoutMs);
  return {
    signal: controller.signal,
    cleanup: () => {
      window.clearTimeout(timer);
      parent?.removeEventListener("abort", abortFromParent);
    },
  };
}

function apiConnectionError(error: unknown, signal: AbortSignal): ApiConnectionError {
  const errorName = namedError(error);
  const reasonName = namedError(signal.reason);
  if (errorName === "TimeoutError" || reasonName === "TimeoutError") {
    return new ApiConnectionError("timeout", error);
  }
  if (signal.aborted || errorName === "AbortError") {
    return new ApiConnectionError("aborted", error);
  }
  return new ApiConnectionError("network", error);
}

function namedError(value: unknown): string | undefined {
  if (typeof value !== "object" || value === null || !("name" in value)) return undefined;
  return typeof value.name === "string" ? value.name : undefined;
}

async function readResponseText(response: Response, signal: AbortSignal): Promise<string> {
  try {
    return await response.text();
  } catch (error) {
    throw apiConnectionError(error, signal);
  }
}

function parseJsonResponse(body: string): unknown {
  try {
    return JSON.parse(body);
  } catch {
    throw new ApiError({
      title: "服务器响应格式无效",
      status: 502,
      detail: "服务器返回了无法解析的 JSON 响应",
    });
  }
}

function fallbackProblem(response: Response, detail?: string): ProblemDetails {
  return {
    title: response.statusText || "请求失败",
    status: response.status,
    detail,
  };
}

function responseProblem(response: Response, payload: unknown): ProblemDetails {
  if (!isRecord(payload) || typeof payload.title !== "string") {
    return fallbackProblem(response, typeof payload === "string" ? payload : undefined);
  }
  return {
    ...(typeof payload.type === "string" ? { type: payload.type } : {}),
    title: payload.title,
    status: response.status,
    ...(typeof payload.detail === "string" ? { detail: payload.detail } : {}),
    ...(typeof payload.suggestion === "string" ? { suggestion: payload.suggestion } : {}),
    ...(typeof payload.instance === "string" ? { instance: payload.instance } : {}),
    ...(typeof payload.code === "string" ? { code: payload.code } : {}),
    ...(typeof payload.traceId === "string" ? { traceId: payload.traceId } : {}),
    ...(fieldErrorRecord(payload.errors) ? { errors: payload.errors } : {}),
    ...(fieldErrorRecord(payload.fieldErrors) ? { fieldErrors: payload.fieldErrors } : {}),
    ...(safeInteger(payload.expectedVersion, 0) ? { expectedVersion: payload.expectedVersion } : {}),
    ...(safeInteger(payload.currentVersion, 0) ? { currentVersion: payload.currentVersion } : {}),
    ...(safeInteger(payload.retryAfterSeconds, 1) ? { retryAfterSeconds: payload.retryAfterSeconds } : {}),
    ...(typeof payload.conflictResourceType === "string" ? { conflictResourceType: payload.conflictResourceType } : {}),
    ...(typeof payload.conflictResourceId === "string" ? { conflictResourceId: payload.conflictResourceId } : {}),
    ...(typeof payload.albumId === "string" ? { albumId: payload.albumId } : {}),
    ...(typeof payload.trackId === "string" ? { trackId: payload.trackId } : {}),
    ...(typeof payload.setupStage === "string" ? { setupStage: payload.setupStage } : {}),
    ...(typeof payload.decisionResource === "string" ? { decisionResource: payload.decisionResource } : {}),
    ...(typeof payload.databaseState === "string" ? { databaseState: payload.databaseState } : {}),
    ...(typeof payload.rollbackIncomplete === "boolean" ? { rollbackIncomplete: payload.rollbackIncomplete } : {}),
    ...(typeof payload.destructiveStageStarted === "boolean" ? { destructiveStageStarted: payload.destructiveStageStarted } : {}),
    ...(typeof payload.migrationRequired === "boolean" ? { migrationRequired: payload.migrationRequired } : {}),
    ...(stringArray(payload.reusable) ? { reusable: payload.reusable } : {}),
    ...(stringArray(payload.missing) ? { missing: payload.missing } : {}),
    ...(typeof payload.conflictType === "string" ? { conflictType: payload.conflictType } : {}),
    ...(duplicateAlbumArray(payload.duplicateAlbums) ? { duplicateAlbums: payload.duplicateAlbums } : {}),
  };
}

function fieldErrorRecord(value: unknown): value is Record<string, string[]> {
  return isRecord(value) && !Array.isArray(value) && Object.values(value).every((messages) =>
    Array.isArray(messages) && messages.every((message) => typeof message === "string"));
}

function safeInteger(value: unknown, minimum: number): value is number {
  return typeof value === "number" && Number.isSafeInteger(value) && value >= minimum;
}

function stringArray(value: unknown): value is string[] {
  return Array.isArray(value) && value.every((item) => typeof item === "string");
}

function duplicateAlbumArray(value: unknown): value is NonNullable<ProblemDetails["duplicateAlbums"]> {
  return Array.isArray(value) && value.every((item) => isRecord(item) &&
    typeof item.id === "string" && typeof item.title === "string" &&
    typeof item.version === "number" && Number.isFinite(item.version));
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
    return payload as T;
  } finally {
    timeout.cleanup();
  }
}

export interface BinaryUploadOptions {
  contentType: string;
  signal?: AbortSignal;
  onProgress?: (percentage: number) => void;
}

function uploadBinaryOnce(path: string, body: Blob, options: BinaryUploadOptions): Promise<void> {
  return new Promise((resolve, reject) => {
    const request = new XMLHttpRequest();
    const abort = () => request.abort();
    const cleanup = () => options.signal?.removeEventListener("abort", abort);

    request.open("PUT", buildApiUrl(path));
    request.timeout = UPLOAD_TIMEOUT_MS;
    request.withCredentials = true;
    request.setRequestHeader("Accept", "application/json, application/problem+json");
    request.setRequestHeader("Content-Type", options.contentType);
    const token = csrfToken();
    if (token) request.setRequestHeader("X-CSRF-Token", token);

    request.upload.addEventListener("progress", (event) => {
      const total = event.lengthComputable && event.total > 0 ? event.total : body.size;
      if (total > 0) {
        options.onProgress?.(Math.min(100, Math.round(event.loaded / total * 100)));
      }
    });
    request.addEventListener("load", () => {
      cleanup();
      const responseToken = request.getResponseHeader("X-CSRF-Token");
      if (responseToken) setCsrfToken(responseToken);
      if (request.status >= 200 && request.status < 300) {
        options.onProgress?.(100);
        resolve();
        return;
      }
      let problem: ProblemDetails;
      try {
        problem = JSON.parse(request.responseText) as ProblemDetails;
      } catch {
        problem = { title: request.statusText || "上传失败", status: request.status, detail: request.responseText || undefined };
      }
      reject(new ApiError({ ...problem, status: request.status }));
    });
    request.addEventListener("error", () => { cleanup(); reject(new Error("上传连接中断，请检查网络后重试")); });
    request.addEventListener("abort", () => { cleanup(); reject(new DOMException("上传已取消", "AbortError")); });

    request.addEventListener("timeout", () => {
      cleanup();
      reject(new DOMException("上传超时，请稍后重试", "TimeoutError"));
    });

    if (options.signal?.aborted) {
      reject(new DOMException("上传已取消", "AbortError"));
      return;
    }
    options.signal?.addEventListener("abort", abort, { once: true });
    request.send(body);
  });
}

export async function uploadBinary(path: string, body: Blob, options: BinaryUploadOptions): Promise<void> {
  try {
    await uploadBinaryOnce(path, body, options);
  } catch (error) {
    if (error instanceof ApiError && error.status === 401 && await refreshAdminSession()) {
      await uploadBinaryOnce(path, body, options);
      return;
    }
    if (error instanceof ApiError && error.status === 401) {
      setCsrfToken();
      window.dispatchEvent(new CustomEvent("xymusic:unauthorized"));
    }
    throw error;
  }
}

export function openEventStream(path: string): EventSource {
  return new EventSource(buildApiUrl(path), { withCredentials: true });
}

function publishAdminAuthChange(): void {
  try {
    window.localStorage.setItem(ADMIN_AUTH_SYNC_STORAGE_KEY, `${Date.now()}:${randomUuid()}`);
  } catch {
    // Other tabs will still reconcile on their next authenticated request.
  }
}
