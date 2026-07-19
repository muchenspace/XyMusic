import { describe, expect, it } from "vitest";
import { normalizeResumePosition } from "../src/domain/playbackState";

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
