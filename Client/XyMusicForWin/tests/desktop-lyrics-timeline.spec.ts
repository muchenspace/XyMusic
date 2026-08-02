import { describe, expect, it } from "vitest";
import type { Lyrics } from "../src/domain/music";
import { shouldReanchorLyricPlaybackClock } from "../src/domain/lyricsTimeline";
import { buildDesktopLyricsFrame, estimatePlaybackSeconds } from "../src/desktop-lyrics/timeline";
import type { DesktopLyricsClockPayload } from "../src/desktop-lyrics/protocol";

describe("desktop lyrics timeline", () => {
  it("does not reanchor on a normal coarse sample but does reanchor a pause or jump", () => {
    const current = clock({ isPlaying: true, positionSeconds: 0, anchoredAtMs: 0 });

    expect(shouldReanchorLyricPlaybackClock(current, { ...current, positionSeconds: 0.1, anchoredAtMs: 34 }, 50)).toBe(false);
    expect(shouldReanchorLyricPlaybackClock(current, { ...current, isPlaying: false, positionSeconds: 0.05, anchoredAtMs: 50 }, 50)).toBe(true);
    expect(shouldReanchorLyricPlaybackClock(current, { ...current, positionSeconds: 1, anchoredAtMs: 50 }, 50)).toBe(true);
  });

  it("interpolates a playing clock and keeps a paused clock fixed", () => {
    const playing = clock({ isPlaying: true, positionSeconds: 12, anchoredAtMs: 1_000 });
    const paused = clock({ isPlaying: false, positionSeconds: 12, anchoredAtMs: 1_000 });

    expect(estimatePlaybackSeconds(playing, 2_500)).toBe(13.5);
    expect(estimatePlaybackSeconds(paused, 2_500)).toBe(12);
  });

  it("selects the latest line for repeated timestamps", () => {
    const lyrics: Lyrics = {
      ...synchronizedLyrics(),
      lines: [
        { time: 0, text: "first" },
        { time: 2, text: "second" },
        { time: 2, text: "replacement" },
        { time: 4, text: "fourth" },
      ],
    };

    expect(buildDesktopLyricsFrame(lyrics, clock({ positionSeconds: 2 }), 0, 0).activeIndex).toBe(2);
  });

  it("builds current and next ordinary LRC lines without estimating line progress", () => {
    const frame = buildDesktopLyricsFrame(synchronizedLyrics(), clock({
      isPlaying: true,
      positionSeconds: 0.5,
      anchoredAtMs: 1_000,
    }), 0, 1_000);

    expect(frame.activeIndex).toBe(0);
    expect(frame.current?.line.text).toBe("hello world");
    expect(frame.current?.wordIndex).toBe(-1);
    expect(frame.next?.line.text).toBe("next line");
  });

  it("keeps the final line active without a fixed duration fallback", () => {
    const frame = buildDesktopLyricsFrame(synchronizedLyrics(), clock({ positionSeconds: 6 }), 0, 0);

    expect(frame.current?.line.text).toBe("last line");
    expect(frame.current?.started).toBe(true);
    expect(frame.current?.wordIndex).toBe(-1);
  });

  it("keeps the final line active after its start time", () => {
    const frame = buildDesktopLyricsFrame(synchronizedLyrics(), clock({ positionSeconds: 8 }), 0, 0);

    expect(frame.current?.line.text).toBe("last line");
    expect(frame.current?.started).toBe(true);
  });

  it("selects explicit word timing without deriving a line progress", () => {
    const lyrics: Lyrics = {
      trackId: "track-1",
      source: "qmusic",
      synchronized: true,
      timing: "WORD",
      lines: [{
        time: 1,
        text: "hello world",
        words: [
          { time: 1, endTime: 1.5, text: "hello" },
          { time: 1.5, endTime: 2.5, text: " world" },
        ],
      }],
    };
    const frame = buildDesktopLyricsFrame(lyrics, clock({ positionSeconds: 1.75 }), 0, 0);

    expect(frame.activeIndex).toBe(0);
    expect(frame.current?.wordIndex).toBe(1);
  });

  it("applies the per-track offset and rejects stale track clocks", () => {
    const lyrics = synchronizedLyrics();
    const shifted = buildDesktopLyricsFrame(lyrics, clock({ positionSeconds: 1 }), 1.2, 0);
    const stale = buildDesktopLyricsFrame(lyrics, clock({ trackId: "other", positionSeconds: 1 }), 0, 0);

    expect(shifted.activeIndex).toBe(1);
    expect(shifted.current?.line.text).toBe("next line");
    expect(stale.current).toBeNull();
  });

  it("shows the first two lines without falsely marking plain lyrics as synchronized", () => {
    const lyrics: Lyrics = {
      trackId: "track-1",
      source: "plain",
      synchronized: false,
      timing: "LINE",
      lines: [
        { time: null, text: "plain first" },
        { time: null, text: "plain second" },
      ],
    };
    const frame = buildDesktopLyricsFrame(lyrics, clock(), 0, 0);

    expect(frame.activeIndex).toBe(-1);
    expect(frame.current?.line.text).toBe("plain first");
    expect(frame.current?.started).toBe(false);
    expect(frame.next?.line.text).toBe("plain second");
  });
});

function clock(overrides: Partial<DesktopLyricsClockPayload> = {}): DesktopLyricsClockPayload {
  return {
    version: 4,
    transportEpoch: "test-main-window",
    trackId: "track-1",
    isPlaying: false,
    positionSeconds: 0,
    anchoredAtMs: 0,
    ...overrides,
  };
}

function synchronizedLyrics(): Lyrics {
  return {
    trackId: "track-1",
    source: "lrc",
    synchronized: true,
    timing: "LINE",
    lines: [
      { time: 0, text: "hello world" },
      { time: 2, text: "next line" },
      { time: 4, text: "last line" },
    ],
  };
}
