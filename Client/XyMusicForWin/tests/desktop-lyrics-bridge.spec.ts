import { mount } from "@vue/test-utils";
import { createPinia } from "pinia";
import { defineComponent, h, nextTick } from "vue";
import { describe, expect, it, vi } from "vitest";
import type {
  DesktopLyricsClock,
  DesktopLyricsSnapshot,
} from "../src/application/ports/DesktopLyrics";
import type {
  DesktopLyricsController,
  DesktopLyricsControllerState,
  DesktopLyricsPlaybackRequest,
} from "../src/application/ports/DesktopLyricsController";
import type { ApplicationServices } from "../src/application/services";
import type { Track } from "../src/domain/music";
import { useDesktopLyricsBridge } from "../src/presentation/composables/useDesktopLyricsBridge";
import { applicationServicesKey } from "../src/presentation/services";
import { FakePlaybackSession } from "./support/FakePlaybackSession";

describe("desktop lyrics bridge", () => {
  it("routes playback requests through the player projection and cleans up its subscription", async () => {
    const controller = new FakeDesktopLyricsController(visibleState());
    const playbackSession = new FakePlaybackSession({
      state: { queue: [track("one"), track("two")], currentIndex: 0, duration: 180 },
    });
    const mounted = mountBridge(controller, playbackSession);
    try {
      await settle();
      controller.sendSnapshot.mockClear();

      controller.emitRequest("next");
      await settle();
      expect(playbackSession.state().currentIndex).toBe(1);

      controller.emitRequest("previous");
      await settle();
      expect(playbackSession.state().currentIndex).toBe(0);

      controller.emitRequest("toggle-playback");
      await settle();
      expect(playbackSession.state().isPlaying).toBe(true);

      const snapshotsBeforeReady = controller.sendSnapshot.mock.calls.length;
      controller.emitRequest("ready");
      await settle();
      expect(controller.sendSnapshot).toHaveBeenCalledTimes(snapshotsBeforeReady + 1);
    } finally {
      mounted.unmount();
    }

    expect(controller.removePlaybackRequests).toHaveBeenCalledOnce();
  });

  it("keeps only the latest clock while the native transport is busy", async () => {
    const firstClock = deferred<void>();
    let clockCalls = 0;
    const controller = new FakeDesktopLyricsController(visibleState(), {
      sendClock: vi.fn(() => {
        clockCalls += 1;
        return clockCalls === 1 ? firstClock.promise : Promise.resolve();
      }),
    });
    const playbackSession = new FakePlaybackSession({
      state: {
        queue: [track("one")],
        currentIndex: 0,
        currentTime: 0,
        duration: 180,
        isPlaying: true,
      },
    });
    const mounted = mountBridge(controller, playbackSession);
    try {
      await waitFor(() => controller.sendClock.mock.calls.length === 1);

      playbackSession.update({ currentTime: 1 });
      playbackSession.update({ currentTime: 2 });
      playbackSession.update({ currentTime: 3 });
      await nextTick();

      expect(controller.sendClock).toHaveBeenCalledOnce();
      firstClock.resolve();
      await waitFor(() => controller.sendClock.mock.calls.length === 2);

      expect((controller.sendClock.mock.calls[1]?.[0] as DesktopLyricsClock).positionSeconds).toBe(3);
    } finally {
      mounted.unmount();
    }
  });

  it("orders snapshots and clocks in one monotonic transport stream", async () => {
    const sent: Array<DesktopLyricsSnapshot | DesktopLyricsClock> = [];
    const controller = new FakeDesktopLyricsController(visibleState(), {
      sendSnapshot: vi.fn(async (snapshot: DesktopLyricsSnapshot) => { sent.push(snapshot); }),
      sendClock: vi.fn(async (clock: DesktopLyricsClock) => { sent.push(clock); }),
    });
    const playbackSession = new FakePlaybackSession({
      state: { queue: [track("one")], currentIndex: 0, currentTime: 1, duration: 180, isPlaying: true },
    });
    const mounted = mountBridge(controller, playbackSession);
    try {
      await waitFor(() => sent.some((message) => "track" in message) && sent.some((message) => "trackId" in message));
      const revisions = sent.map((message) => message.revision);
      const transportEpochs = sent.map((message) => message.transportEpoch);

      expect(revisions.every((revision) => Number.isFinite(revision))).toBe(true);
      expect(revisions.slice(1).every((revision, index) => revision! > revisions[index]!)).toBe(true);
      expect(new Set(transportEpochs).size).toBe(1);
      expect(transportEpochs[0]).toMatch(/\S/u);
    } finally {
      mounted.unmount();
    }
  });

  it("sends a small explicit seek immediately with its discontinuity generation", async () => {
    const controller = new FakeDesktopLyricsController(visibleState());
    const playbackSession = new FakePlaybackSession({
      state: {
        queue: [track("one")],
        currentIndex: 0,
        currentTime: 10,
        duration: 180,
        isPlaying: true,
      },
    });
    const mounted = mountBridge(controller, playbackSession);
    try {
      await waitFor(() => controller.sendClock.mock.calls.length > 0);
      controller.sendClock.mockClear();

      playbackSession.seekTo(9.9);
      await waitFor(() => controller.sendClock.mock.calls.length === 1);

      expect(controller.sendClock).toHaveBeenCalledWith(expect.objectContaining({
        positionSeconds: 9.9,
        positionDiscontinuityVersion: 1,
      }));
    } finally {
      mounted.unmount();
    }
  });
});

function mountBridge(controller: FakeDesktopLyricsController, playbackSession: FakePlaybackSession) {
  const pinia = createPinia();
  const services = {
    catalog: { lyrics: vi.fn(async () => null) },
    playbackSession,
    desktopLyricsController: controller,
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
  return mount(defineComponent({
    setup() {
      useDesktopLyricsBridge();
      return () => h("div");
    },
  }), {
    global: {
      plugins: [pinia],
      provide: { [applicationServicesKey as symbol]: services },
    },
  });
}

class FakeDesktopLyricsController implements DesktopLyricsController {
  private stateValue: DesktopLyricsControllerState;
  private readonly stateListeners = new Set<(state: DesktopLyricsControllerState) => void>();
  private playbackRequestListener: ((request: DesktopLyricsPlaybackRequest) => void) | undefined;
  readonly removePlaybackRequests = vi.fn();
  readonly initialize = vi.fn(async () => undefined);
  readonly setVisible = vi.fn(async () => undefined);
  readonly toggleVisible = vi.fn(async () => undefined);
  readonly setLocked = vi.fn(async () => undefined);
  readonly setFullscreenBehavior = vi.fn(async () => undefined);
  readonly setFontScale = vi.fn();
  readonly setTextColor = vi.fn();
  readonly setHighlightColor = vi.fn();
  readonly sendSnapshot: ReturnType<typeof vi.fn>;
  readonly sendClock: ReturnType<typeof vi.fn>;
  readonly dispose = vi.fn();

  constructor(
    state: DesktopLyricsControllerState,
    overrides: Partial<Pick<DesktopLyricsController, "sendSnapshot" | "sendClock">> = {},
  ) {
    this.stateValue = state;
    this.sendSnapshot = vi.fn(async (_snapshot: DesktopLyricsSnapshot) => undefined);
    this.sendClock = vi.fn(async (_clock: DesktopLyricsClock) => undefined);
    if (overrides.sendSnapshot) this.sendSnapshot = overrides.sendSnapshot as ReturnType<typeof vi.fn>;
    if (overrides.sendClock) this.sendClock = overrides.sendClock as ReturnType<typeof vi.fn>;
  }

  state(): DesktopLyricsControllerState {
    return this.stateValue;
  }

  subscribe(listener: (state: DesktopLyricsControllerState) => void): () => void {
    this.stateListeners.add(listener);
    listener(this.stateValue);
    return () => this.stateListeners.delete(listener);
  }

  subscribePlaybackRequests(listener: (request: DesktopLyricsPlaybackRequest) => void): () => void {
    this.playbackRequestListener = listener;
    return this.removePlaybackRequests;
  }

  emitRequest(request: DesktopLyricsPlaybackRequest): void {
    this.playbackRequestListener?.(request);
  }
}

function visibleState(): DesktopLyricsControllerState {
  return {
    visible: true,
    actuallyVisible: true,
    locked: false,
    hiddenForFullscreen: false,
    fullscreenBehavior: "show",
    fontScale: 1,
    textColor: "#f4f5f7",
    highlightColor: "#cf9437",
  };
}

function track(id: string): Track {
  return {
    id,
    title: id,
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

function deferred<T>() {
  let resolve!: (value: T | PromiseLike<T>) => void;
  const promise = new Promise<T>((resolvePromise) => { resolve = resolvePromise; });
  return { promise, resolve };
}

async function waitFor(condition: () => boolean): Promise<void> {
  for (let attempt = 0; attempt < 10; attempt += 1) {
    if (condition()) return;
    await settle();
  }
  throw new Error("Expected condition to become true");
}

async function settle(): Promise<void> {
  await Promise.resolve();
  await nextTick();
  await Promise.resolve();
}
