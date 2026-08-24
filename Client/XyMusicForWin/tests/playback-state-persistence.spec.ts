import { describe, expect, it, vi } from "vitest";
import type { Diagnostics } from "../src/application/ports/Diagnostics";
import type { TaskScheduler } from "../src/application/ports/TaskScheduler";
import type { PlaybackStateUseCases } from "../src/application/use-cases/PlaybackStateUseCases";
import { PlaybackStatePersistence } from "../src/application/services/PlaybackStatePersistence";

describe("playback state persistence", () => {
  it("coalesces snapshot work and persists the latest state only after idle time", () => {
    const scheduler = new ManualTaskScheduler();
    const state = createPlaybackState();
    const persistence = new PlaybackStatePersistence(state.useCases, diagnostics(), scheduler);
    persistence.restore("owner-1");
    const first = snapshot(12);
    const latest = snapshot(48);

    persistence.scheduleSnapshot(() => first);
    persistence.scheduleSnapshot(() => latest);
    expect(state.save).not.toHaveBeenCalled();

    scheduler.runDelayed();
    expect(state.save).not.toHaveBeenCalled();
    scheduler.runIdle();

    expect(state.save).toHaveBeenCalledExactlyOnceWith(expect.objectContaining({
      ownerKey: "owner-1",
      position: 48,
    }));
    expect(persistence.persistedPosition).toBe(48);
  });

  it("writes a checkpoint after a persisted snapshot and flushes pending state synchronously", () => {
    const scheduler = new ManualTaskScheduler();
    const state = createPlaybackState();
    const persistence = new PlaybackStatePersistence(state.useCases, diagnostics(), scheduler);
    persistence.restore("owner-1");

    persistence.scheduleSnapshot(() => snapshot(10));
    scheduler.runDelayed();
    scheduler.runIdle();
    persistence.scheduleCheckpoint(() => ({ ownerKey: "owner-1", currentIndex: 0, trackId: "track-1", position: 28 }));
    scheduler.runDelayed();
    scheduler.runIdle();

    expect(state.checkpoint).toHaveBeenCalledExactlyOnceWith(expect.objectContaining({
      position: 28,
      snapshotSavedAt: expect.any(String),
    }));

    persistence.scheduleSnapshot(() => snapshot(63));
    persistence.flush(() => snapshot(64), () => null);
    expect(state.save).toHaveBeenLastCalledWith(expect.objectContaining({ position: 64 }));
    scheduler.runDelayed();
    scheduler.runIdle();
    expect(state.save).toHaveBeenCalledTimes(2);
  });

  it("falls back to the current track when a quota error prevents saving a full queue", () => {
    const scheduler = new ManualTaskScheduler();
    const state = createPlaybackState();
    state.save.mockImplementationOnce(() => { throw new DOMException("full", "QuotaExceededError"); });
    const warn = vi.fn();
    const persistence = new PlaybackStatePersistence(state.useCases, diagnostics(warn), scheduler);
    persistence.restore("owner-1");

    persistence.scheduleSnapshot(() => ({ ...snapshot(16), queue: [track("track-0"), track("track-1")], currentIndex: 1 }));
    scheduler.runDelayed();
    scheduler.runIdle();

    expect(state.save).toHaveBeenCalledTimes(2);
    expect(state.save).toHaveBeenLastCalledWith(expect.objectContaining({
      queue: [expect.objectContaining({ id: "track-1" })],
      currentIndex: 0,
    }));
    expect(warn).toHaveBeenCalledWith("playback-state", "Playback queue exceeded local storage quota; saved the current track only");
  });
});

function createPlaybackState() {
  const save = vi.fn();
  const checkpoint = vi.fn();
  return {
    save,
    checkpoint,
    useCases: {
      restore: vi.fn(() => null),
      save,
      checkpoint,
      clear: vi.fn(),
    } as unknown as PlaybackStateUseCases,
  };
}

function diagnostics(warn = vi.fn()): Diagnostics {
  return { info() {}, warn, error() {} };
}

function snapshot(position: number) {
  return {
    ownerKey: "owner-1",
    queue: [track("track-1")],
    currentIndex: 0,
    position,
    shuffled: false,
    repeat: false,
    repeatMode: "off" as const,
    quality: "AUTO" as const,
    crossfadeSeconds: 0,
  };
}

function track(id: string) {
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

class ManualTaskScheduler implements TaskScheduler {
  private readonly delayed = new Set<() => void>();
  private readonly idle = new Set<() => void>();

  delay(callback: () => void): () => void {
    this.delayed.add(callback);
    return () => this.delayed.delete(callback);
  }

  whenIdle(callback: () => void): () => void {
    this.idle.add(callback);
    return () => this.idle.delete(callback);
  }

  runDelayed(): void {
    const callbacks = [...this.delayed];
    this.delayed.clear();
    for (const callback of callbacks) callback();
  }

  runIdle(): void {
    const callbacks = [...this.idle];
    this.idle.clear();
    for (const callback of callbacks) callback();
  }
}
