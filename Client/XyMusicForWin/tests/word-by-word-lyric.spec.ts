import { describe, expect, it } from "vitest";
import {
  localWordHighlightFragment,
  validateTimedWordText,
  wordHighlightRects,
} from "../src/shared/lyrics/wordByWordLyric";

describe("word-by-word lyric layout", () => {
  it("accepts only contiguous timed words that cover the complete line", () => {
    const valid = validateTimedWordText("A e\u0301!", [
      { text: "A " },
      { text: "e\u0301" },
      { text: "!" },
    ]);
    expect(valid?.words.map((word) => [word.startOffset, word.endOffset])).toEqual([
      [0, 2],
      [2, 4],
      [4, 5],
    ]);
    expect(validateTimedWordText("A e\u0301!", [{ text: "A e" }, { text: "\u0301!" }])).toBeNull();
    expect(validateTimedWordText("A e\u0301!", [{ text: "A " }])).toBeNull();
    expect(validateTimedWordText("👨‍👩‍👧", [{ text: "👨" }, { text: "‍👩‍👧" }])).toBeNull();
  });

  it("reveals by measured advance and handles RTL fragments from the right", () => {
    const fragments = [
      { wordIndex: 0, x: 0, y: 0, width: 10, height: 20, rtl: false },
      { wordIndex: 0, x: 10, y: 0, width: 30, height: 20, rtl: false },
      { wordIndex: 1, x: 50, y: 0, width: 20, height: 20, rtl: true },
    ] as const;
    expect(wordHighlightRects(fragments, [0.5, 0.25])).toEqual([
      { x: 0, y: 0, width: 10, height: 20 },
      { x: 10, y: 0, width: 10, height: 20 },
      { x: 65, y: 0, width: 5, height: 20 },
    ]);
  });

  it("converts transformed client rectangles back to local SVG coordinates", () => {
    expect(localWordHighlightFragment(
      2,
      { left: 124, top: 62, width: 36, height: 24 },
      { left: 100, top: 50, width: 240, height: 120 },
      200,
      100,
      true,
    )).toEqual({
      wordIndex: 2,
      x: 20,
      y: 10,
      width: 30,
      height: 20,
      rtl: true,
    });
  });
});
