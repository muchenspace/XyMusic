import { createApp, defineComponent, h, nextTick } from "vue";
import { createPinia } from "pinia";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { ApplicationServices } from "../src/application/services";
import type { AudioPlayer, AudioSnapshot } from "../src/application/ports/AudioPlayer";
import type { Track } from "../src/domain/music";
import { applicationServicesKey } from "../src/presentation/services";
import { usePlayerStore } from "../src/presentation/stores/playerStore";
import { FakePlaybackSession } from "./support/FakePlaybackSession";

describe("player preference updates", () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  it("forwards writable volume changes while preserving the explicit persistence boundary", () => {
    const audio = new FakeAudioPlayer();
    const writeVolume = vi.fn();
    const app = createApp(defineComponent({
      setup() {
        usePlayerStore();
        return () => h("div");
      },
    }));
    app.use(createPinia());
    app.provide(applicationServicesKey, createServices(audio, writeVolume));
    const element = document.createElement("div");
    document.body.appendChild(element);
    app.mount(element);
    const player = usePlayerStore();

    player.volume = 18;
    player.volume = 41;
    expect(audio.volumes.at(-1)).toBe(0.41);
    expect(writeVolume).not.toHaveBeenCalled();

    player.flushPlayerPreferences();
    expect(writeVolume).toHaveBeenCalledExactlyOnceWith(41);

    app.unmount();
    element.remove();
  });

  it("closes transient overlays after mini mode is enabled", async () => {
    const playbackSession = new FakePlaybackSession({
      state: { queue: [track()], currentIndex: 0 },
    });
    const app = createApp(defineComponent({
      setup() {
        usePlayerStore();
        return () => h("div");
      },
    }));
    app.use(createPinia());
    app.provide(applicationServicesKey, { playbackSession } as unknown as ApplicationServices);
    const element = document.createElement("div");
    document.body.appendChild(element);
    app.mount(element);
    const player = usePlayerStore();
    player.queueOpen = true;
    player.lyricsOpen = true;

    await player.setMiniMode(true);

    expect(player.miniMode).toBe(true);
    expect(player.queueOpen).toBe(false);
    expect(player.lyricsOpen).toBe(false);

    app.unmount();
    element.remove();
  });

  it("projects favorite overrides without mutating the playback snapshot", async () => {
    const sourceTrack = track();
    const playbackSession = new FakePlaybackSession({
      state: { queue: [sourceTrack], currentIndex: 0 },
    });
    const app = createApp(defineComponent({
      setup() {
        const player = usePlayerStore();
        return () => h("span", player.currentTrack?.liked ? "liked" : "not-liked");
      },
    }));
    app.use(createPinia());
    app.provide(applicationServicesKey, { playbackSession } as unknown as ApplicationServices);
    const element = document.createElement("div");
    document.body.appendChild(element);
    app.mount(element);
    const player = usePlayerStore();

    expect(element.textContent).toBe("not-liked");
    player.setFavorite(sourceTrack.id, true);
    await nextTick();

    expect(element.textContent).toBe("liked");
    expect(player.currentTrack).not.toBe(sourceTrack);
    expect(playbackSession.state().queue[0]?.liked).toBe(false);

    const refreshedTrack = track();
    playbackSession.update({ queue: [refreshedTrack] });
    await nextTick();

    expect(element.textContent).toBe("liked");
    expect(playbackSession.state().queue[0]?.liked).toBe(false);

    app.unmount();
    element.remove();
  });

  it("does not rerender track consumers for time-only playback snapshots", async () => {
    const playbackSession = new FakePlaybackSession({
      state: { queue: [track()], currentIndex: 0, duration: 180 },
    });
    let renderCount = 0;
    const app = createApp(defineComponent({
      setup() {
        const player = usePlayerStore();
        return () => {
          renderCount += 1;
          return h("span", player.currentTrack?.title);
        };
      },
    }));
    app.use(createPinia());
    app.provide(applicationServicesKey, { playbackSession } as unknown as ApplicationServices);
    const element = document.createElement("div");
    document.body.appendChild(element);
    app.mount(element);
    await nextTick();

    const stableRenderCount = renderCount;
    playbackSession.update({ currentTime: 1.25, progress: 1.25 / 180 * 100 });
    await nextTick();

    expect(renderCount).toBe(stableRenderCount);
    app.unmount();
    element.remove();
  });

  it("releases its session subscription without disposing the application service", () => {
    const playbackSession = new FakePlaybackSession({
      state: { queue: [track()], currentIndex: 0 },
    });
    const dispose = vi.spyOn(playbackSession, "dispose");
    const pinia = createPinia();
    const app = createApp(defineComponent({
      setup() {
        usePlayerStore();
        return () => h("div");
      },
    }));
    app.use(pinia);
    app.provide(applicationServicesKey, { playbackSession } as unknown as ApplicationServices);
    const element = document.createElement("div");
    document.body.appendChild(element);
    app.mount(element);
    const player = usePlayerStore(pinia);

    playbackSession.update({ isPlaying: true });
    expect(player.isPlaying).toBe(true);

    player.$dispose();
    playbackSession.update({ isPlaying: false });

    expect(dispose).not.toHaveBeenCalled();
    expect(player.isPlaying).toBe(true);

    app.unmount();
    element.remove();
  });
});

function createServices(audio: AudioPlayer, writeVolume: (value: number) => void): ApplicationServices {
  let pendingVolume: number | null = null;
  return {
    playbackSession: new FakePlaybackSession({
      onSetVolume: (value) => {
        audio.setVolume(value / 100);
        pendingVolume = value;
        return value;
      },
      onFlushPlayerPreferences: () => {
        if (pendingVolume !== null) writeVolume(pendingVolume);
        pendingVolume = null;
      },
    }),
  } as unknown as ApplicationServices;
}

class FakeAudioPlayer implements AudioPlayer {
  readonly volumes: number[] = [];

  load(): Promise<void> { return Promise.resolve(); }
  play(): Promise<void> { return Promise.resolve(); }
  pause(): void {}
  stop(): void {}
  seek(): void {}
  setVolume(volume: number): void { this.volumes.push(volume); }
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
    albumId: "album-1",
    coverUrl: "",
    duration: 180,
    liked: false,
    publishedAt: "2026-08-01T00:00:00.000Z",
  };
}
