import { createApp, defineComponent, h } from "vue";
import { createPinia } from "pinia";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { ApplicationServices } from "../src/application/services";
import type { Lyrics } from "../src/domain/music";
import { applicationServicesKey } from "../src/presentation/services";
import { useLyricsStore } from "../src/presentation/stores/lyricsStore";

describe("lyrics store", () => {
  const mountedApps: Array<ReturnType<typeof createApp>> = [];

  afterEach(() => {
    mountedApps.splice(0).forEach((app) => app.unmount());
    vi.useRealTimers();
  });

  it("requests the latest lyrics when the same track is explicitly loaded again", async () => {
    const catalogLyrics = vi.fn()
      .mockResolvedValueOnce(lyrics("LINE"))
      .mockResolvedValueOnce(lyrics("WORD"));
    const services = createServices(catalogLyrics);
    let store!: ReturnType<typeof useLyricsStore>;
    const app = createApp(defineComponent({
      setup() {
        store = useLyricsStore();
        return () => h("div");
      },
    }));
    app.use(createPinia());
    app.provide(applicationServicesKey, services);
    app.mount(document.createElement("div"));
    mountedApps.push(app);

    await store.load("track-1");
    expect(store.lyrics?.timing).toBe("LINE");

    await store.load("track-1");

    expect(catalogLyrics).toHaveBeenCalledTimes(2);
    expect(store.lyrics?.timing).toBe("WORD");
  });

  it("clears cached lyrics and exposes an error when refreshing the same track fails", async () => {
    const catalogLyrics = vi.fn()
      .mockResolvedValueOnce(lyrics("LINE"))
      .mockRejectedValueOnce(new Error("refresh failed"));
    const services = createServices(catalogLyrics);
    let store!: ReturnType<typeof useLyricsStore>;
    const app = createApp(defineComponent({
      setup() {
        store = useLyricsStore();
        return () => h("div");
      },
    }));
    app.use(createPinia());
    app.provide(applicationServicesKey, services);
    app.mount(document.createElement("div"));
    mountedApps.push(app);

    await store.load("track-1");
    expect(store.lyrics?.timing).toBe("LINE");

    await store.load("track-1");

    expect(store.lyrics).toBeNull();
    expect(store.error).not.toBe("");
  });

  it("flushes pending lyric display preferences before the page exits", () => {
    vi.useFakeTimers();
    const writeLyricsFontScale = vi.fn();
    const services = createServices(vi.fn(async () => null));
    services.uiPreferences.writeLyricsFontScale = writeLyricsFontScale;
    let store!: ReturnType<typeof useLyricsStore>;
    const app = createApp(defineComponent({
      setup() {
        store = useLyricsStore();
        return () => h("div");
      },
    }));
    app.use(createPinia());
    app.provide(applicationServicesKey, services);
    app.mount(document.createElement("div"));
    mountedApps.push(app);

    store.setFontScale(1.1);
    store.setFontScale(1.2);

    expect(store.fontScale).toBe(1.2);
    expect(writeLyricsFontScale).not.toHaveBeenCalled();

    window.dispatchEvent(new Event("pagehide"));

    expect(writeLyricsFontScale).toHaveBeenCalledExactlyOnceWith(1.2);
    vi.advanceTimersByTime(1_000);
    expect(writeLyricsFontScale).toHaveBeenCalledTimes(1);
    store.$dispose();
  });
});

function lyrics(timing: "LINE" | "WORD"): Lyrics {
  return {
    trackId: "track-1",
    source: "und",
    synchronized: true,
    timing,
    lines: timing === "WORD"
      ? [{ time: 1, text: "word lyric", words: [{ time: 1, text: "word lyric" }] }]
      : [{ time: 1, text: "line lyric" }],
  };
}

function createServices(catalogLyrics: (trackId: string, signal?: AbortSignal) => Promise<Lyrics | null>): ApplicationServices {
  return {
    catalog: { lyrics: catalogLyrics },
    uiPreferences: {
      readLyrics: () => ({
        fontScale: 1,
        showTranslation: true,
        colors: {
          dark: { textColor: "#8e98a3", highlightColor: "#d7e6f3" },
          light: { textColor: "#626a74", highlightColor: "#1b4269" },
        },
      }),
      writeLyricsFontScale() {},
      writeLyricsTranslation() {},
      writeLyricsTextColor() {},
      writeLyricsHighlightColor() {},
      readLyricsOffset: () => 0,
      writeLyricsOffset() {},
      clearLyricsOffsets() {},
    },
  } as unknown as ApplicationServices;
}
