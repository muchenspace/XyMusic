import { mount } from "@vue/test-utils";
import { createPinia } from "pinia";
import { nextTick } from "vue";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ApplicationServices } from "../src/application/services";
import type { Track } from "../src/domain/music";
import LyricsView from "../src/presentation/components/LyricsView.vue";
import { applicationServicesKey } from "../src/presentation/services";
import { useLyricsStore } from "../src/presentation/stores/lyricsStore";
import { usePlayerStore } from "../src/presentation/stores/playerStore";
import { FakePlaybackSession } from "./support/FakePlaybackSession";

const renderList = vi.hoisted(() => vi.fn());
const originalScrollIntoView = Object.getOwnPropertyDescriptor(HTMLElement.prototype, "scrollIntoView");

vi.mock("vue", async (importOriginal) => {
  const vue = await importOriginal<typeof import("vue")>();
  return {
    ...vue,
    renderList: (...args: unknown[]) => {
      renderList();
      return (vue.renderList as (...parameters: unknown[]) => unknown)(...args);
    },
  };
});

describe("word-timed lyric rendering", () => {
  const animationFrames: FrameRequestCallback[] = [];

  beforeEach(() => {
    animationFrames.splice(0);
    renderList.mockReset();
    Object.defineProperty(HTMLElement.prototype, "scrollIntoView", {
      configurable: true,
      value: vi.fn(),
    });
    vi.stubGlobal("requestAnimationFrame", (callback: FrameRequestCallback) => {
      animationFrames.push(callback);
      return animationFrames.length;
    });
    vi.stubGlobal("cancelAnimationFrame", () => undefined);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    if (originalScrollIntoView) Object.defineProperty(HTMLElement.prototype, "scrollIntoView", originalScrollIntoView);
    else Reflect.deleteProperty(HTMLElement.prototype, "scrollIntoView");
  });

  it("renders only the active word list during a smooth playback frame", async () => {
    const pinia = createPinia();
    const playbackSession = new FakePlaybackSession({
      state: {
        queue: [track()],
        currentIndex: 0,
        duration: 180,
        isPlaying: true,
      },
    });
    const wrapper = mount(LyricsView, {
      global: {
        plugins: [pinia],
        provide: { [applicationServicesKey as symbol]: services(playbackSession) },
        stubs: {
          ArtworkImage: { template: "<div />" },
          LyricsPlayerControls: { template: "<div />" },
        },
      },
    });
    const player = usePlayerStore(pinia);
    const lyrics = useLyricsStore(pinia);
    player.lyricsOpen = true;
    lyrics.lyrics = {
      trackId: "track-1",
      source: "lrc",
      synchronized: true,
      timing: "WORD",
      lines: [
        {
          time: 0,
          text: "first second",
          words: [{ time: 0, endTime: 1, text: "first" }, { time: 1, endTime: 2, text: " second" }],
        },
        {
          time: 3,
          text: "third fourth",
          words: [{ time: 3, endTime: 4, text: "third" }, { time: 4, endTime: 5, text: " fourth" }],
        },
      ],
    };
    await nextTick();
    await nextTick();

    try {
      expect(animationFrames).not.toHaveLength(0);
      renderList.mockClear();

      animationFrames.shift()?.(performance.now() + 100);
      await nextTick();

      expect(wrapper.findAll(".lyric-word")[0]?.attributes("style")).not.toContain("--lyric-word-progress: 0%;");
      expect(renderList).toHaveBeenCalledTimes(1);
    } finally {
      wrapper.unmount();
    }
  });

  it("highlights the active line word-by-word immediately when opened mid-playback", async () => {
    const pinia = createPinia();
    const playbackSession = new FakePlaybackSession({
      state: {
        queue: [track()],
        currentIndex: 0,
        currentTime: 3.5,
        duration: 180,
        isPlaying: true,
      },
    });

    const wrapper = mount(LyricsView, {
      global: {
        plugins: [pinia],
        provide: { [applicationServicesKey as symbol]: services(playbackSession) },
        stubs: {
          ArtworkImage: { template: "<div />" },
          LyricsPlayerControls: { template: "<div />" },
        },
      },
    });

    const player = usePlayerStore(pinia);
    const lyrics = useLyricsStore(pinia);
    lyrics.lyrics = {
      trackId: "track-1",
      source: "lrc",
      synchronized: true,
      timing: "WORD",
      lines: [
        {
          time: 0,
          text: "first second",
          words: [{ time: 0, endTime: 1, text: "first" }, { time: 1, endTime: 2, text: " second" }],
        },
        {
          time: 3,
          text: "third fourth",
          words: [{ time: 3, endTime: 4, text: "third" }, { time: 4, endTime: 5, text: " fourth" }],
        },
      ],
    };

    try {
      player.lyricsOpen = true;
      await nextTick();
      await nextTick();

      const lines = wrapper.findAll(".lyric-line");
      expect(lines[1]?.classes()).toContain("active");
      const thirdWord = wrapper.findAll(".lyric-word").find((element) => element.text().includes("third"));
      expect(thirdWord).toBeDefined();
      expect(thirdWord?.classes()).toContain("is-current");
      expect(thirdWord?.attributes("style")).toContain("--lyric-word-progress: 50%;");
    } finally {
      wrapper.unmount();
    }
  });
});

function services(playbackSession: FakePlaybackSession): ApplicationServices {
  return {
    catalog: { lyrics: vi.fn(async () => null) },
    playbackSession,
    desktopWindowController: {
      state: () => ({ maximized: false, fullscreen: false }),
      subscribe: () => () => undefined,
      toggleMaximize: vi.fn(async () => undefined),
    },
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
