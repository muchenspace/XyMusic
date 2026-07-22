import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { UserProfile } from "../src/application/ports/SessionRepository";
import {
  ApiClient,
  type AuthSessionResponse,
  type CurrentUserResponse,
  type StoredSession,
} from "../src/infrastructure/http/ApiClient";
import { HttpSessionRepository } from "../src/infrastructure/repositories/HttpSessionRepository";
import type {
  PersistedSessionCredential,
  SessionCredentialStore,
} from "../src/infrastructure/session/SessionCredentialStore";

describe("desktop authentication API contract", () => {
  beforeEach(() => {
    localStorage.clear();
    sessionStorage.clear();
  });

  afterEach(() => vi.unstubAllGlobals());

  it("logs in, updates the profile, and revokes all sessions with the expected credentials", async () => {
    const credentials = new MemoryCredentialStore();
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(jsonResponse(authResponse()))
      .mockResolvedValueOnce(jsonResponse(currentUser({ displayName: "Updated", version: 2 })))
      .mockResolvedValueOnce(new Response(null, { status: 204 }));
    vi.stubGlobal("fetch", fetchMock);
    const api = new ApiClient({ credentialStore: credentials as SessionCredentialStore });
    const repository = new HttpSessionRepository(api);

    await expect(repository.login(
      { protocol: "https", host: "music.example.com", port: "443" },
      " listener ",
      "password-123",
    )).resolves.toMatchObject({ user: { id: "user-1", displayName: "Listener" } });
    await expect(repository.updateProfile({ displayName: "Updated", bio: "Bio", expectedVersion: 1 }))
      .resolves.toMatchObject({ user: { displayName: "Updated", version: 2 } });
    await expect(repository.logoutAll()).resolves.toEqual({ warning: null });

    const [login, profile, logoutAll] = fetchMock.mock.calls.map(requestCall);
    expect(login.url).toBe("https://music.example.com/api/v1/auth/login");
    expect(login.init.method).toBe("POST");
    expect(JSON.parse(String(login.init.body))).toMatchObject({
      username: "listener",
      password: "password-123",
      device: { platform: "WINDOWS", appVersion: "0.1.2" },
    });
    expect(profile.url).toBe("https://music.example.com/api/v1/users/me");
    expect(profile.init.method).toBe("PATCH");
    expect(new Headers(profile.init.headers).get("Authorization")).toBe("Bearer access-token");
    expect(new Headers(profile.init.headers).get("Idempotency-Key")).toBeTruthy();
    expect(JSON.parse(String(profile.init.body))).toEqual({ displayName: "Updated", bio: "Bio", expectedVersion: 1 });
    expect(logoutAll.url).toBe("https://music.example.com/api/v1/auth/logout-all");
    expect(new Headers(logoutAll.init.headers).get("Authorization")).toBe("Bearer access-token");
    expect(api.storedSession).toBeNull();
    expect(credentials.value).toBeNull();
  });

  it("refreshes a restored credential before loading the current profile", async () => {
    const credentials = new MemoryCredentialStore();
    credentials.value = persistedCredential();
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(jsonResponse(authResponse({ accessToken: "new-access", refreshToken: "new-refresh" })))
      .mockResolvedValueOnce(jsonResponse(currentUser({ displayName: "Fresh profile", version: 4 })));
    vi.stubGlobal("fetch", fetchMock);
    const api = new ApiClient({ credentialStore: credentials as SessionCredentialStore });

    await expect(new HttpSessionRepository(api).restore())
      .resolves.toMatchObject({ user: { id: "user-1", displayName: "Fresh profile", version: 4 } });

    const [refresh, profile] = fetchMock.mock.calls.map(requestCall);
    expect(refresh.url).toBe("https://music.example.com/api/v1/auth/refresh");
    expect(refresh.init.method).toBe("POST");
    expect(new Headers(refresh.init.headers).get("Idempotency-Key")).toBe("refresh-key-1");
    expect(JSON.parse(String(refresh.init.body))).toEqual({ refreshToken: "persisted-refresh" });
    expect(profile.url).toBe("https://music.example.com/api/v1/users/me");
    expect(new Headers(profile.init.headers).get("Authorization")).toBe("Bearer new-access");
    expect(credentials.value).toMatchObject({ refreshToken: "new-refresh", user: { displayName: "Fresh profile", version: 4 } });
    expect(credentials.value?.refreshIdempotencyKey).not.toBe("refresh-key-1");
  });

  it("retries one protected request after a 401 using a refreshed access token", async () => {
    const credentials = new MemoryCredentialStore();
    const api = new ApiClient({ credentialStore: credentials as SessionCredentialStore });
    await api.setSession(storedSession());
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(problemResponse(401, "TOKEN_EXPIRED"))
      .mockResolvedValueOnce(jsonResponse(authResponse({ accessToken: "renewed-access", refreshToken: "renewed-refresh" })))
      .mockResolvedValueOnce(jsonResponse({ items: [] }));
    vi.stubGlobal("fetch", fetchMock);

    await expect(api.request("api/v1/tracks")).resolves.toEqual({ items: [] });

    const [first, refresh, retry] = fetchMock.mock.calls.map(requestCall);
    expect(new Headers(first.init.headers).get("Authorization")).toBe("Bearer access-token");
    expect(refresh.url).toBe("https://music.example.com/api/v1/auth/refresh");
    expect(JSON.parse(String(refresh.init.body))).toEqual({ refreshToken: "refresh-token" });
    expect(new Headers(retry.init.headers).get("Authorization")).toBe("Bearer renewed-access");
    expect(credentials.value?.refreshToken).toBe("renewed-refresh");
  });

  it("reserves, uploads, and completes an avatar without forwarding forbidden headers", async () => {
    const credentials = new MemoryCredentialStore();
    const api = new ApiClient({ credentialStore: credentials as SessionCredentialStore });
    await api.setSession(storedSession());
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(jsonResponse({
        id: "upload/1",
        method: "PUT",
        uploadUrl: "/api/v1/oss/c3RvcmFnZS5leGFtcGxl/upload?X-Amz-Signature=avatar",
        requiredHeaders: { "Content-Type": "image/png", "Content-Length": "3", "X-Upload-Token": "token" },
      }))
      .mockResolvedValueOnce(new Response(null, { status: 200, headers: { ETag: "etag-1" } }))
      .mockResolvedValueOnce(jsonResponse(currentUser({
        avatar: { url: "/api/v1/oss/c3RvcmFnZS5leGFtcGxl/avatar?X-Amz-Signature=image" },
        version: 2,
      })));
    vi.stubGlobal("fetch", fetchMock);

    const avatar = new File([new Uint8Array([1, 2, 3])], "avatar.png", { type: "image/png" });
    Object.defineProperty(avatar, "arrayBuffer", {
      value: async () => new Uint8Array([1, 2, 3]).buffer,
    });

    await expect(new HttpSessionRepository(api).uploadAvatar(avatar))
      .resolves.toMatchObject({
        user: {
          avatarUrl: "https://music.example.com/api/v1/oss/c3RvcmFnZS5leGFtcGxl/avatar?X-Amz-Signature=image",
          version: 2,
        },
      });

    const [reserve, upload, complete] = fetchMock.mock.calls.map(requestCall);
    expect(reserve.url).toBe("https://music.example.com/api/v1/users/me/avatar/uploads");
    expect(reserve.init.method).toBe("POST");
    expect(new Headers(reserve.init.headers).get("Idempotency-Key")).toBeTruthy();
    expect(JSON.parse(String(reserve.init.body))).toMatchObject({
      fileName: "avatar.png",
      contentType: "image/png",
      sizeBytes: 3,
    });
    expect(upload.url).toBe("https://music.example.com/api/v1/oss/c3RvcmFnZS5leGFtcGxl/upload?X-Amz-Signature=avatar");
    expect(upload.init.method).toBe("PUT");
    expect(new Headers(upload.init.headers).get("Content-Length")).toBeNull();
    expect(new Headers(upload.init.headers).get("X-Upload-Token")).toBe("token");
    expect(complete.url).toBe("https://music.example.com/api/v1/users/me/avatar/uploads/upload%2F1/complete");
    expect(JSON.parse(String(complete.init.body))).toEqual({ observedEtag: "etag-1" });
    expect(credentials.value?.user.avatarUrl)
      .toBe("https://music.example.com/api/v1/oss/c3RvcmFnZS5leGFtcGxl/avatar?X-Amz-Signature=image");
  });

  it("rejects an unexpectedly large storage response before completing an avatar upload", async () => {
    const credentials = new MemoryCredentialStore();
    const api = new ApiClient({ credentialStore: credentials as SessionCredentialStore });
    await api.setSession(storedSession());
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(jsonResponse({
        id: "upload-oversize",
        method: "PUT",
        uploadUrl: "https://storage.example/upload",
        requiredHeaders: { "Content-Type": "image/png" },
      }))
      .mockResolvedValueOnce(new Response(null, { status: 200, headers: { "Content-Length": "65537" } }));
    vi.stubGlobal("fetch", fetchMock);
    const avatar = new File([new Uint8Array([1])], "avatar.png", { type: "image/png" });
    Object.defineProperty(avatar, "arrayBuffer", { value: async () => new Uint8Array([1]).buffer });

    await expect(new HttpSessionRepository(api).uploadAvatar(avatar))
      .rejects.toMatchObject({ code: "UPLOAD_RESPONSE_TOO_LARGE" });
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });
});

class MemoryCredentialStore {
  value: PersistedSessionCredential | null = null;

  async read(): Promise<PersistedSessionCredential | null> { return this.value; }
  async write(value: PersistedSessionCredential): Promise<void> { this.value = structuredClone(value); }
  async delete(): Promise<void> { this.value = null; }
}

function requestCall(call: unknown[]): { url: string; init: RequestInit } {
  return { url: String(call[0]), init: (call[1] ?? {}) as RequestInit };
}

function jsonResponse(value: unknown, status = 200): Response {
  return new Response(JSON.stringify(value), { status, headers: { "Content-Type": "application/json" } });
}

function problemResponse(status: number, code: string): Response {
  return new Response(JSON.stringify({ title: "Unauthorized", code }), {
    status,
    headers: { "Content-Type": "application/problem+json" },
  });
}

function authResponse(tokens: Partial<AuthSessionResponse["tokens"]> = {}): AuthSessionResponse {
  return {
    user: currentUser(),
    tokens: { accessToken: "access-token", refreshToken: "refresh-token", ...tokens },
  };
}

function currentUser(overrides: Partial<CurrentUserResponse> = {}): CurrentUserResponse {
  return {
    id: "user-1",
    username: "listener",
    displayName: "Listener",
    bio: null,
    role: "USER",
    version: 1,
    ...overrides,
  };
}

function storedSession(): StoredSession {
  return {
    serverUrl: "https://music.example.com",
    accessToken: "access-token",
    refreshToken: "refresh-token",
    user: userProfile(),
  };
}

function persistedCredential(): PersistedSessionCredential {
  return {
    serverUrl: "https://music.example.com",
    refreshToken: "persisted-refresh",
    refreshIdempotencyKey: "refresh-key-1",
    user: userProfile(),
  };
}

function userProfile(): UserProfile {
  return {
    id: "user-1",
    username: "listener",
    displayName: "Listener",
    bio: null,
    role: "USER",
    version: 1,
  };
}
