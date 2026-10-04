import { describe, expect, it, vi } from "vitest";
import { isQuotaExceededError } from "../src/application/ports/PlaybackStateRepository";
import { LocalPlaybackStateRepository } from "../src/infrastructure/playback/LocalPlaybackStateRepository";
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

  it("returns null when no credential exists", async () => {
    const current = new MemoryStorage();
    const credentials = new SessionCredentialStore(current);

    await expect(credentials.read()).resolves.toBeNull();
  });

  it("translates browser quota failures into the playback-state port error", () => {
    const setItem = vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new DOMException("quota", "QuotaExceededError");
    });
    try {
      const repository = new LocalPlaybackStateRepository();

      let thrown: unknown;
      try {
        repository.write({
          ownerKey: "owner-1",
          queue: [],
          currentIndex: -1,
          position: 0,
          shuffled: false,
          repeat: false,
          repeatMode: "off",
          crossfadeSeconds: 0,
          savedAt: new Date(0).toISOString(),
        });
      } catch (cause) {
        thrown = cause;
      }

      expect(isQuotaExceededError(thrown)).toBe(true);
    } finally {
      setItem.mockRestore();
    }
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
