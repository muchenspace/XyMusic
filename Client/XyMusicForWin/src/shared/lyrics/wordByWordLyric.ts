import type { LyricWord } from "../../domain/music";
import { lyricGraphemeRanges, type LyricGraphemeRange } from "../../domain/lyricsTimeline";

export interface TimedWordTextRange {
  wordIndex: number;
  startOffset: number;
  endOffset: number;
  graphemes: readonly LyricGraphemeRange[];
}

export interface ValidatedTimedWordText {
  text: string;
  words: readonly TimedWordTextRange[];
}

export interface WordHighlightFragment {
  wordIndex: number;
  x: number;
  y: number;
  width: number;
  height: number;
  rtl: boolean;
}

export interface WordHighlightRect {
  x: number;
  y: number;
  width: number;
  height: number;
}

export interface LyricClientRect {
  left: number;
  top: number;
  width: number;
  height: number;
}

/**
 * Creates UTF-16 ranges for a line only when the timestamps describe its
 * visible text exactly. DOM Range also uses UTF-16 offsets, so the result can
 * be passed to layout measurement without splitting combining characters.
 */
export function validateTimedWordText(
  text: string,
  words: readonly Pick<LyricWord, "text">[] | undefined,
): ValidatedTimedWordText | null {
  if (!words?.length || words.some((word) => typeof word.text !== "string")) return null;
  if (words.map((word) => word.text).join("") !== text) return null;

  // A timed-word boundary is only safe when it lands on an extended grapheme
  // boundary in the complete line. This rejects an otherwise matching split
  // such as `e` + combining acute, which would make the overlay drift from the
  // glyph layout.
  const validBoundaries = new Set<number>([0, text.length]);
  lyricGraphemeRanges(text).forEach((range) => validBoundaries.add(range.endOffset));

  let offset = 0;
  const ranges: TimedWordTextRange[] = [];
  for (const [wordIndex, word] of words.entries()) {
    const startOffset = offset;
    offset += word.text.length;
    if (!validBoundaries.has(startOffset) || !validBoundaries.has(offset)) return null;
    ranges.push({
      wordIndex,
      startOffset,
      endOffset: offset,
      graphemes: lyricGraphemeRanges(text.slice(startOffset, offset)).map((range) => ({
        startOffset: startOffset + range.startOffset,
        endOffset: startOffset + range.endOffset,
      })),
    });
  }
  return { text, words: ranges };
}

/** Converts timed-word progress into layout-space clip rectangles by advance. */
export function wordHighlightRects(
  fragments: readonly WordHighlightFragment[],
  progresses: readonly number[],
): readonly WordHighlightRect[] {
  const rects: WordHighlightRect[] = [];
  const fragmentsByWord = new Map<number, WordHighlightFragment[]>();
  fragments.forEach((fragment) => {
    const wordFragments = fragmentsByWord.get(fragment.wordIndex) ?? [];
    wordFragments.push(fragment);
    fragmentsByWord.set(fragment.wordIndex, wordFragments);
  });

  fragmentsByWord.forEach((wordFragments, wordIndex) => {
    const totalAdvance = wordFragments.reduce((total, fragment) => total + fragment.width, 0);
    let remainingAdvance = totalAdvance * clampedProgress(progresses[wordIndex]);
    for (const fragment of wordFragments) {
      if (remainingAdvance <= 0 || fragment.width <= 0 || fragment.height <= 0) break;
      const width = Math.min(fragment.width, remainingAdvance);
      rects.push({
        x: fragment.rtl ? fragment.x + fragment.width - width : fragment.x,
        y: fragment.y,
        width,
        height: fragment.height,
      });
      remainingAdvance -= fragment.width;
    }
  });
  return rects;
}

/** Restores DOM Range screen coordinates to the lyric element's pre-transform layout space. */
export function localWordHighlightFragment(
  wordIndex: number,
  fragmentRect: LyricClientRect,
  rootRect: LyricClientRect,
  layoutWidth: number,
  layoutHeight: number,
  rtl: boolean,
): WordHighlightFragment {
  const scaleX = layoutScale(rootRect.width, layoutWidth);
  const scaleY = layoutScale(rootRect.height, layoutHeight);
  return {
    wordIndex,
    x: (fragmentRect.left - rootRect.left) / scaleX,
    y: (fragmentRect.top - rootRect.top) / scaleY,
    width: fragmentRect.width / scaleX,
    height: fragmentRect.height / scaleY,
    rtl,
  };
}

function clampedProgress(value: number | undefined): number {
  return Number.isFinite(value) ? Math.max(0, Math.min(1, value!)) : 0;
}

function layoutScale(renderedSize: number, layoutSize: number): number {
  const scale = renderedSize / layoutSize;
  return Number.isFinite(scale) && scale > 0 ? scale : 1;
}
