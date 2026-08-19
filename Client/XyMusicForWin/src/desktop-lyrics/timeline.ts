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

/** The two ways a desktop lyric line can change, matching the Android player contract. */
export type DesktopLyricsTransitionMode = "SNAP" | "ANIMATE";

export interface DesktopLyricsEmphasisWeight {
  index: number;
  value: number;
}

/**
 * A line transition is deliberately independent from Vue.  Keeping the start weights lets an
 * interrupted dense transition be retargeted from the exact frame that was on screen instead of
 * flashing back to either the old or new line.
 */
export interface DesktopLyricsTransition {
  sourceIndex: number;
  targetIndex: number;
  startLinePosition: number;
  startedAtMs: number;
  durationMs: number;
  startWeights: readonly DesktopLyricsEmphasisWeight[];
  initialVelocityPerMs: number;
  preserveVelocity: boolean;
}

export interface DesktopLyricsTransitionSample {
  progress: number;
  linePosition: number;
  velocityPerMs: number;
  done: boolean;
  weights: readonly DesktopLyricsEmphasisWeight[];
}

export const DESKTOP_LYRICS_DENSE_INTERVAL_SECONDS = 0.45;
export const DESKTOP_LYRICS_TRANSITION_MIN_DURATION_MS = 300;
export const DESKTOP_LYRICS_TRANSITION_MAX_DURATION_MS = 520;
export const DESKTOP_LYRICS_TRANSITION_LINE_DISTANCE_PX = 56;
export const DESKTOP_LYRICS_TRANSITION_PIXELS_PER_SECOND = 185;
const DESKTOP_LYRICS_SPRING_STIFFNESS = 200;
const DESKTOP_LYRICS_SPRING_SETTLE_EPSILON = 0.001;
const DESKTOP_LYRICS_SPRING_MIN_SETTLE_MS = 300;
const DESKTOP_LYRICS_SPRING_MAX_SETTLE_MS = 1_000;

/**
 * Selects whether a line change is natural playback or a discontinuity.  A backwards change is
 * always a seek/repeat snap; skipped lines can still animate when their timestamps are dense.
 */
export function resolveDesktopLyricsTransitionMode(
  previousIndex: number | null,
  targetIndex: number,
  previousTimeSeconds: number | null = null,
  targetTimeSeconds: number | null = null,
): DesktopLyricsTransitionMode {
  if (previousIndex === null || previousIndex < 0 || targetIndex < 0) return "SNAP";
  if (targetIndex < previousIndex) return "SNAP";
  const indexGap = Math.abs(targetIndex - previousIndex);
  const denseAdvance = previousTimeSeconds !== null
    && targetTimeSeconds !== null
    && Number.isFinite(previousTimeSeconds)
    && Number.isFinite(targetTimeSeconds)
    && targetTimeSeconds >= previousTimeSeconds
    && targetTimeSeconds - previousTimeSeconds <= DESKTOP_LYRICS_DENSE_INTERVAL_SECONDS;
  return indexGap > 1 && !denseAdvance ? "SNAP" : "ANIMATE";
}

/** Computes the same bounded visual duration used by the Android line transition. */
export function desktopLyricsTransitionDurationMs(
  lineDistance: number,
  scrollDistance = 0,
): number {
  const distance = Math.max(
    Math.abs(finiteNumber(lineDistance)) * DESKTOP_LYRICS_TRANSITION_LINE_DISTANCE_PX,
    Math.abs(finiteNumber(scrollDistance)),
  );
  return Math.max(
    DESKTOP_LYRICS_TRANSITION_MIN_DURATION_MS,
    Math.min(
      DESKTOP_LYRICS_TRANSITION_MAX_DURATION_MS,
      Math.trunc(distance / DESKTOP_LYRICS_TRANSITION_PIXELS_PER_SECOND * 1_000),
    ),
  );
}

/** Starts a transition with the source line fully emphasized. */
export function createDesktopLyricsTransition(
  sourceIndex: number,
  targetIndex: number,
  startedAtMs: number,
  durationMs = desktopLyricsTransitionDurationMs(targetIndex - sourceIndex),
): DesktopLyricsTransition {
  return {
    sourceIndex,
    targetIndex,
    startLinePosition: sourceIndex,
    startedAtMs: finiteNumber(startedAtMs),
    durationMs: normalizeTransitionDuration(durationMs),
    startWeights: sourceIndex >= 0 ? [{ index: sourceIndex, value: 1 }] : [],
    initialVelocityPerMs: 0,
    preserveVelocity: false,
  };
}

/** Samples all visible emphasis weights at a monotonic timestamp. */
export function sampleDesktopLyricsTransition(
  transition: DesktopLyricsTransition,
  nowMs: number,
): DesktopLyricsTransitionSample {
  const elapsed = Math.max(0, finiteNumber(nowMs) - transition.startedAtMs);
  const rawProgress = transition.durationMs <= 0
    ? 1
    : Math.max(0, Math.min(1, elapsed / transition.durationMs));
  const timing = transition.preserveVelocity
    ? noBounceSpringTiming(elapsed / 1_000, transition.initialVelocityPerMs * 1_000)
    : fastOutSlowInTiming(rawProgress);
  const springDone = transition.preserveVelocity
    && ((elapsed >= DESKTOP_LYRICS_SPRING_MIN_SETTLE_MS
      && 1 - timing.value <= DESKTOP_LYRICS_SPRING_SETTLE_EPSILON)
      || elapsed >= DESKTOP_LYRICS_SPRING_MAX_SETTLE_MS);
  const done = transition.preserveVelocity ? springDone : rawProgress >= 1;
  const progress = done ? 1 : timing.value;
  const linePosition = transition.startLinePosition
    + (transition.targetIndex - transition.startLinePosition) * progress;
  const sourceWeights = new Map<number, number>();
  transition.startWeights.forEach(({ index, value }) => {
    if (index < 0 || !Number.isFinite(value) || value <= 0) return;
    sourceWeights.set(index, Math.max(sourceWeights.get(index) ?? 0, Math.min(1, value)));
  });
  const indices = new Set(sourceWeights.keys());
  if (transition.targetIndex >= 0) indices.add(transition.targetIndex);
  const weights = [...indices]
    .map((index) => {
      const initial = sourceWeights.get(index) ?? 0;
      const target = index === transition.targetIndex ? 1 : 0;
      return { index, value: initial + (target - initial) * progress };
    })
    .filter(({ value }) => value > 1 / 1_024)
    .sort((left, right) => right.value - left.value);
  return {
    progress,
    linePosition,
    velocityPerMs: done
      ? 0
      : transition.preserveVelocity
        ? timing.slope / 1_000
        : timing.slope / transition.durationMs,
    done,
    weights,
  };
}

/** Retargets an in-flight transition while preserving every visible weight at this frame. */
export function retargetDesktopLyricsTransition(
  transition: DesktopLyricsTransition,
  targetIndex: number,
  nowMs: number,
  durationMs?: number,
): DesktopLyricsTransition {
  const sampled = sampleDesktopLyricsTransition(transition, nowMs);
  const source = sampled.weights
    .filter(({ index }) => index !== targetIndex)
    .sort((left, right) => {
      const weightDifference = right.value - left.value;
      if (Math.abs(weightDifference) > Number.EPSILON) return weightDifference;
      if (left.index === transition.targetIndex) return -1;
      if (right.index === transition.targetIndex) return 1;
      return 0;
    })[0]?.index
    ?? transition.targetIndex;
  return {
    sourceIndex: source,
    targetIndex,
    startLinePosition: sampled.linePosition,
    startedAtMs: finiteNumber(nowMs),
    durationMs: normalizeTransitionDuration(
      durationMs ?? desktopLyricsTransitionDurationMs(targetIndex - sampled.linePosition),
    ),
    startWeights: sampled.weights,
    initialVelocityPerMs: sampled.velocityPerMs,
    preserveVelocity: true,
  };
}

/** Maps a logical lyric index onto the shared vertical transition axis. */
export function desktopLyricsLineShiftPx(
  lineIndex: number,
  linePosition: number,
  layoutSlotOffset = 0,
): number {
  return (finiteNumber(lineIndex) - finiteNumber(linePosition) - finiteNumber(layoutSlotOffset))
    * DESKTOP_LYRICS_TRANSITION_LINE_DISTANCE_PX;
}

export function desktopLyricsTransitionWeight(
  sample: DesktopLyricsTransitionSample,
  index: number,
): number {
  return sample.weights.find((weight) => weight.index === index)?.value ?? 0;
}

export function smoothstep(value: number): number {
  const clamped = Math.max(0, Math.min(1, finiteNumber(value)));
  return clamped * clamped * (3 - 2 * clamped);
}

/** Material/Compose FastOutSlowInEasing: cubic-bezier(0.4, 0, 0.2, 1). */
export function fastOutSlowIn(value: number): number {
  return fastOutSlowInTiming(value).value;
}

interface DesktopLyricsTimingSample {
  value: number;
  slope: number;
}

function fastOutSlowInTiming(value: number): DesktopLyricsTimingSample {
  const input = Math.max(0, Math.min(1, finiteNumber(value)));
  if (input === 0 || input === 1) return { value: input, slope: 0 };
  let lower = 0;
  let upper = 1;
  for (let iteration = 0; iteration < 30; iteration += 1) {
    const candidate = (lower + upper) / 2;
    if (cubicBezierCoordinate(candidate, 0.4, 0.2) < input) lower = candidate;
    else upper = candidate;
  }
  const curveTime = (lower + upper) / 2;
  const xVelocity = cubicBezierDerivative(curveTime, 0.4, 0.2);
  const yVelocity = cubicBezierDerivative(curveTime, 0, 1);
  return {
    value: cubicBezierCoordinate(curveTime, 0, 1),
    slope: xVelocity > Number.EPSILON ? yVelocity / xVelocity : 0,
  };
}

/** Compose's no-bounce spring: damping ratio 1 and low stiffness 200. */
function noBounceSpringTiming(elapsedSeconds: number, initialVelocityPerSecond: number): DesktopLyricsTimingSample {
  const time = Math.max(0, finiteNumber(elapsedSeconds));
  const velocity = Math.max(0, finiteNumber(initialVelocityPerSecond));
  const angularFrequency = Math.sqrt(DESKTOP_LYRICS_SPRING_STIFFNESS);
  const coefficient = angularFrequency - velocity;
  const decay = Math.exp(-angularFrequency * time);
  return {
    value: Math.max(0, Math.min(1, 1 - (1 + coefficient * time) * decay)),
    slope: Math.max(0, (velocity + angularFrequency * coefficient * time) * decay),
  };
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

function cubicBezierCoordinate(time: number, firstControl: number, secondControl: number): number {
  const inverse = 1 - time;
  return 3 * inverse * inverse * time * firstControl
    + 3 * inverse * time * time * secondControl
    + time * time * time;
}

function cubicBezierDerivative(time: number, firstControl: number, secondControl: number): number {
  const inverse = 1 - time;
  return 3 * inverse * inverse * firstControl
    + 6 * inverse * time * (secondControl - firstControl)
    + 3 * time * time * (1 - secondControl);
}

function normalizeTransitionDuration(durationMs: number): number {
  return Math.max(
    DESKTOP_LYRICS_TRANSITION_MIN_DURATION_MS,
    Math.min(DESKTOP_LYRICS_TRANSITION_MAX_DURATION_MS, Math.trunc(finiteNumber(durationMs))),
  );
}

function finiteNumber(value: number): number {
  return Number.isFinite(value) ? value : 0;
}
