import { describe, expect, it } from "vitest";
import {
  canonicalLyricTargetIndex,
  correctionDurationMs,
  lyricLineTransitionEmphasis,
  lyricLayoutDeltaHasSettled,
  lyricSeekBaselineIndex,
  lyricTransitionDurationMs,
  lyricTransitionIsSettled,
  lyricTransitionMode,
  retargetLyricEmphasis,
  settledLyricEmphasis,
  smoothLyricEmphasis,
} from "../src/presentation/components/lyricsTransition";

describe("Android-compatible lyric transitions", () => {
  it("snaps discontinuities and animates natural or dense advances", () => {
    expect(lyricTransitionMode(null, 0)).toBe("snap");
    expect(lyricTransitionMode(2, 1, 2, 1)).toBe("snap");
    expect(lyricTransitionMode(2, 3, 2, 3)).toBe("animate");
    expect(lyricTransitionMode(0, 3, 1, 1.45)).toBe("animate");
    expect(lyricTransitionMode(0, 3, 1, 1.451)).toBe("snap");
  });

  it("derives a bounded duration from the larger visual distance", () => {
    expect(lyricTransitionDurationMs(0, 0)).toBe(300);
    expect(lyricTransitionDurationMs(1, 0)).toBe(302);
    expect(lyricTransitionDurationMs(0, 74)).toBe(400);
    expect(lyricTransitionDurationMs(2, 0)).toBe(520);
    expect(lyricTransitionDurationMs(1, -200)).toBe(520);
  });

  it("bounds the post-layout correction to 90 through 180ms", () => {
    expect(correctionDurationMs(40)).toBe(90);
    expect(correctionDurationMs(150)).toBe(150);
    expect(correctionDurationMs(300)).toBe(180);
  });

  it("retargets from the currently visible source and target weights", () => {
    const first = retargetLyricEmphasis(settledLyricEmphasis(2), 0, 3);
    expect(lyricLineTransitionEmphasis(0.4, 2, first)).toBeCloseTo(0.6);
    expect(lyricLineTransitionEmphasis(0.4, 3, first)).toBeCloseTo(0.4);

    const interrupted = retargetLyricEmphasis(first, 0.4, 4);
    expect(lyricLineTransitionEmphasis(0.7, 2, interrupted)).toBeCloseTo(0.42);
    expect(lyricLineTransitionEmphasis(0.7, 3, interrupted)).toBeCloseTo(0.28);
    expect(lyricLineTransitionEmphasis(0.7, 4, interrupted)).toBeCloseTo(0.3);
    expect(lyricLineTransitionEmphasis(0.7, 1, interrupted)).toBe(0);
  });

  it("settles only when both line position and emphasis reach the target", () => {
    const transition = retargetLyricEmphasis(settledLyricEmphasis(1), 0, 2);
    expect(lyricTransitionIsSettled(2, 0.5, 2, transition)).toBe(false);
    expect(lyricTransitionIsSettled(1.5, 1, 2, transition)).toBe(false);
    expect(lyricTransitionIsSettled(2, 1, 2, transition)).toBe(true);
  });

  it("uses smoothstep only for the word-overlay visibility gate", () => {
    expect(smoothLyricEmphasis(0)).toBe(0);
    expect(smoothLyricEmphasis(0.25)).toBeCloseTo(0.15625);
    expect(smoothLyricEmphasis(1)).toBe(1);
  });

  it("waits for a seek target and canonicalizes duplicate timestamps", () => {
    const lines = [
      { time: 1, text: "first" },
      { time: 1, text: "duplicate" },
      { time: 2, text: "last" },
    ];
    expect(canonicalLyricTargetIndex(lines, 0)).toBe(1);
    expect(lyricSeekBaselineIndex(2, 0, -1)).toBeNull();
    expect(lyricSeekBaselineIndex(0, 8, 7)).toBeNull();
    expect(lyricSeekBaselineIndex(0, 8, 8)).toBe(8);
    expect(lyricSeekBaselineIndex(0, 8, 9)).toBe(8);
    expect(lyricSeekBaselineIndex(8, 2, 3)).toBeNull();
    expect(lyricSeekBaselineIndex(8, 2, 1)).toBe(2);
  });

  it("requires finite layout deltas within the Android stability epsilon", () => {
    expect(lyricLayoutDeltaHasSettled(null, null)).toBe(false);
    expect(lyricLayoutDeltaHasSettled(null, 0)).toBe(false);
    expect(lyricLayoutDeltaHasSettled(10, null)).toBe(false);
    expect(lyricLayoutDeltaHasSettled(Number.NaN, 10)).toBe(false);
    expect(lyricLayoutDeltaHasSettled(10, Number.POSITIVE_INFINITY)).toBe(false);
    expect(lyricLayoutDeltaHasSettled(10, 10.5)).toBe(true);
    expect(lyricLayoutDeltaHasSettled(10, 10.5001)).toBe(false);
  });
});
