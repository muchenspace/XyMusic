import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  ADMIN_AUTH_SYNC_STORAGE_KEY,
  ApiError,
  apiRequest,
  resetApiClientAuthState,
  setCsrfToken,
} from "@/api/client";

describe("admin API client", () => {
  beforeEach(() => {
    resetApiClientAuthState();
    localStorage.clear();
    document.cookie = "xymusic_admin_csrf=csrf-cookie; path=/";
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
    document.cookie = "xymusic_admin_csrf=; max-age=0; path=/";
  });

  it("reuses the mutation idempotency key after a 401 refresh", async () => {
    const mutationKeys: string[] = [];
    const fetchMock = vi.fn(async (_input: RequestInfo | URL, init?: RequestInit) => {
      const headers = new Headers(init?.headers);
      const method = init?.method ?? "GET";
      if (method === "POST" && headers.get("Authorization") === null && mutationKeys.length < 2) {
        const url = String(_input);
        if (url.endsWith("/api/v1/admin/auth/refresh")) {
          return jsonResponse({ user: { id: "admin" }, csrfToken: "csrf-cookie" });
        }
        mutationKeys.push(headers.get("Idempotency-Key") ?? "");
        if (mutationKeys.length === 1) return jsonResponse({ title: "Unauthorized" }, 401);
        return jsonResponse({ ok: true });
      }
      return jsonResponse({ title: "Unauthorized" }, 401);
    });
    vi.stubGlobal("fetch", fetchMock);

    await expect(apiRequest<{ ok: boolean }>("/api/v1/admin/users/example", {
      method: "POST",
      body: { displayName: "测试" },
    })).resolves.toEqual({ ok: true });

    expect(mutationKeys).toHaveLength(2);
    expect(mutationKeys[0]).toMatch(/^[A-Za-z0-9._~-]{8,128}$/);
    expect(mutationKeys[1]).toBe(mutationKeys[0]);
  });

  it("prefers the current CSRF cookie over stale in-memory state", async () => {
    setCsrfToken("stale-token");
    let sentToken: string | null = null;
    vi.stubGlobal("fetch", vi.fn(async (_input: RequestInfo | URL, init?: RequestInit) => {
      sentToken = new Headers(init?.headers).get("X-CSRF-Token");
      return jsonResponse({ ok: true });
    }));

    await apiRequest("/api/v1/admin/users/example", { method: "PATCH", body: { enabled: true } });

    expect(sentToken).toBe("csrf-cookie");
  });

  it("publishes successful login changes for other tabs", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => jsonResponse({ user: { id: "admin" }, csrfToken: "csrf-cookie" })));

    await apiRequest("/api/v1/admin/auth/login", { method: "POST", body: { username: "admin", password: "secret" } });

    expect(localStorage.getItem(ADMIN_AUTH_SYNC_STORAGE_KEY)).toMatch(/^\d+:/);
  });

  it("does not expose an English server error directly to users", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => jsonResponse({ title: "Forbidden", detail: "Administrator role is required" }, 403)));

    const error = await apiRequest("/api/v1/admin/settings").catch((cause: unknown) => cause);

    expect(error).toBeInstanceOf(ApiError);
    expect((error as ApiError).message).toBe("当前账号没有执行此操作的权限");
    expect((error as ApiError).problem.detail).toBe("Administrator role is required");
  });

  it("includes the server suggestion and trace ID in actionable errors", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => jsonResponse({
      title: "初始化未完成",
      detail: "初始化在数据库清除阶段失败。",
      suggestion: "检查数据库权限后再重试。",
      traceId: "trace-setup-123",
      code: "SETUP_FAILED",
    }, 500)));

    const error = await apiRequest("/api/setup/complete", { method: "POST", body: {} }).catch((cause: unknown) => cause);

    expect(error).toBeInstanceOf(ApiError);
    expect((error as ApiError).message).toContain("初始化在数据库清除阶段失败。");
    expect((error as ApiError).message).toContain("处理建议：检查数据库权限后再重试。");
    expect((error as ApiError).message).toContain("追踪 ID：trace-setup-123");
  });

  it("honors a request-specific timeout", async () => {
    vi.useFakeTimers();
    vi.stubGlobal("fetch", vi.fn((_input: RequestInfo | URL, init?: RequestInit) => new Promise<Response>((_resolve, reject) => {
      const signal = init?.signal;
      signal?.addEventListener("abort", () => reject(signal.reason), { once: true });
    })));

    const outcome = apiRequest("/api/setup/complete", { method: "POST", body: {}, timeoutMs: 50 })
      .catch((error: unknown) => error);
    await vi.advanceTimersByTimeAsync(49);
    await vi.advanceTimersByTimeAsync(1);

    await expect(outcome).resolves.toMatchObject({ kind: "timeout" });
  });

  it("adds idempotency only to mutations and preserves an explicit key", async () => {
    const sent: Array<{ method: string; key: string | null }> = [];
    vi.stubGlobal("fetch", vi.fn(async (_input: RequestInfo | URL, init?: RequestInit) => {
      sent.push({ method: init?.method ?? "GET", key: new Headers(init?.headers).get("Idempotency-Key") });
      return jsonResponse({ ok: true });
    }));

    await apiRequest("/api/v1/admin/dashboard");
    await apiRequest("/api/v1/admin/settings", { method: "PATCH", headers: { "Idempotency-Key": "explicit-key-123" }, body: {} });

    expect(sent).toEqual([
      { method: "GET", key: null },
      { method: "PATCH", key: "explicit-key-123" },
    ]);
  });

  it("preserves structured conflict metadata from problem responses", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => jsonResponse({
      title: "Album conflict",
      status: 409,
      expectedVersion: 3,
      currentVersion: 4,
      retryAfterSeconds: 2,
      conflictResourceType: "media_job",
      conflictResourceId: "job-1",
      albumId: "album-1",
      trackId: "track-1",
      decisionResource: "database",
      databaseState: "READY",
      migrationRequired: false,
      conflictType: "DUPLICATE_ALBUM",
      duplicateAlbums: [{ id: "album-1", title: "Album", version: 2 }],
    }, 409)));

    const error = await apiRequest("/api/v1/admin/albums/merge", { method: "POST", body: {} }).catch((cause: unknown) => cause);

    expect(error).toBeInstanceOf(ApiError);
    expect((error as ApiError).problem).toMatchObject({
      conflictType: "DUPLICATE_ALBUM",
      expectedVersion: 3,
      currentVersion: 4,
      retryAfterSeconds: 2,
      conflictResourceType: "media_job",
      conflictResourceId: "job-1",
      albumId: "album-1",
      trackId: "track-1",
      decisionResource: "database",
      databaseState: "READY",
      migrationRequired: false,
      duplicateAlbums: [{ id: "album-1", title: "Album", version: 2 }],
    });
  });

  it("turns a structured media resource conflict into an actionable message", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => jsonResponse({
      title: "操作冲突",
      status: 409,
      code: "RESOURCE_CONFLICT",
      detail: "当前数据状态不允许此操作，请刷新后重试。",
      conflictResourceType: "media_job",
      conflictResourceId: "job-1",
      traceId: "trace-media-conflict",
    }, 409)));

    const error = await apiRequest("/api/v1/admin/tracks/track-1", {
      method: "DELETE",
      body: { expectedVersion: 4 },
    }).catch((cause: unknown) => cause);

    expect(error).toBeInstanceOf(ApiError);
    expect((error as ApiError).message).toContain("媒体处理仍在进行，请等待任务结束后重试");
    expect((error as ApiError).message).toContain("trace-media-conflict");
  });
});

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}
