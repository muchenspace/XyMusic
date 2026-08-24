import { describe, expect, it, vi } from "vitest";
import type { DesktopIntegration, DesktopMediaAction } from "../src/application/ports/DesktopIntegration";
import type { Diagnostics } from "../src/application/ports/Diagnostics";
import type { Track } from "../src/domain/music";
import { PlaybackDesktopIntegration } from "../src/application/services/PlaybackDesktopIntegration";

describe("playback desktop integration", () => {
  it("maps native commands, validates seek positions, and coalesces outbound state", async () => {
    let listener!: (action: DesktopMediaAction, position?: number) => void;
    const metadata = vi.fn(async () => undefined);
    const playback = vi.fn(async () => undefined);
    const clear = vi.fn(async () => undefined);
    const desktop = createDesktop({
      onMediaAction: async (next) => {
        listener = next;
        return () => undefined;
      },
      updateMediaMetadata: metadata,
      updateMediaPlayback: playback,
      clearMediaSession: clear,
    });
    const commands = {
      play: vi.fn(),
      pause: vi.fn(),
      toggle: vi.fn(),
      previous: vi.fn(),
      next: vi.fn(),
      stop: vi.fn(),
      seekTo: vi.fn(),
    };
    const service = new PlaybackDesktopIntegration(desktop, diagnostics());

    service.connect(commands);
    listener("play");
    listener("pause");
    listener("toggle");
    listener("previous");
    listener("next");
    listener("stop");
    listener("seek");
    listener("seek", Number.NaN);
    listener("seek", 42);

    expect(commands.play).toHaveBeenCalledOnce();
    expect(commands.pause).toHaveBeenCalledOnce();
    expect(commands.toggle).toHaveBeenCalledOnce();
    expect(commands.previous).toHaveBeenCalledOnce();
    expect(commands.next).toHaveBeenCalledOnce();
    expect(commands.stop).toHaveBeenCalledOnce();
    expect(commands.seekTo).toHaveBeenCalledTimes(1);
    expect(commands.seekTo).toHaveBeenCalledWith(42);

    service.setTrack(track());
    service.setPlayback("playing", 42, 180);
    await service.whenIdle();
    expect(metadata).toHaveBeenCalledWith({
      title: "Track",
      artist: "Artist",
      album: "Album",
      artworkUrl: "https://example.test/cover.jpg",
    });
    expect(playback).toHaveBeenCalledWith({ status: "playing", position: 42, duration: 180 });

    service.clear();
    await service.whenIdle();
    expect(clear).toHaveBeenCalledOnce();
  });

  it("removes a late media-action subscription after disposal", async () => {
    let resolveSubscription!: (remove: () => void) => void;
    const remove = vi.fn();
    const desktop = createDesktop({
      onMediaAction: () => new Promise((resolve) => {
        resolveSubscription = resolve;
      }),
    });
    const service = new PlaybackDesktopIntegration(desktop, diagnostics());

    service.connect(noopCommands());
    service.dispose();
    resolveSubscription(remove);
    await Promise.resolve();

    expect(remove).toHaveBeenCalledOnce();
  });

  it("reports command failures without breaking the native listener", async () => {
    let listener!: (action: "next") => void;
    const warn = vi.fn();
    const desktop = createDesktop({
      onMediaAction: async (next) => {
        listener = next as (action: "next") => void;
        return () => undefined;
      },
    });
    const commands = noopCommands();
    commands.next.mockImplementation(() => { throw new Error("command failed"); });
    const service = new PlaybackDesktopIntegration(desktop, diagnostics(warn));

    service.connect(commands);
    listener("next");

    expect(warn).toHaveBeenCalledWith("media-session", "Could not handle native media action: command failed");
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

function diagnostics(warn = vi.fn()): Diagnostics {
  return { info() {}, warn, error() {} };
}

function noopCommands() {
  return {
    play: vi.fn(),
    pause: vi.fn(),
    toggle: vi.fn(),
    previous: vi.fn(),
    next: vi.fn(),
    stop: vi.fn(),
    seekTo: vi.fn(),
  };
}

function track(): Track {
  return {
    id: "track-1",
    title: "Track",
    artist: "Artist",
    artistIds: ["artist-1"],
    album: "Album",
    albumId: "album-1",
    coverUrl: "https://example.test/cover.jpg",
    duration: 180,
    liked: false,
    publishedAt: "2026-08-01T00:00:00.000Z",
  };
}
