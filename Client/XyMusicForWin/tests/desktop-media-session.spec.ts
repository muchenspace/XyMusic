import { describe, expect, it, vi } from "vitest";
import type { DesktopIntegration } from "../src/application/ports/DesktopIntegration";
import { DesktopMediaSessionCoordinator } from "../src/application/services/DesktopMediaSessionCoordinator";

describe("desktop media session coordination", () => {
  it("clears stale metadata after an in-flight update finishes", async () => {
    const metadataGate = deferred<void>();
    const calls: string[] = [];
    const desktop = createDesktop({
      updateMediaMetadata: vi.fn(async () => {
        calls.push("metadata:start");
        await metadataGate.promise;
        calls.push("metadata:end");
      }),
      clearMediaSession: vi.fn(async () => { calls.push("clear"); }),
    });
    const coordinator = new DesktopMediaSessionCoordinator(desktop);

    coordinator.updateMetadata({ title: "Old track", artist: "Artist", album: "Album" });
    await Promise.resolve();
    coordinator.clear();

    expect(calls).toEqual(["metadata:start"]);
    metadataGate.resolve();
    await coordinator.whenIdle();
    expect(calls).toEqual(["metadata:start", "metadata:end", "clear"]);
  });

  it("continues with the latest state after a native update fails", async () => {
    const failures: string[] = [];
    const playback = vi.fn(async () => undefined);
    const desktop = createDesktop({
      updateMediaMetadata: vi.fn(async () => { throw new Error("native failure"); }),
      updateMediaPlayback: playback,
    });
    const coordinator = new DesktopMediaSessionCoordinator(desktop, (operation) => failures.push(operation));

    coordinator.updateMetadata({ title: "Track", artist: "Artist", album: "Album" });
    coordinator.updatePlayback({ status: "playing", position: 12, duration: 180 });
    await coordinator.whenIdle();

    expect(failures).toEqual(["metadata"]);
    expect(playback).toHaveBeenCalledWith({ status: "playing", position: 12, duration: 180 });
  });
});

function createDesktop(overrides: Partial<DesktopIntegration>): DesktopIntegration {
  return {
    async onMediaAction() { return () => undefined; },
    async updateMediaMetadata() { return undefined; },
    async updateMediaPlayback() { return undefined; },
    async clearMediaSession() { return undefined; },
    ...overrides,
  };
}

function deferred<T>() {
  let resolve!: (value: T | PromiseLike<T>) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise;
    reject = rejectPromise;
  });
  return { promise, resolve, reject };
}
