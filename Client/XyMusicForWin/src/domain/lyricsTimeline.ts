import type { Lyrics } from "./music";

export interface LyricPlaybackPosition {
  lineIndex: number;
  lineProgress: number;
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
  const lineStart = line.time;
  if (lineStart === null) return EMPTY_POSITION;
  const nextLineTime = findNextTimedLine(lyrics, lineIndex + 1);
  const fallbackLineEnd = lineStart + DEFAULT_LINE_DURATION_SECONDS;
  const lineEnd = validEndTime(nextLineTime, lineStart) ?? fallbackLineEnd;
  if (playback >= lineEnd) return EMPTY_POSITION;
  return {
    lineIndex,
    lineProgress: progressBetween(playback, lineStart, lineEnd),
  };
}

function findActiveLineIndex(lyrics: Lyrics, playbackSeconds: number): number {
  let low = 0;
  let high = lyrics.lines.length - 1;
  let result = -1;
  while (low <= high) {
    const middle = (low + high) >>> 1;
    const time = lyrics.lines[middle]?.time;
    if (time !== null && time !== undefined && time <= playbackSeconds) {
      result = middle;
      low = middle + 1;
    } else {
      high = middle - 1;
    }
  }
  return result;
}

function findNextTimedLine(lyrics: Lyrics, fromIndex: number): number | undefined {
  for (let index = fromIndex; index < lyrics.lines.length; index += 1) {
    const time = lyrics.lines[index]?.time;
    if (time !== null && time !== undefined && Number.isFinite(time)) return time;
  }
  return undefined;
}

function validEndTime(value: number | undefined, start: number): number | undefined {
  return value !== undefined && Number.isFinite(value) && value > start ? value : undefined;
}

function progressBetween(value: number, start: number, end: number): number {
  if (!Number.isFinite(start) || !Number.isFinite(end) || end <= start) return value >= end ? 1 : 0;
  return Math.max(0, Math.min(1, (value - start) / (end - start)));
}

function finiteNumber(value: number): number {
  return Number.isFinite(value) ? value : 0;
}

const EMPTY_POSITION: LyricPlaybackPosition = { lineIndex: -1, lineProgress: 0 };
const DEFAULT_LINE_DURATION_SECONDS = 4;
