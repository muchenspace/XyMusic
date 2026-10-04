import { ApiConnectionError, ApiError, type ProblemDetails } from "@/shared/application/api-error";

const API_BASE = (import.meta.env.VITE_API_BASE_URL ?? "").replace(/\/$/, "");
export const REQUEST_TIMEOUT_MS = 20_000;
export const UPLOAD_TIMEOUT_MS = 120_000;

export type QueryValue = string | number | boolean | null | undefined;

/**
 * Builds an absolute API URL. Kept internal to the api/ transport modules;
 * callers use the exported API functions instead.
 */
export function buildApiUrl(path: string, query?: Record<string, QueryValue>): string {
  const url = new URL(`${API_BASE}${path}`, window.location.origin);
  for (const [key, value] of Object.entries(query ?? {})) {
    if (value !== undefined && value !== null && value !== "") url.searchParams.set(key, String(value));
  }
  return url.toString();
}

export function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}

export function createTimeoutSignal(parent: AbortSignal | null | undefined, timeoutMs: number): {
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

export function apiConnectionError(error: unknown, signal: AbortSignal): ApiConnectionError {
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

export function namedError(value: unknown): string | undefined {
  if (typeof value !== "object" || value === null || !("name" in value)) return undefined;
  return typeof value.name === "string" ? value.name : undefined;
}

export async function readResponseText(response: Response, signal: AbortSignal): Promise<string> {
  try {
    return await response.text();
  } catch (error) {
    throw apiConnectionError(error, signal);
  }
}

export function parseJsonResponse(body: string): unknown {
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

export function fallbackProblem(response: Response, detail?: string): ProblemDetails {
  return {
    title: response.statusText || "请求失败",
    status: response.status,
    detail,
  };
}

export function responseProblem(response: Response, payload: unknown): ProblemDetails {
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

export function fieldErrorRecord(value: unknown): value is Record<string, string[]> {
  return isRecord(value) && !Array.isArray(value) && Object.values(value).every((messages) =>
    Array.isArray(messages) && messages.every((message) => typeof message === "string"));
}

export function safeInteger(value: unknown, minimum: number): value is number {
  return typeof value === "number" && Number.isSafeInteger(value) && value >= minimum;
}

export function stringArray(value: unknown): value is string[] {
  return Array.isArray(value) && value.every((item) => typeof item === "string");
}

export function duplicateAlbumArray(value: unknown): value is NonNullable<ProblemDetails["duplicateAlbums"]> {
  return Array.isArray(value) && value.every((item) => isRecord(item) &&
    typeof item.id === "string" && typeof item.title === "string" &&
    typeof item.version === "number" && Number.isFinite(item.version));
}
