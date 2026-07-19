import type { LyricLine, Lyrics } from "../domain/music";
import type { DesktopLyricsClockPayload } from "./protocol";

export interface DesktopLyricLineFrame {
  index: number;
  line: LyricLine;
  progress: number;
  started: boolean;
}

export interface DesktopLyricsFrame {
  playbackSeconds: number;
  activeIndex: number;
  current: DesktopLyricLineFrame | null;
  next: DesktopLyricLineFrame | null;
}

const DEFAULT_LINE_DURATION_SECONDS = 4;

export function estimatePlaybackSeconds(clock: DesktopLyricsClockPayload, nowMs = Date.now()): number {
  const position = finiteNonNegative(clock.positionSeconds);
  if (!clock.isPlaying) return position;
  const elapsed = Math.max(0, finiteNumber(nowMs) - finiteNumber(clock.anchoredAtMs)) / 1_000;
  return position + elapsed;
}

export function findActiveLyricIndex(lines: readonly LyricLine[], playbackSeconds: number): number {
  const playback = finiteNumber(playbackSeconds);
  let result = -1;
  for (let index = 0; index < lines.length; index += 1) {
    const time = lines[index]?.time;
    if (time === null || !Number.isFinite(time)) continue;
    if (time > playback) break;
    result = index;
  }
  return result;
}

export function progressBetween(playbackSeconds: number, startSeconds: number, endSeconds: number): number {
  const start = finiteNumber(startSeconds);
  const end = Math.max(start, finiteNumber(endSeconds));
  if (end <= start) return playbackSeconds >= end ? 1 : 0;
  return clamp01((finiteNumber(playbackSeconds) - start) / (end - start));
}

export function buildDesktopLyricsFrame(
  lyrics: Lyrics | null,
  clock: DesktopLyricsClockPayload,
  offsetSeconds: number,
  nowMs = Date.now(),
): DesktopLyricsFrame {
  const playbackSeconds = estimatePlaybackSeconds(clock, nowMs) + finiteNumber(offsetSeconds);
  if (!lyrics?.lines.length || !isMatchingTrack(lyrics, clock)) {
    return { playbackSeconds, activeIndex: -1, current: null, next: null };
  }

  const activeIndex = lyrics.synchronized ? findActiveLyricIndex(lyrics.lines, playbackSeconds) : -1;
  const currentIndex = activeIndex >= 0 ? activeIndex : 0;
  const nextIndex = currentIndex + 1;
  return {
    playbackSeconds,
    activeIndex,
    current: lineFrame(lyrics.lines, currentIndex, playbackSeconds, activeIndex >= 0),
    next: lineFrame(lyrics.lines, nextIndex, playbackSeconds, false),
  };
}

function lineFrame(
  lines: readonly LyricLine[],
  index: number,
  playbackSeconds: number,
  started: boolean,
): DesktopLyricLineFrame | null {
  const line = lines[index];
  if (!line) return null;
  const start = line.time;
  const nextLineTime = nextTimedLineTime(lines, index + 1);
  const lineEnd = start === null
    ? 0
    : nextLineTime ?? start + DEFAULT_LINE_DURATION_SECONDS;
  const isActive = started
    && start !== null
    && playbackSeconds >= start
    && playbackSeconds < lineEnd;
  const progress = isActive
    ? progressBetween(playbackSeconds, start, lineEnd)
    : 0;
  return { index, line, progress, started: isActive };
}

function isMatchingTrack(lyrics: Lyrics, clock: DesktopLyricsClockPayload): boolean {
  return clock.trackId !== null && lyrics.trackId === clock.trackId;
}

function nextTimedLineTime(lines: readonly LyricLine[], fromIndex: number): number | null {
  for (let index = fromIndex; index < lines.length; index += 1) {
    const time = lines[index]?.time;
    if (time !== null && Number.isFinite(time)) return time;
  }
  return null;
}

function finiteNonNegative(value: number): number {
  return Math.max(0, finiteNumber(value));
}

function finiteNumber(value: number): number {
  return Number.isFinite(value) ? value : 0;
}

function clamp01(value: number): number {
  return Math.max(0, Math.min(1, value));
}
