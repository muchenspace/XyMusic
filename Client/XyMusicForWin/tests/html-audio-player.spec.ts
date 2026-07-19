import { describe, expect, it } from "vitest";
import { normalizedAudioVolume } from "../src/infrastructure/audio/HtmlAudioPlayer";

describe("HTML audio volume normalization", () => {
  it.each([
    [-0.000288, 0],
    [0, 0],
    [0.72, 0.72],
    [1, 1],
    [1.000001, 1],
    [Number.NaN, 0],
    [Number.POSITIVE_INFINITY, 0],
  ])("normalizes %s to %s", (input, expected) => {
    expect(normalizedAudioVolume(input)).toBe(expected);
  });
});
