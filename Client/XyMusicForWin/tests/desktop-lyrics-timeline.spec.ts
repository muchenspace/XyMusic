import { describe, expect, it } from "vitest";
import type { Lyrics } from "../src/domain/music";
import { buildDesktopLyricsFrame, estimatePlaybackSeconds, findActiveLyricIndex } from "../src/desktop-lyrics/timeline";
import type { DesktopLyricsClockPayload } from "../src/desktop-lyrics/protocol";

describe("desktop lyrics timeline", () => {
  it("interpolates a playing clock and keeps a paused clock fixed", () => {
    const playing = clock({ isPlaying: true, positionSeconds: 12, anchoredAtMs: 1_000 });
    const paused = clock({ isPlaying: false, positionSeconds: 12, anchoredAtMs: 1_000 });

    expect(estimatePlaybackSeconds(playing, 2_500)).toBe(13.5);
    expect(estimatePlaybackSeconds(paused, 2_500)).toBe(12);
  });

  it("selects the latest line for repeated timestamps", () => {
    expect(findActiveLyricIndex([
      { time: 0, text: "first" },
      { time: 2, text: "second" },
      { time: 2, text: "replacement" },
      { time: 4, text: "fourth" },
    ], 2)).toBe(2);
  });

  it("builds current, next, and ordinary LRC line progress from one anchor", () => {
    const frame = buildDesktopLyricsFrame(synchronizedLyrics(), clock({
      isPlaying: true,
      positionSeconds: 0.5,
      anchoredAtMs: 1_000,
    }), 0, 1_000);

    expect(frame.activeIndex).toBe(0);
    expect(frame.current?.line.text).toBe("hello world");
    expect(frame.current?.progress).toBeCloseTo(0.25);
    expect(frame.next?.line.text).toBe("next line");
  });

  it("uses a four second duration for the final line", () => {
    const frame = buildDesktopLyricsFrame(synchronizedLyrics(), clock({ positionSeconds: 6 }), 0, 0);

    expect(frame.current?.line.text).toBe("last line");
    expect(frame.current?.started).toBe(true);
    expect(frame.current?.progress).toBeCloseTo(0.5);
  });

  it("keeps the final line visible but clears its progress after four seconds", () => {
    const frame = buildDesktopLyricsFrame(synchronizedLyrics(), clock({ positionSeconds: 8 }), 0, 0);

    expect(frame.current?.line.text).toBe("last line");
    expect(frame.current?.started).toBe(false);
    expect(frame.current?.progress).toBe(0);
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
    version: 2,
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
    lines: [
      { time: 0, text: "hello world" },
      { time: 2, text: "next line" },
      { time: 4, text: "last line" },
    ],
  };
}
