import { afterEach, describe, expect, it, vi } from "vitest";
import { isQuotaExceededError } from "../src/application/ports/PlaybackStateRepository";
import { LocalPlaybackStateRepository } from "../src/infrastructure/playback/LocalPlaybackStateRepository";
import { emptyServerConfig, ServerConfigStore } from "../src/infrastructure/server/ServerConfigStore";
import { SessionCredentialStore } from "../src/infrastructure/session/SessionCredentialStore";

describe("optional browser storage resilience", () => {
  afterEach(() => {
    localStorage.clear();
  });
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

  it("clears compact v3 snapshots and checkpoints for the matching owner while preserving other owners", () => {
    const repository = new LocalPlaybackStateRepository();
    const savedAt = "2026-08-01T00:00:00.000Z";
    const ownerOneState = {
      ownerKey: "owner-1",
      queue: [{
        id: "track-1",
        title: "Track 1",
        artist: "Artist",
        artistIds: ["artist-1"],
        album: "Album",
        albumId: "album-1",
        coverUrl: "",
        duration: 180,
        liked: false,
        publishedAt: savedAt,
      }],
      currentIndex: 0,
      position: 24,
      shuffled: false,
      repeat: false,
      repeatMode: "off" as const,
      crossfadeSeconds: 0,
      savedAt,
    };

    repository.write(ownerOneState);
    repository.writeCheckpoint({
      ownerKey: "owner-1",
      currentIndex: 0,
      trackId: "track-1",
      position: 56,
      savedAt: "2026-08-01T00:01:00.000Z",
      snapshotSavedAt: savedAt,
    });

    expect(localStorage.getItem("xymusic.desktop.playback-state.snapshot.v2")).toContain('"v":3');
    expect(repository.read("owner-1")?.position).toBe(56);

    repository.clear("owner-2");
    expect(repository.read("owner-1")?.position).toBe(56);

    repository.clear("owner-1");
    expect(repository.read("owner-1")).toBeNull();
    expect(localStorage.getItem("xymusic.desktop.playback-state.snapshot.v2")).toBeNull();
    expect(localStorage.getItem("xymusic.desktop.playback-state.checkpoint.v2")).toBeNull();
  });

  it("clears legacy ownerKey snapshots only when the owner matches", () => {
    const repository = new LocalPlaybackStateRepository();
    localStorage.setItem("xymusic.desktop.playback-state.snapshot.v2", JSON.stringify({
      ownerKey: "legacy-owner",
      queue: [{ id: "track-1", title: "Legacy Track" }],
      currentIndex: 0,
      position: 15,
      savedAt: "2026-08-01T00:00:00.000Z",
    }));

    repository.clear("other-owner");
    expect(repository.read("legacy-owner")?.position).toBe(15);

    repository.clear("legacy-owner");
    expect(repository.read("legacy-owner")).toBeNull();
    expect(localStorage.getItem("xymusic.desktop.playback-state.snapshot.v2")).toBeNull();
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
