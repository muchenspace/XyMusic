import { describe, expect, it } from "vitest";
import type { Lyrics } from "../src/domain/music";
import { shouldReanchorLyricPlaybackClock } from "../src/domain/lyricsTimeline";
import {
  buildDesktopLyricsFrame,
  createDesktopLyricsTransition,
  desktopLyricsLineShiftPx,
  desktopLyricsTransitionDurationMs,
  desktopLyricsTransitionWeight,
  estimatePlaybackSeconds,
  fastOutSlowIn,
  resolveDesktopLyricsTransitionMode,
  retargetDesktopLyricsTransition,
  sampleDesktopLyricsTransition,
} from "../src/desktop-lyrics/timeline";
import type { DesktopLyricsClockPayload } from "../src/desktop-lyrics/protocol";

describe("desktop lyrics timeline", () => {
  it("uses Android-aligned snap and dense playback transition rules", () => {
    expect(resolveDesktopLyricsTransitionMode(null, 1, null, 1)).toBe("SNAP");
    expect(resolveDesktopLyricsTransitionMode(4, 3, 4, 3)).toBe("SNAP");
    expect(resolveDesktopLyricsTransitionMode(1, 4, 1, 3)).toBe("SNAP");
    expect(resolveDesktopLyricsTransitionMode(1, 2, 1, 2)).toBe("ANIMATE");
    expect(resolveDesktopLyricsTransitionMode(1, 4, 1, 1.45)).toBe("ANIMATE");
  });

  it("bounds line transition duration between 300 and 520 milliseconds", () => {
    expect(desktopLyricsTransitionDurationMs(0)).toBe(300);
    expect(desktopLyricsTransitionDurationMs(1)).toBe(302);
    expect(desktopLyricsTransitionDurationMs(2)).toBe(520);
    expect(desktopLyricsTransitionDurationMs(0, 74)).toBe(400);
  });

  it("uses a measured line distance without changing the default transition geometry", () => {
    expect(desktopLyricsLineShiftPx(3, 2.5)).toBe(28);
    expect(desktopLyricsLineShiftPx(3, 2.5, 0, 104)).toBe(52);
    expect(desktopLyricsLineShiftPx(4, 2.5, 1, 104)).toBe(52);
  });

  it("samples source and target emphasis from one smooth frame clock", () => {
    const transition = createDesktopLyricsTransition(2, 3, 1_000, 400);

    const quarter = sampleDesktopLyricsTransition(transition, 1_100);
    const expectedProgress = fastOutSlowIn(0.25);
    expect(quarter.progress).toBeCloseTo(expectedProgress, 8);
    expect(desktopLyricsTransitionWeight(quarter, 2)).toBeCloseTo(1 - expectedProgress, 8);
    expect(desktopLyricsTransitionWeight(quarter, 3)).toBeCloseTo(expectedProgress, 8);

    const complete = sampleDesktopLyricsTransition(transition, 1_400);
    expect(complete.done).toBe(true);
    expect(complete.weights).toEqual([{ index: 3, value: 1 }]);
  });

  it("preserves visible weights when a dense transition is interrupted", () => {
    const first = createDesktopLyricsTransition(1, 2, 0, 400);
    const beforeRetarget = sampleDesktopLyricsTransition(first, 200);
    const interrupted = retargetDesktopLyricsTransition(first, 3, 200, 400);
    const atRetarget = sampleDesktopLyricsTransition(interrupted, 200);

    expect(interrupted.sourceIndex).toBe(2);
    expect(interrupted.initialVelocityPerMs).toBeCloseTo(beforeRetarget.velocityPerMs, 10);
    expect(interrupted.startLinePosition).toBeCloseTo(beforeRetarget.linePosition, 10);
    expect(atRetarget.linePosition).toBeCloseTo(beforeRetarget.linePosition, 10);
    expect(desktopLyricsLineShiftPx(2, atRetarget.linePosition))
      .toBeCloseTo(desktopLyricsLineShiftPx(2, beforeRetarget.linePosition), 10);
    expect(atRetarget.velocityPerMs).toBeCloseTo(beforeRetarget.velocityPerMs, 10);
    expect(desktopLyricsTransitionWeight(atRetarget, 1))
      .toBeCloseTo(desktopLyricsTransitionWeight(beforeRetarget, 1), 8);
    expect(desktopLyricsTransitionWeight(atRetarget, 2))
      .toBeCloseTo(desktopLyricsTransitionWeight(beforeRetarget, 2), 8);
    expect(desktopLyricsTransitionWeight(atRetarget, 3)).toBe(0);

    const halfway = sampleDesktopLyricsTransition(interrupted, 400);
    const remaining = 1 - halfway.progress;
    expect(desktopLyricsTransitionWeight(halfway, 1))
      .toBeCloseTo(desktopLyricsTransitionWeight(beforeRetarget, 1) * remaining, 8);
    expect(desktopLyricsTransitionWeight(halfway, 2))
      .toBeCloseTo(desktopLyricsTransitionWeight(beforeRetarget, 2) * remaining, 8);
    expect(desktopLyricsTransitionWeight(halfway, 3)).toBeCloseTo(halfway.progress, 8);

    const afterTweenDuration = sampleDesktopLyricsTransition(interrupted, 600);
    expect(afterTweenDuration.done).toBe(false);
    const settled = sampleDesktopLyricsTransition(interrupted, 1_200);
    expect(settled.done).toBe(true);
    expect(settled.velocityPerMs).toBe(0);
    expect(settled.weights).toEqual([{ index: 3, value: 1 }]);
  });

  it("keeps dense multi-line distance and retarget position on one spatial clock", () => {
    const dense = createDesktopLyricsTransition(1, 4, 0);
    expect(dense.durationMs).toBe(520);
    const beforeRetarget = sampleDesktopLyricsTransition(dense, 260);
    expect(beforeRetarget.linePosition).toBeCloseTo(
      1 + (4 - 1) * beforeRetarget.progress,
      10,
    );

    const retargeted = retargetDesktopLyricsTransition(dense, 5, 260);
    const atRetarget = sampleDesktopLyricsTransition(retargeted, 260);
    expect(atRetarget.linePosition).toBeCloseTo(beforeRetarget.linePosition, 10);
    expect(desktopLyricsLineShiftPx(4, atRetarget.linePosition))
      .toBeCloseTo(desktopLyricsLineShiftPx(4, beforeRetarget.linePosition), 10);
    expect(retargeted.durationMs).toBe(
      desktopLyricsTransitionDurationMs(5 - beforeRetarget.linePosition),
    );
  });

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

  it("alternates active slot between top and bottom lines in a ping-pong pattern", () => {
    const lyrics = synchronizedLyrics();

    // Line 0 (even index): Top is active, Bottom is next line
    const frame0 = buildDesktopLyricsFrame(lyrics, clock({ positionSeconds: 0.5 }), 0, 0);
    expect(frame0.activeIndex).toBe(0);
    expect(frame0.activeSlot).toBe("top");
    expect(frame0.top?.line.text).toBe("hello world");
    expect(frame0.top?.started).toBe(true);
    expect(frame0.bottom?.line.text).toBe("next line");
    expect(frame0.bottom?.started).toBe(false);

    // Line 1 (odd index): Bottom is active, Top switches to the line after next
    const frame1 = buildDesktopLyricsFrame(lyrics, clock({ positionSeconds: 2.5 }), 0, 0);
    expect(frame1.activeIndex).toBe(1);
    expect(frame1.activeSlot).toBe("bottom");
    expect(frame1.bottom?.line.text).toBe("next line");
    expect(frame1.bottom?.started).toBe(true);
    expect(frame1.top?.line.text).toBe("last line");
    expect(frame1.top?.started).toBe(false);

    // Line 2 (even index): Top is active, Bottom is null (end of lyrics)
    const frame2 = buildDesktopLyricsFrame(lyrics, clock({ positionSeconds: 4.5 }), 0, 0);
    expect(frame2.activeIndex).toBe(2);
    expect(frame2.activeSlot).toBe("top");
    expect(frame2.top?.line.text).toBe("last line");
    expect(frame2.top?.started).toBe(true);
    expect(frame2.bottom).toBeNull();
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
