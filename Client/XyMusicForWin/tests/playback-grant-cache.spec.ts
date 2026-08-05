import { afterEach, describe, expect, it, vi } from "vitest";
import type { PlaybackUseCases } from "../src/application/use-cases/PlaybackUseCases";
import { PlaybackGrantCache } from "../src/application/services/PlaybackGrantCache";

describe("playback grant cache resume freshness", () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  it("reuses a grant with enough lifetime and refreshes one near expiry", async () => {
    vi.useFakeTimers();
    const now = new Date("2026-08-02T12:00:00.000Z");
    vi.setSystemTime(now);
    const playback = {
      grant: vi.fn()
        .mockResolvedValueOnce({
          url: "https://example.test/valid.mp3",
          expiresAt: new Date(now.getTime() + 60_000).toISOString(),
          selectedQuality: "STANDARD",
        })
        .mockResolvedValueOnce({
          url: "https://example.test/refreshed.mp3",
          expiresAt: new Date(now.getTime() + 300_000).toISOString(),
          selectedQuality: "STANDARD",
        }),
    } as unknown as PlaybackUseCases;
    const cache = new PlaybackGrantCache(playback);

    await cache.get("track-1", "STANDARD");
    const valid = await cache.getForResume("track-1", "STANDARD");

    expect(valid).toMatchObject({ refreshed: false, grant: { url: "https://example.test/valid.mp3" } });
    expect(playback.grant).toHaveBeenCalledOnce();

    vi.setSystemTime(new Date(now.getTime() + 35_000));
    const refreshed = await cache.getForResume("track-1", "STANDARD");

    expect(refreshed).toMatchObject({ refreshed: true, grant: { url: "https://example.test/refreshed.mp3" } });
    expect(playback.grant).toHaveBeenCalledTimes(2);
  });
});
