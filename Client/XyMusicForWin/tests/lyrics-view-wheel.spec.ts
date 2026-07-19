import { mount, type VueWrapper } from "@vue/test-utils";
import { createPinia } from "pinia";
import { nextTick } from "vue";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ApplicationServices } from "../src/application/services";
import type { AudioPlayer, AudioSnapshot } from "../src/application/ports/AudioPlayer";
import type { Track } from "../src/domain/music";
import LyricsView from "../src/presentation/components/LyricsView.vue";
import { applicationServicesKey } from "../src/presentation/services";
import { useLyricsStore } from "../src/presentation/stores/lyricsStore";
import { usePlayerStore } from "../src/presentation/stores/playerStore";

const originalScrollIntoView = Object.getOwnPropertyDescriptor(HTMLElement.prototype, "scrollIntoView");
const scrollIntoView = vi.fn();

describe("playback lyrics wheel controls", () => {
  beforeEach(() => {
    scrollIntoView.mockReset();
    Object.defineProperty(HTMLElement.prototype, "scrollIntoView", {
      configurable: true,
      writable: true,
      value: scrollIntoView,
    });
  });

  afterEach(() => {
    if (originalScrollIntoView) Object.defineProperty(HTMLElement.prototype, "scrollIntoView", originalScrollIntoView);
    else Reflect.deleteProperty(HTMLElement.prototype, "scrollIntoView");
  });

  it("prevents Ctrl+wheel defaults and persists font-size changes", async () => {
    const mounted = await mountLyricsView();
    try {
      const scroll = mounted.wrapper.get(".lyrics-scroll").element;
      const increase = new WheelEvent("wheel", { cancelable: true, ctrlKey: true, deltaY: -100 });
      scroll.dispatchEvent(increase);
      await nextTick();

      expect(increase.defaultPrevented).toBe(true);
      expect(mounted.lyrics.fontScale).toBe(1.1);
      expect(mounted.writeLyricsFontScale).toHaveBeenLastCalledWith(1.1);

      const decrease = new WheelEvent("wheel", { cancelable: true, ctrlKey: true, deltaY: 100 });
      scroll.dispatchEvent(decrease);
      await nextTick();

      expect(decrease.defaultPrevented).toBe(true);
      expect(mounted.lyrics.fontScale).toBe(1);
      expect(mounted.writeLyricsFontScale).toHaveBeenLastCalledWith(1);
    } finally {
      mounted.wrapper.unmount();
    }
  });

  it("binds both theme palettes to playback lyric CSS variables", async () => {
    const mounted = await mountLyricsView();
    try {
      const style = mounted.wrapper.get(".lyrics-scroll").attributes("style");
      expect(style).toContain("--playback-lyric-text-dark: #8e98a3");
      expect(style).toContain("--playback-lyric-highlight-dark: #d7e6f3");
      expect(style).toContain("--playback-lyric-text-light: #626a74");
      expect(style).toContain("--playback-lyric-highlight-light: #1b4269");
    } finally {
      mounted.wrapper.unmount();
    }
  });

  it("keeps ordinary wheel scrolling and pauses automatic lyric following", async () => {
    const mounted = await mountLyricsView();
    try {
      scrollIntoView.mockClear();
      const scroll = mounted.wrapper.get(".lyrics-scroll").element;
      const wheel = new WheelEvent("wheel", { cancelable: true, deltaY: 100 });
      scroll.dispatchEvent(wheel);

      expect(wheel.defaultPrevented).toBe(false);
      expect(mounted.writeLyricsFontScale).not.toHaveBeenCalled();

      mounted.player.currentTime = 1.5;
      await nextTick();
      await nextTick();

      expect(scrollIntoView).not.toHaveBeenCalled();
    } finally {
      mounted.wrapper.unmount();
    }
  });
});

async function mountLyricsView(): Promise<{
  wrapper: VueWrapper;
  player: ReturnType<typeof usePlayerStore>;
  lyrics: ReturnType<typeof useLyricsStore>;
  writeLyricsFontScale: ReturnType<typeof vi.fn>;
}> {
  const pinia = createPinia();
  const writeLyricsFontScale = vi.fn();
  const services = createServices(writeLyricsFontScale);
  const wrapper = mount(LyricsView, {
    global: {
      plugins: [pinia],
      provide: { [applicationServicesKey as symbol]: services },
      stubs: {
        ArtworkImage: { template: "<div />" },
        LyricsPlayerControls: { template: "<div />" },
      },
    },
  });
  const player = usePlayerStore(pinia);
  const lyrics = useLyricsStore(pinia);
  player.queue = [track()];
  player.currentIndex = 0;
  player.currentTime = 0;
  player.lyricsOpen = true;
  lyrics.lyrics = {
    trackId: "track-1",
    source: "lrc",
    synchronized: true,
    lines: [
      { time: 0, text: "first line" },
      { time: 1, text: "second line" },
    ],
  };
  await nextTick();
  await nextTick();
  return { wrapper, player, lyrics, writeLyricsFontScale };
}

function createServices(writeLyricsFontScale: (value: number) => void): ApplicationServices {
  return {
    catalog: { lyrics: vi.fn(async () => null) },
    playback: { record: vi.fn(async () => undefined) },
    audio: new FakeAudioPlayer(),
    desktopWindow: {
      toggleMaximize: vi.fn(async () => undefined),
    },
    uiPreferences: {
      readLyrics: () => ({
        fontScale: 1,
        showTranslation: true,
        wordLyricsEnabled: true,
        colors: {
          dark: { textColor: "#8e98a3", highlightColor: "#d7e6f3" },
          light: { textColor: "#626a74", highlightColor: "#1b4269" },
        },
      }),
      writeLyricsFontScale,
      writeLyricsTranslation() {},
      writeLyricsWordLyricsEnabled() {},
      writeLyricsTextColor() {},
      writeLyricsHighlightColor() {},
      readLyricsOffset: () => 0,
      writeLyricsOffset() {},
      clearLyricsOffsets() {},
    },
  } as unknown as ApplicationServices;
}

class FakeAudioPlayer implements AudioPlayer {
  load(): Promise<void> { return Promise.resolve(); }
  play(): Promise<void> { return Promise.resolve(); }
  pause(): void {}
  stop(): void {}
  seek(): void {}
  setVolume(): void {}
  snapshot(): AudioSnapshot { return { currentTime: 0, duration: 0, paused: true }; }
  onUpdate(): () => void { return () => undefined; }
  onEnded(): () => void { return () => undefined; }
  onError(): () => void { return () => undefined; }
}

function track(): Track {
  return {
    id: "track-1",
    title: "Track",
    artist: "Artist",
    artistIds: ["artist-1"],
    album: "Album",
    coverUrl: "",
    duration: 180,
    liked: false,
    publishedAt: "2026-07-18T00:00:00.000Z",
  };
}
