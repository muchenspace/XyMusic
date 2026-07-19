import { describe, expect, it } from "vitest";
import { emptyServerConfig, ServerConfigStore } from "../src/infrastructure/server/ServerConfigStore";
import { SessionCredentialStore } from "../src/infrastructure/session/SessionCredentialStore";

describe("optional browser storage resilience", () => {
  it("does not turn a valid server selection into a login failure when config persistence is denied", () => {
    const store = new ServerConfigStore({
      getItem: () => null,
      setItem: () => { throw new DOMException("quota", "QuotaExceededError"); },
    });

    expect(store.write({ protocol: "https", host: "music.example.com", port: "443" })).toEqual({
      protocol: "https",
      host: "music.example.com",
      port: "443",
    });
  });

  it("falls back to an empty config when stored data cannot be read", () => {
    const store = new ServerConfigStore({
      getItem: () => { throw new DOMException("denied"); },
      setItem: () => undefined,
    });

    expect(store.read()).toEqual(emptyServerConfig());
  });

  it("ignores an unavailable legacy session store when no secure web credential exists", async () => {
    const current = new MemoryStorage();
    const credentials = new SessionCredentialStore(current, {
      getItem: () => { throw new DOMException("denied"); },
      removeItem: () => { throw new DOMException("denied"); },
    });

    await expect(credentials.read()).resolves.toBeNull();
  });
});

class MemoryStorage {
  readonly values: Record<string, string> = {};

  getItem(key: string): string | null {
    return this.values[key] ?? null;
  }

  setItem(key: string, value: string): void {
    this.values[key] = value;
  }

  removeItem(key: string): void {
    delete this.values[key];
  }
}
