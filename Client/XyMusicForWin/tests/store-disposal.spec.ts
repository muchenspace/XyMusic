import { createPinia } from "pinia";
import { createApp, defineComponent, h, nextTick } from "vue";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { ApplicationServices } from "../src/application/services";
import { applicationServicesKey } from "../src/presentation/services";
import { useHomeStore } from "../src/presentation/stores/homeStore";
import { useLibraryStore } from "../src/presentation/stores/libraryStore";
import { useLyricsStore } from "../src/presentation/stores/lyricsStore";
import { useToastStore } from "../src/presentation/stores/toastStore";

const mountedApps: Array<ReturnType<typeof createApp>> = [];

describe("presentation store disposal", () => {
  afterEach(() => {
    mountedApps.splice(0).forEach((app) => app.unmount());
    vi.useRealTimers();
  });

  it("cancels search debounce and in-flight home requests", async () => {
    vi.useFakeTimers();
    let signal: AbortSignal | undefined;
    const search = vi.fn();
    const services = {
      catalog: {
        home: vi.fn((nextSignal: AbortSignal) => {
          signal = nextSignal;
          return new Promise(() => undefined);
        }),
        randomAlbums: vi.fn(async () => []),
        randomTracks: vi.fn(async () => []),
        search,
      },
    } as unknown as ApplicationServices;
    const { store } = mountStore(useHomeStore, services);

    store.updateSearch("needle");
    void store.load();
    await nextTick();
    store.$dispose();
    await vi.advanceTimersByTimeAsync(300);

    expect(search).not.toHaveBeenCalled();
    expect(signal?.aborted).toBe(true);
  });

  it("aborts active library and lyric requests on disposal", async () => {
    let librarySignal: AbortSignal | undefined;
    const libraryServices = {
      catalog: {},
      library: {
        history: vi.fn((_cursor: string | undefined, _limit: number, signal: AbortSignal) => {
          librarySignal = signal;
          return new Promise(() => undefined);
        }),
      },
      playlists: {},
    } as unknown as ApplicationServices;
    const { store: library } = mountStore(useLibraryStore, libraryServices);

    void library.navigate("recent");
    await nextTick();
    library.$dispose();
    expect(librarySignal?.aborted).toBe(true);

    let lyricsSignal: AbortSignal | undefined;
    const lyricServices = {
      catalog: {
        lyrics: vi.fn((_trackId: string, signal: AbortSignal) => {
          lyricsSignal = signal;
          return new Promise(() => undefined);
        }),
      },
      uiPreferences: preferences(),
    } as unknown as ApplicationServices;
    const { store: lyrics } = mountStore(useLyricsStore, lyricServices);

    void lyrics.load("track-1");
    await nextTick();
    lyrics.$dispose();
    expect(lyricsSignal?.aborted).toBe(true);
  });

  it("clears outstanding toast timers when the store is disposed", () => {
    vi.useFakeTimers();
    const { store } = mountStore(useToastStore, {} as ApplicationServices);

    store.show("Background work finished", "info", 3_200);
    store.$dispose();
    vi.advanceTimersByTime(3_200);

    expect(store.messages).toEqual([]);
  });
});

function mountStore<T>(
  useStore: () => T,
  services: ApplicationServices,
): { store: T } {
  let store!: T;
  const app = createApp(defineComponent({
    setup() {
      store = useStore();
      return () => h("div");
    },
  }));
  app.use(createPinia());
  app.provide(applicationServicesKey, services);
  app.mount(document.createElement("div"));
  mountedApps.push(app);
  return { store };
}

function preferences() {
  return {
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
  };
}
