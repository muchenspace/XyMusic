import { mount } from "@vue/test-utils";
import { createPinia } from "pinia";
import { defineComponent, h, nextTick } from "vue";
import { describe, expect, it, vi } from "vitest";
import type {
  DesktopLyricsClock,
  DesktopLyricsSnapshot,
} from "../src/application/ports/DesktopLyrics";
import { DESKTOP_LYRICS_PROTOCOL_VERSION } from "../src/application/ports/DesktopLyrics";
import type {
  DesktopLyricsClockInput,
  DesktopLyricsController,
  DesktopLyricsControllerState,
  DesktopLyricsPlaybackRequest,
  DesktopLyricsSnapshotInput,
} from "../src/application/ports/DesktopLyricsController";
import type { ApplicationServices } from "../src/application/services";
import type { Track } from "../src/domain/music";
import { useDesktopLyricsBridge } from "../src/presentation/composables/useDesktopLyricsBridge";
import { applicationServicesKey } from "../src/presentation/services";
import { useLyricsStore } from "../src/presentation/stores/lyricsStore";
import { FakePlaybackSession } from "./support/FakePlaybackSession";
import { createTestLyricsPreferencePersistence } from "./support/lyricsPreferencePersistence";

const TEST_TRANSPORT_EPOCH = "test-main-window";

describe("desktop lyrics bridge", () => {
  it("routes playback requests through the player projection and cleans up its subscription", async () => {
    const controller = new FakeDesktopLyricsController(visibleState());
    const playbackSession = new FakePlaybackSession({
      state: { queue: [track("one"), track("two")], currentIndex: 0, duration: 180 },
    });
    const { wrapper } = mountBridge(controller, playbackSession);
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
      wrapper.unmount();
    }

    expect(controller.removePlaybackRequests).toHaveBeenCalledOnce();
    expect(controller.cancelPendingSends).toHaveBeenCalledOnce();
  });

  it("forwards playback time changes to the controller clock policy", async () => {
    const controller = new FakeDesktopLyricsController(visibleState());
    const playbackSession = new FakePlaybackSession({
      state: {
        queue: [track("one")],
        currentIndex: 0,
        currentTime: 0,
        duration: 180,
        isPlaying: true,
      },
    });
    const { wrapper } = mountBridge(controller, playbackSession);
    try {
      await waitFor(() => controller.offerClock.mock.calls.length > 0);
      controller.offerClock.mockClear();

      playbackSession.update({ currentTime: 3 });
      await nextTick();

      expect(controller.offerClock).toHaveBeenCalledOnce();
      const clock = controller.offerClock.mock.calls[0]?.[0]();
      expect(clock?.positionSeconds).toBe(3);
    } finally {
      wrapper.unmount();
    }
  });

  it("requests a snapshot when the lyric projection changes", async () => {
    const controller = new FakeDesktopLyricsController(visibleState());
    const playbackSession = new FakePlaybackSession({
      state: { queue: [track("one")], currentIndex: 0, duration: 180 },
    });
    const { wrapper, pinia } = mountBridge(controller, playbackSession);
    try {
      await settle();
      const lyricsStore = useLyricsStore(pinia);
      controller.requestSnapshot.mockClear();

      lyricsStore.showTranslation = false;
      await nextTick();

      expect(controller.requestSnapshot).toHaveBeenCalled();
    } finally {
      wrapper.unmount();
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
    const { wrapper } = mountBridge(controller, playbackSession);
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
      wrapper.unmount();
    }
  });
});

function mountBridge(controller: FakeDesktopLyricsController, playbackSession: FakePlaybackSession) {
  const pinia = createPinia();
  const uiPreferences = {
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
  const services = {
    catalog: { lyrics: vi.fn(async () => null) },
    playbackSession,
    desktopLyricsController: controller,
    uiPreferences,
    lyricsPreferencePersistence: createTestLyricsPreferencePersistence(uiPreferences),
  } as unknown as ApplicationServices;
  const wrapper = mount(defineComponent({
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
  return { wrapper, pinia };
}

class FakeDesktopLyricsController implements DesktopLyricsController {
  private stateValue: DesktopLyricsControllerState;
  private readonly stateListeners = new Set<(state: DesktopLyricsControllerState) => void>();
  private playbackRequestListener: ((request: DesktopLyricsPlaybackRequest) => void) | undefined;
  private transportRevision = 0;
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
  readonly requestSnapshot = vi.fn((create: () => DesktopLyricsSnapshotInput) => {
    this.sendSnapshot({
      version: DESKTOP_LYRICS_PROTOCOL_VERSION,
      transportEpoch: TEST_TRANSPORT_EPOCH,
      revision: ++this.transportRevision,
      ...create(),
    });
  });
  readonly scheduleSnapshot = vi.fn((create: () => DesktopLyricsSnapshotInput) => { this.requestSnapshot(create); });
  readonly offerClock = vi.fn((create: () => DesktopLyricsClockInput | null) => {
    const input = create();
    if (!input) return;
    this.sendClock({
      version: DESKTOP_LYRICS_PROTOCOL_VERSION,
      transportEpoch: TEST_TRANSPORT_EPOCH,
      revision: ++this.transportRevision,
      ...input,
    });
  });
  readonly discardPendingClock = vi.fn();
  readonly cancelPendingSends = vi.fn();
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
