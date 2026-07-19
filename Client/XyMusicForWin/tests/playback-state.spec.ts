import { describe, expect, it } from "vitest";
import {
  cyclePlayMode,
  derivePlayMode,
  normalizeResumePosition,
  PLAY_MODE_ORDER,
  type PlayMode,
  type RepeatMode,
  splitPlayMode,
} from "../src/domain/playbackState";

describe("playback resume position", () => {
  it("clamps invalid positions and restarts tracks saved at the end", () => {
    expect(normalizeResumePosition(Number.NaN, 240)).toBe(0);
    expect(normalizeResumePosition(-12, 240)).toBe(0);
    expect(normalizeResumePosition(999, 240)).toBe(0);
    expect(normalizeResumePosition(239, 240)).toBe(0);
  });

  it("preserves useful progress and supports unknown durations", () => {
    expect(normalizeResumePosition(118.5, 240)).toBe(118.5);
    expect(normalizeResumePosition(42, 0)).toBe(42);
    expect(normalizeResumePosition(238.5, 240, 1)).toBe(238.5);
  });
});

describe("play mode derivation", () => {
  const cases: Array<{ repeatMode: RepeatMode; shuffled: boolean; expected: PlayMode }> = [
    { repeatMode: "off", shuffled: false, expected: "repeat-all" },
    { repeatMode: "all", shuffled: false, expected: "repeat-all" },
    { repeatMode: "one", shuffled: false, expected: "repeat-one" },
    { repeatMode: "off", shuffled: true, expected: "shuffle" },
    // shuffled 优先级高于 repeatMode，避免歧义状态
    { repeatMode: "one", shuffled: true, expected: "shuffle" },
    { repeatMode: "all", shuffled: true, expected: "shuffle" },
  ];

  for (const { repeatMode, shuffled, expected } of cases) {
    it(`derives ${expected} from repeat=${repeatMode}, shuffled=${shuffled}`, () => {
      expect(derivePlayMode(repeatMode, shuffled)).toBe(expected);
    });
  }
});

describe("play mode split", () => {
  it("splits each mode into mutually exclusive repeatMode and shuffled", () => {
    expect(splitPlayMode("repeat-all")).toEqual({ repeatMode: "all", shuffled: false });
    expect(splitPlayMode("repeat-one")).toEqual({ repeatMode: "one", shuffled: false });
    expect(splitPlayMode("shuffle")).toEqual({ repeatMode: "off", shuffled: true });
  });

  it("derives the same mode after splitting (round-trip)", () => {
    for (const mode of PLAY_MODE_ORDER) {
      const { repeatMode, shuffled } = splitPlayMode(mode);
      expect(derivePlayMode(repeatMode, shuffled)).toBe(mode);
    }
  });
});

describe("play mode cycle", () => {
  it("cycles through all modes in order and wraps around", () => {
    expect(cyclePlayMode("repeat-all")).toBe("repeat-one");
    expect(cyclePlayMode("repeat-one")).toBe("shuffle");
    expect(cyclePlayMode("shuffle")).toBe("repeat-all");
  });
});

