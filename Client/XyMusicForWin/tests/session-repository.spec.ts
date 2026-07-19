import { afterEach, describe, expect, it, vi } from "vitest";
import type { UserProfile } from "../src/application/ports/SessionRepository";
import { ApiClient, ApiError, type StoredSession } from "../src/infrastructure/http/ApiClient";
import { HttpSessionRepository } from "../src/infrastructure/repositories/HttpSessionRepository";
import type { PersistedSessionCredential, SessionCredentialStore } from "../src/infrastructure/session/SessionCredentialStore";

describe("desktop session cleanup", () => {
  afterEach(() => vi.unstubAllGlobals());

  it("clears local credentials and returns a visible warning when remote revocation fails", async () => {
    const credentials = new MemoryCredentialStore();
    const api = new ApiClient({ credentialStore: credentials as SessionCredentialStore });
    await api.setSession(storedSession());
    vi.stubGlobal("fetch", vi.fn().mockRejectedValue(new TypeError("network failed")));

    const result = await new HttpSessionRepository(api).logout();

    expect(result.warning).toContain("服务器会话撤销失败");
    expect(api.storedSession).toBeNull();
    expect(credentials.value).toBeNull();
  });

  it("still attempts server revocation when Windows credential deletion fails", async () => {
    const credentials = new MemoryCredentialStore();
    const api = new ApiClient({ credentialStore: credentials as SessionCredentialStore });
    await api.setSession(storedSession());
    credentials.failDelete = true;
    const fetchMock = vi.fn().mockResolvedValue(new Response(null, { status: 204 }));
    vi.stubGlobal("fetch", fetchMock);

    const error = await new HttpSessionRepository(api).logout().catch((cause: unknown) => cause);

    expect(error).toBeInstanceOf(ApiError);
    expect((error as ApiError).code).toBe("CREDENTIAL_DELETE_FAILED");
    expect((error as ApiError).message).toContain("Windows 凭据管理器");
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(api.storedSession).toBeNull();
  });

  it("warns when an offline restored session has no access token to revoke", async () => {
    const credentials = new MemoryCredentialStore();
    const api = new ApiClient({ credentialStore: credentials as SessionCredentialStore });
    await api.setSession({ ...storedSession(), accessToken: "" });
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);

    const result = await new HttpSessionRepository(api).logout();

    expect(result.warning).toContain("服务器会话撤销失败");
    expect(fetchMock).not.toHaveBeenCalled();
    expect(credentials.value).toBeNull();
  });
});

describe("desktop registration", () => {
  afterEach(() => vi.unstubAllGlobals());

  it("creates an account on the selected server without creating a local session", async () => {
    const credentials = new MemoryCredentialStore();
    const api = new ApiClient({ credentialStore: credentials as SessionCredentialStore });
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({
      userId: "user-new",
      username: "listener",
      status: "ACTIVE",
    }), { status: 201, headers: { "Content-Type": "application/json" } }));
    vi.stubGlobal("fetch", fetchMock);

    const result = await new HttpSessionRepository(api).register(
      { protocol: "https", host: "music.example.com", port: "443" },
      " listener ",
      "password-123",
    );

    expect(result).toEqual({ userId: "user-new", username: "listener", status: "ACTIVE" });
    expect(api.storedSession).toBeNull();
    expect(JSON.parse(String(fetchMock.mock.calls[0]?.[1]?.body))).toEqual({ username: "listener", password: "password-123" });
  });

  it("preserves the detailed registration error returned by the server", async () => {
    const api = new ApiClient({ credentialStore: new MemoryCredentialStore() as SessionCredentialStore });
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify({
      code: "FORBIDDEN",
      detail: "当前服务器未开放用户注册，请联系管理员开启注册功能。",
    }), { status: 403, headers: { "Content-Type": "application/problem+json" } }))); 

    const error = await new HttpSessionRepository(api).register(
      { protocol: "https", host: "music.example.com", port: "443" },
      "listener",
      "password-123",
    ).catch((cause: unknown) => cause);

    expect(error).toBeInstanceOf(ApiError);
    expect((error as ApiError).message).toContain("未开放用户注册");
  });
});

class MemoryCredentialStore {
  value: PersistedSessionCredential | null = null;
  failDelete = false;

  async read(): Promise<PersistedSessionCredential | null> { return this.value; }
  async write(value: PersistedSessionCredential): Promise<void> { this.value = value; }
  async delete(): Promise<void> {
    if (this.failDelete) throw new Error("credential delete failed");
    this.value = null;
  }
}

function storedSession(): StoredSession {
  return {
    serverUrl: "https://music.example.com",
    accessToken: "access-token",
    refreshToken: "refresh-token",
    user: userProfile(),
  };
}

function userProfile(): UserProfile {
  return {
    id: "user-1",
    username: "listener",
    displayName: "听众",
    bio: null,
    role: "USER",
    version: 1,
  };
}
