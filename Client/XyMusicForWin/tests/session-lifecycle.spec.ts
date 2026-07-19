import { createApp, defineComponent, h, nextTick } from "vue";
import { createPinia } from "pinia";
import { flushPromises } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { ApplicationServices } from "../src/application/services";
import type { AudioPlayer, AudioSnapshot } from "../src/application/ports/AudioPlayer";
import type { Track } from "../src/domain/music";
import { useSessionLifecycle } from "../src/presentation/composables/useSessionLifecycle";
import { applicationServicesKey } from "../src/presentation/services";
import { useHomeStore } from "../src/presentation/stores/homeStore";
import { usePlayerStore } from "../src/presentation/stores/playerStore";
import { useSessionStore } from "../src/presentation/stores/sessionStore";

describe("session workspace restoration", () => {
  beforeEach(() => localStorage.clear());

  it("restores the local playback queue even when the home request fails", async () => {
    const track = createTrack();
    const restorePlayback = vi.fn(() => ({
      ownerKey: "http://music.test:3000|user-1",
      queue: [track],
      currentIndex: 0,
      position: 42,
      shuffled: false,
      repeat: false,
      repeatMode: "off" as const,
      quality: "AUTO" as const,
      crossfadeSeconds: 0,
      savedAt: "2026-07-17T00:00:00.000Z",
    }));
    const services = createServices(restorePlayback);
    let session!: ReturnType<typeof useSessionStore>;
    let player!: ReturnType<typeof usePlayerStore>;
    let home!: ReturnType<typeof useHomeStore>;
    const Root = defineComponent({
      setup() {
        useSessionLifecycle(() => undefined, () => undefined);
        session = useSessionStore();
        player = usePlayerStore();
        home = useHomeStore();
        return () => h("div");
      },
    });
    const app = createApp(Root);
    app.use(createPinia());
    app.provide(applicationServicesKey, services);
    const element = document.createElement("div");
    document.body.appendChild(element);
    app.mount(element);
    await flushPromises();

    session.session = {
      user: {
        id: "user-1",
        username: "listener",
        displayName: "Listener",
        bio: null,
        role: "USER",
        version: 1,
      },
    };
    await nextTick();

    expect(restorePlayback).toHaveBeenCalledWith("http://music.test:3000|user-1");
    expect(player.currentTrack?.id).toBe(track.id);
    expect(player.currentTime).toBe(42);
    await flushPromises();
    expect(home.feed).toBeNull();
    expect(home.feedError).toBe("加载失败");

    app.unmount();
    element.remove();
  });
});

function createServices(restorePlayback: () => unknown): ApplicationServices {
  return {
    catalog: {
      home: vi.fn(async () => { throw new Error("offline"); }),
      randomAlbums: vi.fn(async () => []),
      randomTracks: vi.fn(async () => []),
    },
    library: {},
    playlists: {},
    playback: {
      grant: vi.fn(),
      record: vi.fn(async () => undefined),
    },
    playbackState: {
      restore: restorePlayback,
      save: vi.fn(),
      checkpoint: vi.fn(),
      clear: vi.fn(),
    },
    playbackGrants: {
      clear: vi.fn(),
    },
    session: {
      restore: vi.fn(async () => null),
      serverConfig: vi.fn(() => ({ protocol: "http", host: "music.test", port: "3000" })),
    },
    audio: new FakeAudioPlayer(),
    desktop: {
      async onMediaAction() { return () => undefined; },
      async updateMediaMetadata() { return undefined; },
      async updateMediaPlayback() { return undefined; },
      async clearMediaSession() { return undefined; },
    },
    desktopWindow: {
      async minimize() {},
      async toggleMaximize() {},
      async toggleFullscreen() {},
      async isMaximized() { return false; },
      async close() {},
      async onResized() { return () => undefined; },
      async setTheme() {},
      async setMiniMode() {},
    },
    diagnostics: {
      info() {},
      warn() {},
      error() {},
      entries: () => [],
      clear() {},
    },
    notifier: { async notify() {} },
    uiPreferences: {
      readTheme: () => "system",
      writeTheme() {},
      readLyrics: () => ({
        fontScale: 1,
        showTranslation: true,
        wordLyricsEnabled: true,
        colors: {
          dark: { textColor: "#8e98a3", highlightColor: "#d7e6f3" },
          light: { textColor: "#626a74", highlightColor: "#1b4269" },
        },
      }),
      writeLyricsFontScale() {},
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

function createTrack(): Track {
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
    publishedAt: "2026-07-17T00:00:00.000Z",
  };
}
