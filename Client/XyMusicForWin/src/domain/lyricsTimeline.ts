import type { LyricWord, Lyrics } from "./music";

export interface LyricPlaybackPosition {
  lineIndex: number;
  wordIndex: number;
}

export interface LyricPlaybackClock {
  positionSeconds: number;
  anchoredAtMs: number;
  isPlaying: boolean;
}

export interface LyricPlaybackRenderPlan {
  /** A word fill is in progress and benefits from display-synchronized updates. */
  requiresAnimationFrame: boolean;
  /** The next timestamp at which the displayed lyric state can change. */
  nextChangeAtSeconds: number | null;
}

export interface LyricGraphemeRange {
  startOffset: number;
  endOffset: number;
}

/**
 * Returns the extended grapheme-like ranges used by Android's word reveal.
 * Offsets are UTF-16 indices, matching DOM Range and Compose text layouts.
 */
export function lyricGraphemeRanges(value: string): readonly LyricGraphemeRange[] {
  const ranges: LyricGraphemeRange[] = [];
  let startOffset = 0;
  while (startOffset < value.length) {
    const endOffset = nextLyricGraphemeEnd(value, startOffset, value.length);
    if (endOffset <= startOffset) break;
    ranges.push({ startOffset, endOffset });
    startOffset = endOffset;
  }
  return ranges;
}

/**
 * Resolves the end of a timed word using the same fallback as the Android
 * player. Explicit ends remain authoritative; otherwise an adjacent word
 * starts the boundary, and an unterminated final word gets a short grapheme
 * based reveal window.
 */
export function resolveLyricWordEnd(
  word: LyricWord,
  nextWordStartSeconds?: number,
  nextLineStartSeconds?: number | null,
): number {
  if (word.endTime !== undefined) return word.endTime;
  if (Number.isFinite(nextWordStartSeconds)) {
    return Math.max(finiteNumber(word.time), nextWordStartSeconds!);
  }

  const graphemeCount = countLyricGraphemes(word.text);
  const duration = Math.min(
    UNTERMINATED_WORD_MAX_SECONDS,
    Math.max(UNTERMINATED_WORD_MIN_SECONDS, Math.max(1, graphemeCount) * UNTERMINATED_WORD_SECONDS_PER_GRAPHEME),
  );
  const start = finiteNumber(word.time);
  const inferredEnd = Math.round((start + duration) * 1_000) / 1_000;
  return Number.isFinite(nextLineStartSeconds)
    ? Math.min(inferredEnd, Math.max(start, nextLineStartSeconds!))
    : inferredEnd;
}

/** Counts the extended grapheme-like units used by Android's lyric reveal. */
export function countLyricGraphemes(value: string): number {
  return lyricGraphemeRanges(value).length;
}

function nextLyricGraphemeEnd(value: string, start: number, rangeEnd: number): number {
  const first = value.codePointAt(start) ?? 0;
  let end = Math.min(rangeEnd, start + codePointLength(first));
  if (isRegionalIndicator(first) && end < rangeEnd) {
    const next = value.codePointAt(end) ?? 0;
    if (isRegionalIndicator(next)) end = Math.min(rangeEnd, end + codePointLength(next));
  }

  while (end < rangeEnd) {
    const codePoint = value.codePointAt(end) ?? 0;
    if (isGraphemeContinuation(codePoint)) {
      end = Math.min(rangeEnd, end + codePointLength(codePoint));
    } else if (codePoint === ZERO_WIDTH_JOINER) {
      const afterJoiner = end + codePointLength(codePoint);
      if (afterJoiner >= rangeEnd) {
        end = Math.min(rangeEnd, afterJoiner);
      } else {
        const joined = value.codePointAt(afterJoiner) ?? 0;
        end = Math.min(rangeEnd, afterJoiner + codePointLength(joined));
      }
    } else {
      break;
    }
  }
  return end;
}

function codePointLength(codePoint: number): number {
  return codePoint > 0xffff ? 2 : 1;
}

function isRegionalIndicator(codePoint: number): boolean {
  return codePoint >= REGIONAL_INDICATOR_START && codePoint <= REGIONAL_INDICATOR_END;
}

function isGraphemeContinuation(codePoint: number): boolean {
  const value = String.fromCodePoint(codePoint);
  return MARK_REGEX.test(value)
    || (codePoint >= VARIATION_SELECTOR_START && codePoint <= VARIATION_SELECTOR_END)
    || (codePoint >= SUPPLEMENTARY_VARIATION_SELECTOR_START && codePoint <= SUPPLEMENTARY_VARIATION_SELECTOR_END)
    || (codePoint >= EMOJI_MODIFIER_START && codePoint <= EMOJI_MODIFIER_END)
    || (codePoint >= EMOJI_TAG_START && codePoint <= EMOJI_TAG_END);
}

export function shouldReanchorLyricPlaybackClock(
  current: LyricPlaybackClock,
  next: LyricPlaybackClock,
  nowMs = Date.now(),
): boolean {
  const expectedPosition = interpolateLyricPlaybackSeconds(current, nowMs);
  return (
    !next.isPlaying ||
    current.isPlaying !== next.isPlaying ||
    Math.abs(next.positionSeconds - expectedPosition) > LYRIC_PLAYBACK_POSITION_SNAP_THRESHOLD_SECONDS
  );
}

export function interpolateLyricPlaybackSeconds(
  clock: LyricPlaybackClock,
  nowMs = Date.now(),
): number {
  const position = Math.max(0, finiteNumber(clock.positionSeconds));
  if (!clock.isPlaying) return position;
  const elapsed = Math.max(0, finiteNumber(nowMs) - finiteNumber(clock.anchoredAtMs)) / 1_000;
  return position + elapsed;
}

export function resolveLyricWordProgress(
  word: LyricWord,
  playbackSeconds: number,
  nextWordStartSeconds?: number,
  nextLineStartSeconds?: number | null,
): number {
  const playback = finiteNumber(playbackSeconds);
  const start = finiteNumber(word.time);
  const end = resolveLyricWordEnd(word, nextWordStartSeconds, nextLineStartSeconds);
  if (playback <= start || !Number.isFinite(end) || end <= start) return 0;
  const progress = Math.min(1, Math.max(0, (playback - start) / (end - start)));
  return Math.round(progress * 1_000_000) / 1_000_000;
}

export function resolveLyricPlaybackPosition(
  lyrics: Lyrics | null,
  playbackSeconds: number,
): LyricPlaybackPosition {
  if (!lyrics?.synchronized || !lyrics.lines.length) return EMPTY_POSITION;
  const playback = finiteNumber(playbackSeconds);
  const lineIndex = findActiveLineIndex(lyrics, playback);
  if (lineIndex < 0) return EMPTY_POSITION;
  const line = lyrics.lines[lineIndex]!;
  return {
    lineIndex,
    wordIndex: lyrics.timing === "WORD"
      ? findActiveWordIndex(line.words, playback, lyrics.lines[lineIndex + 1]?.time)
      : -1,
  };
}

/**
 * Describes the next visible lyric transition without coupling the domain model
 * to a specific rendering framework. Line-only lyrics can sleep until their
 * next timestamp; enhanced word lyrics request animation frames only while a
 * word's fill is actively progressing.
 */
export function resolveLyricPlaybackRenderPlan(
  lyrics: Lyrics | null,
  playbackSeconds: number,
): LyricPlaybackRenderPlan {
  if (!lyrics?.synchronized || !lyrics.lines.length) return IDLE_RENDER_PLAN;

  const playback = finiteNumber(playbackSeconds);
  const position = resolveLyricPlaybackPosition(lyrics, playback);
  const activeLine = position.lineIndex >= 0 ? lyrics.lines[position.lineIndex] : undefined;
  const activeWord = lyrics.timing === "WORD" && position.wordIndex >= 0
    ? activeLine?.words?.[position.wordIndex]
    : undefined;
  const activeWordNextStart = activeLine?.words?.[position.wordIndex + 1]?.time;
  const nextLineStart = lyrics.lines[position.lineIndex + 1]?.time;

  if (activeWord && hasActiveWordFill(activeWord, playback, activeWordNextStart, nextLineStart)) {
    return {
      requiresAnimationFrame: true,
      nextChangeAtSeconds: resolveLyricWordEnd(activeWord, activeWordNextStart, nextLineStart),
    };
  }

  return {
    requiresAnimationFrame: false,
    nextChangeAtSeconds: findNextLyricVisualChange(lyrics, playback, position.lineIndex),
  };
}

function findActiveLineIndex(lyrics: Lyrics, playback: number): number {
  let low = 0;
  let high = lyrics.lines.length - 1;
  let result = -1;
  while (low <= high) {
    const middle = (low + high) >>> 1;
    const time = lyrics.lines[middle]?.time;
    if (time !== null && time !== undefined && time <= playback) {
      result = middle;
      low = middle + 1;
    } else {
      high = middle - 1;
    }
  }
  return result;
}

function findActiveWordIndex(
  words: LyricWord[] | undefined,
  playback: number,
  nextLineStart?: number | null,
): number {
  if (!words?.length) return -1;
  for (let index = 0; index < words.length; index += 1) {
    const word = words[index];
    if (!word || !Number.isFinite(word.time) || playback < word.time) return -1;
    const end = resolveLyricWordEnd(word, words[index + 1]?.time, nextLineStart);
    if (!Number.isFinite(end) || playback < end) return index;
  }
  return -1;
}

function hasActiveWordFill(
  word: LyricWord,
  playback: number,
  nextWordStartSeconds?: number,
  nextLineStartSeconds?: number | null,
): boolean {
  const end = resolveLyricWordEnd(word, nextWordStartSeconds, nextLineStartSeconds);
  return Number.isFinite(word.time)
    && Number.isFinite(end)
    && end > word.time
    && playback >= word.time
    && playback < end;
}

function findNextLyricVisualChange(lyrics: Lyrics, playback: number, activeLineIndex: number): number | null {
  let nextChange = findNextLineTime(lyrics.lines, playback);
  if (lyrics.timing !== "WORD" || activeLineIndex < 0) return nextChange;

  // Only the active line's words are rendered dynamically. Boundaries from a
  // later line cannot affect the display until that line becomes active.
  const words = lyrics.lines[activeLineIndex]?.words;
  if (!words?.length) return nextChange;
  const nextLineStart = lyrics.lines[activeLineIndex + 1]?.time;
  for (let index = 0; index < words.length; index += 1) {
    const word = words[index]!;
    const end = resolveLyricWordEnd(word, words[index + 1]?.time, nextLineStart);
    if (Number.isFinite(word.time) && word.time > playback) {
      nextChange = earlierTimestamp(nextChange, word.time);
    }
    if (Number.isFinite(end) && end > playback) {
      nextChange = earlierTimestamp(nextChange, end);
    }
  }
  return nextChange;
}

function findNextLineTime(lines: readonly Lyrics["lines"][number][], playback: number): number | null {
  let low = 0;
  let high = lines.length - 1;
  let result: number | null = null;
  while (low <= high) {
    const middle = (low + high) >>> 1;
    const time = lines[middle]?.time;
    if (time !== null && time !== undefined && time > playback) {
      result = time;
      high = middle - 1;
    } else {
      low = middle + 1;
    }
  }
  return result;
}

function earlierTimestamp(current: number | null, candidate: number): number {
  return current === null || candidate < current ? candidate : current;
}

function finiteNumber(value: number): number {
  return Number.isFinite(value) ? value : 0;
}

const EMPTY_POSITION: LyricPlaybackPosition = { lineIndex: -1, wordIndex: -1 };
const IDLE_RENDER_PLAN: LyricPlaybackRenderPlan = {
  requiresAnimationFrame: false,
  nextChangeAtSeconds: null,
};
export const LYRIC_PLAYBACK_POSITION_CORRECTION_MS = 120;
export const LYRIC_PLAYBACK_POSITION_CORRECTION_EPSILON_SECONDS = 0.0005;
export const LYRIC_PLAYBACK_POSITION_SNAP_THRESHOLD_SECONDS = 0.25;
const UNTERMINATED_WORD_SECONDS_PER_GRAPHEME = 0.09;
const UNTERMINATED_WORD_MIN_SECONDS = 0.24;
const UNTERMINATED_WORD_MAX_SECONDS = 0.9;
const ZERO_WIDTH_JOINER = 0x200d;
const VARIATION_SELECTOR_START = 0xfe00;
const VARIATION_SELECTOR_END = 0xfe0f;
const SUPPLEMENTARY_VARIATION_SELECTOR_START = 0xe0100;
const SUPPLEMENTARY_VARIATION_SELECTOR_END = 0xe01ef;
const EMOJI_MODIFIER_START = 0x1f3fb;
const EMOJI_MODIFIER_END = 0x1f3ff;
const EMOJI_TAG_START = 0xe0020;
const EMOJI_TAG_END = 0xe007f;
const REGIONAL_INDICATOR_START = 0x1f1e6;
const REGIONAL_INDICATOR_END = 0x1f1ff;
const MARK_REGEX = /^\p{M}$/u;
