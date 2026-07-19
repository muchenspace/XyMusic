import { createPinia, setActivePinia } from "pinia";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { queryClient } from "@/app/query-client";
import { resetApiClientAuthState } from "@/api/client";
import { useAuthStore } from "@/stores/auth";

describe("administrator auth store", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    queryClient.clear();
    resetApiClientAuthState();
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    queryClient.clear();
  });

  it("clears user-scoped queries when a forced session check returns 401", async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(jsonResponse({ user: adminUser(), csrfToken: "csrf" }))
      .mockResolvedValueOnce(jsonResponse({ title: "Unauthorized" }, 401));
    vi.stubGlobal("fetch", fetchMock);
    const auth = useAuthStore();

    await expect(auth.ensureSession()).resolves.toMatchObject({ user: { id: "admin-1" } });
    queryClient.setQueryData(["admin", "users"], [{ id: "sensitive" }]);

    await expect(auth.ensureSession(true)).resolves.toBeNull();

    expect(auth.session).toBeNull();
    expect(queryClient.getQueryData(["admin", "users"])).toBeUndefined();
  });

});

function adminUser() {
  return {
    id: "admin-1",
    username: "admin",
    displayName: "管理员",
    bio: null,
    role: "ADMIN",
    status: "ACTIVE",
    version: 1,
  };
}

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}
