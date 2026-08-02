import { mount } from "@vue/test-utils";
import { createPinia } from "pinia";
import { nextTick } from "vue";
import { describe, expect, it, vi } from "vitest";
import type { ApplicationServices } from "../src/application/services";
import type { Track } from "../src/domain/music";
import LyricsPlayerControls from "../src/presentation/components/LyricsPlayerControls.vue";
import MiniPlayer from "../src/presentation/components/MiniPlayer.vue";
import PlayerBar from "../src/presentation/components/PlayerBar.vue";
import { applicationServicesKey } from "../src/presentation/services";
import { FakePlaybackSession } from "./support/FakePlaybackSession";

describe("playback progress rendering", () => {
  it("updates the progress control without rerendering the full player bar", async () => {
    const playbackSession = session();
    const playerBarUpdates = vi.fn();
    const wrapper = mount(PlayerBar, {
      global: mountOptions(playbackSession, PlayerBar, playerBarUpdates),
    });

    playbackSession.update({ currentTime: 90, progress: 50 });
    await nextTick();

    expect(wrapper.get(".progress-row input").element.value).toBe("50");
    expect(playerBarUpdates).not.toHaveBeenCalled();

    await wrapper.get(".progress-row input").setValue("75");
    expect(playbackSession.state().currentTime).toBe(135);
    wrapper.unmount();
  });

  it("updates the compact progress control without rerendering the mini player", async () => {
    const playbackSession = session();
    const miniPlayerUpdates = vi.fn();
    const wrapper = mount(MiniPlayer, {
      global: mountOptions(playbackSession, MiniPlayer, miniPlayerUpdates),
    });

    playbackSession.update({ currentTime: 36, progress: 20 });
    await nextTick();

    expect(wrapper.get(".mini-track-copy input").element.value).toBe("20");
    expect(miniPlayerUpdates).not.toHaveBeenCalled();
    wrapper.unmount();
  });

  it("keeps lyric-page transport controls outside progress ticks", async () => {
    const playbackSession = session();
    const lyricControlUpdates = vi.fn();
    const wrapper = mount(LyricsPlayerControls, {
      global: mountOptions(playbackSession, LyricsPlayerControls, lyricControlUpdates),
    });

    playbackSession.update({ currentTime: 54, progress: 30 });
    await nextTick();

    expect(wrapper.get(".lyrics-progress-row input").element.value).toBe("30");
    expect(lyricControlUpdates).not.toHaveBeenCalled();
    wrapper.unmount();
  });
});

function session(): FakePlaybackSession {
  return new FakePlaybackSession({
    state: {
      queue: [track()],
      currentIndex: 0,
      duration: 180,
    },
  });
}

function mountOptions(
  playbackSession: FakePlaybackSession,
  component: unknown,
  updated: ReturnType<typeof vi.fn>,
) {
  return {
    plugins: [createPinia()],
    provide: {
      [applicationServicesKey as symbol]: services(playbackSession),
    },
    stubs: {
      ArtworkImage: { template: "<div class=\"artwork-image\" />" },
    },
    mixins: [{
      updated() {
        if ((this as unknown as { $: { type: unknown } }).$.type === component) updated();
      },
    }],
  };
}

function services(playbackSession: FakePlaybackSession): ApplicationServices {
  const desktopLyricsState = {
    visible: false,
    actuallyVisible: false,
    locked: false,
    hiddenForFullscreen: false,
    fullscreenBehavior: "show" as const,
    fontScale: 1,
    textColor: "#ffffff",
    highlightColor: "#ffffff",
  };
  const desktopWindowState = { maximized: false, fullscreen: false };
  return {
    playbackSession,
    desktopLyricsController: {
      state: () => desktopLyricsState,
      subscribe: (listener: (state: typeof desktopLyricsState) => void) => {
        listener(desktopLyricsState);
        return () => undefined;
      },
      dispose: () => undefined,
      toggleVisible: () => undefined,
    },
    desktopWindowController: {
      state: () => desktopWindowState,
      subscribe: (listener: (state: typeof desktopWindowState) => void) => {
        listener(desktopWindowState);
        return () => undefined;
      },
      minimize: async () => undefined,
      toggleMaximize: async () => undefined,
      toggleFullscreen: async () => undefined,
      close: async () => undefined,
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
    albumId: "album-1",
    coverUrl: "",
    duration: 180,
    liked: false,
    publishedAt: "2026-08-02T00:00:00.000Z",
  };
}
