import { describe, expect, it, vi } from "vitest";
import type { AudioPlayer, AudioSnapshot } from "../src/application/ports/AudioPlayer";
import type { DesktopWindow } from "../src/application/ports/DesktopWindow";
import type { Diagnostics } from "../src/application/ports/Diagnostics";
import type { Notifier } from "../src/application/ports/Notifier";
import type { PageLifecycle } from "../src/application/ports/PageLifecycle";
import type { SessionIdGenerator } from "../src/application/ports/SessionIdGenerator";
import type { TaskScheduler } from "../src/application/ports/TaskScheduler";
import type { Track } from "../src/domain/music";
import type { PlaybackUseCases } from "../src/application/use-cases/PlaybackUseCases";
import type { PlaybackGrantCache } from "../src/application/services/PlaybackGrantCache";
import type { PlaybackDesktopIntegration } from "../src/application/services/PlaybackDesktopIntegration";
import type { PlaybackPreferences } from "../src/application/services/PlaybackPreferences";
import type { PlaybackStatePersistence } from "../src/application/services/PlaybackStatePersistence";
import { PlaybackSession } from "../src/application/services/PlaybackSession";

describe("playback session", () => {
  it("publishes a queue revision synchronously before asynchronous playback begins", async () => {
    const harness = createHarness();
    const tracks = [track("one"), track("two")];

    const started = harness.session.startQueue(tracks, 0);

    expect(started?.revision).toBe(1);
    expect(harness.session.state()).toMatchObject({
      queue: tracks,
      queueVersion: 1,
      currentIndex: 0,
      loading: true,
    });
    expect(harness.session.appendToQueue(0, [track("stale")])).toBe(false);

    await started?.playback;

    expect(harness.audio.load).toHaveBeenCalledWith("https://example.test/one.mp3", expect.any(AbortSignal));
    expect(harness.audio.play).toHaveBeenCalledOnce();
    expect(harness.session.state().isPlaying).toBe(true);
    expect(harness.session.appendToQueue(1, [track("three")])).toBe(true);
    expect(harness.session.state().queue.map((item) => item.id)).toEqual(["one", "two", "three"]);

    harness.session.dispose();
  });

  it("refreshes an expired grant before resuming and preserves the playback position", async () => {
    const harness = createHarness();
    const started = harness.session.startQueue([track("one")], 0);
    await started?.playback;

    harness.audio.setPlaybackPosition(42);
    await harness.session.toggle();
    harness.grants.getForResume.mockResolvedValueOnce({
      grant: { url: "https://example.test/one-refreshed.mp3", expiresAt: "", selectedQuality: "AUTO" },
      refreshed: true,
    });

    await harness.session.toggle();

    expect(harness.grants.getForResume).toHaveBeenCalledWith("one", "AUTO", expect.any(AbortSignal));
    expect(harness.audio.load).toHaveBeenNthCalledWith(2, "https://example.test/one-refreshed.mp3", expect.any(AbortSignal));
    expect(harness.audio.seek).toHaveBeenLastCalledWith(42);
    expect(harness.audio.play).toHaveBeenCalledTimes(2);
    expect(harness.session.state()).toMatchObject({ currentTime: 42, isPlaying: true, loading: false });

    harness.session.dispose();
  });

  it("invalidates the grant and reloads after a resume play failure", async () => {
    const harness = createHarness();
    const started = harness.session.startQueue([track("one")], 0);
    await started?.playback;

    harness.audio.setPlaybackPosition(42);
    await harness.session.toggle();
    harness.grants.getForResume.mockResolvedValueOnce({
      grant: { url: "https://example.test/one.mp3", expiresAt: "", selectedQuality: "AUTO" },
      refreshed: false,
    });
    harness.grants.get.mockResolvedValueOnce({
      url: "https://example.test/one-recovered.mp3",
      expiresAt: "",
      selectedQuality: "AUTO",
    });
    harness.audio.play.mockRejectedValueOnce(new Error("expired media URL"));

    await harness.session.toggle();

    expect(harness.grants.invalidate).toHaveBeenCalledWith("one", "AUTO");
    expect(harness.audio.load).toHaveBeenNthCalledWith(2, "https://example.test/one-recovered.mp3", expect.any(AbortSignal));
    expect(harness.audio.seek).toHaveBeenLastCalledWith(42);
    expect(harness.audio.play).toHaveBeenCalledTimes(3);
    expect(harness.session.state()).toMatchObject({ currentTime: 42, isPlaying: true, loading: false, error: "" });

    harness.session.dispose();
  });

  it("recovers an active track after a media error without auto-playing a paused track", async () => {
    const harness = createHarness();
    const started = harness.session.startQueue([track("one")], 0);
    await started?.playback;
    harness.audio.setPlaybackPosition(42);
    harness.grants.get.mockResolvedValueOnce({
      url: "https://example.test/one-recovered.mp3",
      expiresAt: "",
      selectedQuality: "AUTO",
    });

    harness.audio.emitError("expired media URL");
    await vi.waitFor(() => expect(harness.session.state()).toMatchObject({ currentTime: 42, isPlaying: true, loading: false }));

    expect(harness.audio.load).toHaveBeenNthCalledWith(2, "https://example.test/one-recovered.mp3", expect.any(AbortSignal));
    expect(harness.audio.play).toHaveBeenCalledTimes(2);

    await harness.session.toggle();
    const playCallsBeforePausedError = harness.audio.play.mock.calls.length;
    harness.audio.emitError("ignored while paused");
    await Promise.resolve();

    expect(harness.audio.play).toHaveBeenCalledTimes(playCallsBeforePausedError);
    harness.session.dispose();
  });

  it("owns immutable snapshots for seeded, started, and appended queues", async () => {
    const harness = createHarness();
    const seeded = [track("seed")];

    harness.session.seed(seeded);
    expectOwnedQueue(harness.session.state().queue, seeded);
    seeded[0]!.title = "changed outside playback";
    seeded[0]!.artistIds.push("outside");
    expect(harness.session.state().queue[0]).toMatchObject({ title: "seed", artistIds: ["artist-1"] });

    harness.session.reset();
    const startedTracks = [track("one")];
    const started = harness.session.startQueue(startedTracks, 0);
    expect(started).not.toBeNull();
    expectOwnedQueue(harness.session.state().queue, startedTracks);
    await started?.playback;

    const appended = [track("two")];
    expect(harness.session.appendToQueue(started!.revision, appended)).toBe(true);
    expect(Object.isFrozen(harness.session.state().queue)).toBe(true);
    expectOwnedTrack(harness.session.state().queue[1], appended[0]);
    harness.session.dispose();
  });

  it("owns an immutable copy of a restored queue", () => {
    const restoredTrack = track("restored");
    const harness = createHarness({
      restore: () => ({
        ownerKey: "server|user",
        queue: [restoredTrack],
        currentIndex: 0,
        position: 0,
        shuffled: false,
        repeat: false,
        repeatMode: "off",
        quality: "AUTO",
        crossfadeSeconds: 0,
        savedAt: "2026-08-01T00:00:00.000Z",
      }),
    });

    expect(harness.session.restoreState("server|user")).toBe(true);
    expectOwnedQueue(harness.session.state().queue, [restoredTrack]);
    restoredTrack.liked = true;
    expect(harness.session.state().queue[0]?.liked).toBe(false);
    harness.session.dispose();
  });

  it("restores a resumable queue without loading or autoplaying its track", () => {
    const restoredTrack = track("restored");
    const harness = createHarness({
      restore: () => ({
        ownerKey: "server|user",
        queue: [restoredTrack],
        currentIndex: 0,
        position: 42,
        shuffled: false,
        repeat: false,
        repeatMode: "off",
        quality: "AUTO",
        crossfadeSeconds: 0,
        savedAt: "2026-08-01T00:00:00.000Z",
      }),
    });

    expect(harness.session.restoreState("server|user")).toBe(true);
    expect(harness.audio.load).not.toHaveBeenCalled();
    expect(harness.audio.play).not.toHaveBeenCalled();
    expect(harness.session.state()).toMatchObject({
      queue: [restoredTrack],
      currentIndex: 0,
      currentTime: 42,
      isPlaying: false,
    });

    harness.session.dispose();
  });

  it("does not checkpoint the stopped old track while replacing it", async () => {
    const harness = createHarness();
    const tracks = [track("one"), track("two")];
    await harness.session.startQueue(tracks, 0)?.playback;
    harness.audio.setPlaybackPosition(42);
    harness.persistence.scheduleCheckpoint.mockClear();

    const replacement = harness.session.playAt(1);

    expect(harness.persistence.scheduleCheckpoint).not.toHaveBeenCalled();
    await replacement;
    harness.session.dispose();
  });

  it("cancels an older load before restoring a different playback owner", async () => {
    let resolveLoad!: () => void;
    const deferredLoad = new Promise<void>((resolve) => { resolveLoad = resolve; });
    const restoredTrack = track("restored");
    const harness = createHarness({
      restore: () => ({
        ownerKey: "server|new-user",
        queue: [restoredTrack],
        currentIndex: 0,
        position: 24,
        shuffled: false,
        repeat: false,
        repeatMode: "off",
        quality: "AUTO",
        crossfadeSeconds: 0,
        savedAt: "2026-08-01T00:00:00.000Z",
      }),
    });
    harness.audio.setLoadResult(deferredLoad);
    const loading = harness.session.startQueue([track("old")], 0);
    await waitForLoad(harness.audio);

    expect(harness.session.restoreState("server|new-user")).toBe(true);
    resolveLoad();
    await loading?.playback;

    expect(harness.audio.play).not.toHaveBeenCalled();
    expect(harness.session.state()).toMatchObject({
      queue: [restoredTrack],
      currentIndex: 0,
      currentTime: 24,
      isPlaying: false,
    });

    harness.session.dispose();
  });

  it("clears an older queue when the next owner has no restorable playback state", async () => {
    const harness = createHarness({ restore: () => null });
    await harness.session.startQueue([track("old")], 0)?.playback;

    expect(harness.session.restoreState("server|new-user")).toBe(false);
    expect(harness.session.state()).toMatchObject({
      queue: [],
      currentIndex: -1,
      currentTime: 0,
      duration: 0,
      loading: false,
    });

    harness.session.dispose();
  });

  it("clears loading when playback is stopped during a deferred load", async () => {
    let resolveLoad!: () => void;
    const deferredLoad = new Promise<void>((resolve) => { resolveLoad = resolve; });
    const harness = createHarness();
    harness.audio.setLoadResult(deferredLoad);
    const loading = harness.session.startQueue([track("slow")], 0);
    await waitForLoad(harness.audio);

    harness.session.stopPlayback();

    expect(harness.session.state().loading).toBe(false);
    resolveLoad();
    await loading?.playback;
    expect(harness.audio.play).not.toHaveBeenCalled();

    harness.session.dispose();
  });

  it("ignores a mini-mode enable that resolves after reset", async () => {
    let resolveEnable!: () => void;
    const desktopWindow = {
      setMiniMode: vi.fn((enabled: boolean) => enabled
        ? new Promise<void>((resolve) => { resolveEnable = resolve; })
        : Promise.resolve()),
    } as unknown as DesktopWindow;
    const harness = createHarness({ desktopWindow });
    await harness.session.startQueue([track("one")], 0)?.playback;

    const enabling = harness.session.setMiniMode(true);
    harness.session.reset();
    resolveEnable();
    await enabling;

    expect(harness.session.state()).toMatchObject({ miniMode: false, queue: [], currentIndex: -1 });
    expect(desktopWindow.setMiniMode).toHaveBeenNthCalledWith(1, true);
    expect(desktopWindow.setMiniMode).toHaveBeenNthCalledWith(2, false);

    harness.session.dispose();
  });

  it("owns page-hide flushing and removes the lifecycle listener on disposal", () => {
    const harness = createHarness();

    harness.session.setVolume(35);
    harness.lifecycle.emitPageHide();

    expect(harness.preferences.flush).toHaveBeenCalledOnce();
    expect(harness.persistence.flush).toHaveBeenCalledOnce();

    harness.session.dispose();
    harness.lifecycle.emitPageHide();

    expect(harness.preferences.flush).toHaveBeenCalledTimes(2);
    expect(harness.persistence.flush).toHaveBeenCalledTimes(2);
  });
});

function createHarness(options: {
  restore?: () => unknown;
  desktopWindow?: DesktopWindow;
} = {}) {
  const audio = new FakeAudioPlayer();
  const lifecycle = new FakePageLifecycle();
  const preferences = {
    read: vi.fn(() => ({ volume: 72, quality: "AUTO" as const, crossfadeSeconds: 0, notificationsEnabled: false, hasCrossfadePreference: true })),
    initializeVolume: vi.fn((value: number) => value),
    setVolume: vi.fn((value: number) => value),
    setQuality: vi.fn(),
    setCrossfadeSeconds: vi.fn((value: number) => value),
    setNotificationsEnabled: vi.fn(),
    flush: vi.fn(),
    dispose: vi.fn(),
  } as unknown as PlaybackPreferences & { flush: ReturnType<typeof vi.fn> };
  const persistence = {
    persistedPosition: -1,
    ownerKey: "",
    restore: vi.fn(options.restore ?? (() => null)),
    setRestoredPosition: vi.fn(),
    detach: vi.fn(),
    clear: vi.fn(),
    scheduleSnapshot: vi.fn(),
    scheduleCheckpoint: vi.fn(),
    flush: vi.fn(),
    dispose: vi.fn(),
  } as unknown as PlaybackStatePersistence & {
    flush: ReturnType<typeof vi.fn>;
    scheduleCheckpoint: ReturnType<typeof vi.fn>;
  };
  const grants = {
    get: vi.fn(async (trackId: string) => ({ url: `https://example.test/${trackId}.mp3`, expiresAt: "", selectedQuality: "AUTO" })),
    getForResume: vi.fn(async (trackId: string) => ({
      grant: { url: `https://example.test/${trackId}.mp3`, expiresAt: "", selectedQuality: "AUTO" },
      refreshed: false,
    })),
    invalidate: vi.fn(),
    clear: vi.fn(),
  };
  const grantCache = grants as unknown as PlaybackGrantCache;
  const desktop = {
    connect: vi.fn(),
    setTrack: vi.fn(),
    setPlayback: vi.fn(),
    clear: vi.fn(),
    dispose: vi.fn(),
  } as unknown as PlaybackDesktopIntegration;
  const desktopWindow = options.desktopWindow ?? { setMiniMode: vi.fn(async () => undefined) } as unknown as DesktopWindow;
  let identifier = 0;
  const session = new PlaybackSession(
    audio,
    { record: vi.fn(async () => undefined) } as unknown as PlaybackUseCases,
    grantCache,
    persistence,
    preferences,
    desktop,
    desktopWindow,
    diagnostics(),
    { notify: vi.fn(async () => undefined) } as unknown as Notifier,
    scheduler(),
    lifecycle,
    { next: () => `session-${++identifier}` } as SessionIdGenerator,
  );
  return { session, audio, lifecycle, preferences, persistence, grants, desktop, desktopWindow };
}

class FakeAudioPlayer implements AudioPlayer {
  private snapshotValue: AudioSnapshot = { currentTime: 0, duration: 180, paused: true };
  private loadResult: Promise<void> = Promise.resolve();
  private readonly updateListeners = new Set<(snapshot: AudioSnapshot) => void>();
  private readonly endedListeners = new Set<() => void>();
  private readonly errorListeners = new Set<(message: string) => void>();
  readonly load = vi.fn(async () => this.loadResult);
  readonly play = vi.fn(async () => {
    this.snapshotValue = { ...this.snapshotValue, paused: false };
    this.emitUpdate();
  });
  readonly pause = vi.fn(() => {
    this.snapshotValue = { ...this.snapshotValue, paused: true };
    this.emitUpdate();
  });
  readonly stop = vi.fn(() => {
    this.snapshotValue = { ...this.snapshotValue, currentTime: 0, paused: true };
    this.emitUpdate();
  });
  readonly seek = vi.fn((seconds: number) => {
    this.snapshotValue = { ...this.snapshotValue, currentTime: seconds };
    this.emitUpdate();
  });
  readonly setVolume = vi.fn();

  snapshot(): AudioSnapshot { return this.snapshotValue; }
  onUpdate(listener: (snapshot: AudioSnapshot) => void): () => void {
    this.updateListeners.add(listener);
    return () => this.updateListeners.delete(listener);
  }
  onEnded(listener: () => void): () => void {
    this.endedListeners.add(listener);
    return () => this.endedListeners.delete(listener);
  }
  onError(listener: (message: string) => void): () => void {
    this.errorListeners.add(listener);
    return () => this.errorListeners.delete(listener);
  }

  setPlaybackPosition(currentTime: number): void {
    this.snapshotValue = { ...this.snapshotValue, currentTime };
    this.emitUpdate();
  }

  setLoadResult(result: Promise<void>): void {
    this.loadResult = result;
  }

  emitError(message: string): void {
    for (const listener of this.errorListeners) listener(message);
  }

  private emitUpdate(): void {
    for (const listener of this.updateListeners) listener(this.snapshotValue);
  }
}

class FakePageLifecycle implements PageLifecycle {
  private listener: (() => void) | undefined;

  onPageHide(listener: () => void): () => void {
    this.listener = listener;
    return () => {
      if (this.listener === listener) this.listener = undefined;
    };
  }

  emitPageHide(): void {
    this.listener?.();
  }
}

function diagnostics(): Diagnostics {
  return { info() {}, warn() {}, error() {}, entries: () => [], clear() {} };
}

function scheduler(): TaskScheduler {
  return {
    delay: (callback) => {
      const handle = window.setTimeout(callback, 0);
      return () => window.clearTimeout(handle);
    },
    whenIdle: (callback) => {
      const handle = window.setTimeout(callback, 0);
      return () => window.clearTimeout(handle);
    },
  };
}

async function waitForLoad(audio: FakeAudioPlayer): Promise<void> {
  for (let attempt = 0; attempt < 5; attempt += 1) {
    if (audio.load.mock.calls.length) return;
    await Promise.resolve();
  }
  throw new Error("Expected the audio load to begin");
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
    publishedAt: "2026-08-01T00:00:00.000Z",
  };
}

function expectOwnedQueue(snapshot: readonly unknown[], source: Track[]): void {
  expect(snapshot).not.toBe(source);
  expect(Object.isFrozen(snapshot)).toBe(true);
  expectOwnedTrack(snapshot[0], source[0]);
}

function expectOwnedTrack(snapshot: unknown, source: Track | undefined): void {
  expect(snapshot).not.toBe(source);
  expect(Object.isFrozen(snapshot)).toBe(true);
  expect(Object.isFrozen((snapshot as Track).artistIds)).toBe(true);
  expect(Reflect.set(snapshot as object, "liked", true)).toBe(false);
}
