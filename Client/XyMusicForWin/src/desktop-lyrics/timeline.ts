import type { LyricLine, Lyrics } from "../domain/music";
import { interpolateLyricPlaybackSeconds, resolveLyricPlaybackPosition } from "../domain/lyricsTimeline";
import type { DesktopLyricsClockPayload } from "./protocol";

export interface DesktopLyricLineFrame {
  index: number;
  line: LyricLine;
  started: boolean;
  wordIndex: number;
}

export interface DesktopLyricsFrame {
  playbackSeconds: number;
  activeIndex: number;
  current: DesktopLyricLineFrame | null;
  next: DesktopLyricLineFrame | null;
}

export function estimatePlaybackSeconds(clock: DesktopLyricsClockPayload, nowMs = Date.now()): number {
  return interpolateLyricPlaybackSeconds(clock, nowMs);
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

  const position = lyrics.synchronized
    ? resolveLyricPlaybackPosition(lyrics, playbackSeconds)
    : { lineIndex: -1, wordIndex: -1 };
  const activeIndex = position.lineIndex;
  const currentIndex = activeIndex >= 0 ? activeIndex : 0;
  const nextIndex = currentIndex + 1;
  return {
    playbackSeconds,
    activeIndex,
    current: lineFrame(lyrics.lines, currentIndex, activeIndex === currentIndex, activeIndex === currentIndex ? position.wordIndex : -1),
    next: lineFrame(lyrics.lines, nextIndex, false, -1),
  };
}

function lineFrame(
  lines: readonly LyricLine[],
  index: number,
  started: boolean,
  wordIndex: number,
): DesktopLyricLineFrame | null {
  const line = lines[index];
  if (!line) return null;
  return { index, line, started, wordIndex };
}

function isMatchingTrack(lyrics: Lyrics, clock: DesktopLyricsClockPayload): boolean {
  return clock.trackId !== null && lyrics.trackId === clock.trackId;
}

function finiteNumber(value: number): number {
  return Number.isFinite(value) ? value : 0;
}
