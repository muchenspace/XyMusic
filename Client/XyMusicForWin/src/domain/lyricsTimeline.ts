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

export function shouldReanchorLyricPlaybackClock(
  current: LyricPlaybackClock,
  next: LyricPlaybackClock,
  nowMs = Date.now(),
): boolean {
  const expectedPosition = interpolateLyricPlaybackSeconds(current, nowMs);
  return (
    !next.isPlaying ||
    current.isPlaying !== next.isPlaying ||
    Math.abs(next.positionSeconds - expectedPosition) > PLAYBACK_JUMP_THRESHOLD_SECONDS
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

export function resolveLyricWordProgress(word: LyricWord, playbackSeconds: number): number {
  const playback = finiteNumber(playbackSeconds);
  const start = finiteNumber(word.time);
  const end = word.endTime;
  if (playback <= start || end === undefined || !Number.isFinite(end) || end <= start) return 0;
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
    wordIndex: lyrics.timing === "WORD" ? findActiveWordIndex(line.words, playback) : -1,
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

function findActiveWordIndex(words: LyricWord[] | undefined, playback: number): number {
	if (!words?.length) return -1;
	for (let index = 0; index < words.length; index += 1) {
		const word = words[index];
		if (!word || !Number.isFinite(word.time) || playback < word.time) return -1;
		if (word.endTime === undefined || !Number.isFinite(word.endTime) || playback < word.endTime) return index;
	}
	return -1;
}

function finiteNumber(value: number): number {
  return Number.isFinite(value) ? value : 0;
}

const EMPTY_POSITION: LyricPlaybackPosition = { lineIndex: -1, wordIndex: -1 };
const PLAYBACK_JUMP_THRESHOLD_SECONDS = 0.08;
