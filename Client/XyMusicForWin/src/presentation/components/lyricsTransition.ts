import type { LyricLine } from "../../domain/music";

/** Keep these values in one place so the normal and desktop lyric surfaces can agree on motion. */
export const LYRIC_TRANSITION_MIN_DURATION_MS = 300;
export const LYRIC_TRANSITION_MAX_DURATION_MS = 520;
export const LYRIC_TRANSITION_LINE_DISTANCE_PX = 56;
export const LYRIC_TRANSITION_PIXELS_PER_SECOND = 185;
export const LYRIC_CORRECTION_MIN_DURATION_MS = 90;
export const LYRIC_CORRECTION_MAX_DURATION_MS = 180;
export const DENSE_LYRIC_TIME_GAP_MS = 450;
export const LYRIC_BASELINE_MAX_FRAME_COUNT = 8;
export const LYRIC_REQUIRED_STABLE_FRAMES = 2;
export const LYRIC_LAYOUT_STABILITY_EPSILON_PX = 0.5;
export const LYRIC_SEEK_ACK_TIMEOUT_MS = 1_500;

export type LyricTransitionMode = "snap" | "animate";

export interface LyricEmphasisTransition {
  startPhase: number;
  endPhase: number;
  targetLineIndex: number;
  startEmphasis: ReadonlyMap<number, number>;
}

export function lyricTransitionMode(
  previousLyricIndex: number | null,
  lyricIndex: number,
  previousLyricTimeSeconds?: number | null,
  lyricTimeSeconds?: number | null,
): LyricTransitionMode {
  if (previousLyricIndex === null || lyricIndex < previousLyricIndex) return "snap";
  const indexGap = Math.abs(lyricIndex - previousLyricIndex);
  const denseNaturalAdvance = previousLyricTimeSeconds !== null
    && previousLyricTimeSeconds !== undefined
    && lyricTimeSeconds !== null
    && lyricTimeSeconds !== undefined
    && lyricTimeSeconds >= previousLyricTimeSeconds
    && (lyricTimeSeconds - previousLyricTimeSeconds) * 1_000 <= DENSE_LYRIC_TIME_GAP_MS;
  return indexGap > 1 && !denseNaturalAdvance ? "snap" : "animate";
}

export function lyricTransitionDurationMs(lineDistance: number, scrollDistance: number): number {
  const visualDistance = Math.max(
    Math.abs(lineDistance) * LYRIC_TRANSITION_LINE_DISTANCE_PX,
    Math.abs(scrollDistance),
  );
  return clamp(
    Math.trunc((visualDistance / LYRIC_TRANSITION_PIXELS_PER_SECOND) * 1_000),
    LYRIC_TRANSITION_MIN_DURATION_MS,
    LYRIC_TRANSITION_MAX_DURATION_MS,
  );
}

export function correctionDurationMs(transitionDurationMs: number): number {
  return clamp(
    transitionDurationMs,
    LYRIC_CORRECTION_MIN_DURATION_MS,
    LYRIC_CORRECTION_MAX_DURATION_MS,
  );
}

export function settledLyricEmphasis(lineIndex: number): LyricEmphasisTransition {
  return lineIndex >= 0
    ? {
      startPhase: 0,
      endPhase: 0,
      targetLineIndex: lineIndex,
      startEmphasis: new Map([[lineIndex, 1]]),
    }
    : emptyLyricEmphasisTransition();
}

export function emptyLyricEmphasisTransition(): LyricEmphasisTransition {
  return { startPhase: 0, endPhase: 0, targetLineIndex: -1, startEmphasis: new Map() };
}

/** Sample the visible weights before changing the target, preserving an interrupted transition. */
export function retargetLyricEmphasis(
  transition: LyricEmphasisTransition,
  emphasisPhase: number,
  targetLineIndex: number,
): LyricEmphasisTransition {
  if (!Number.isFinite(emphasisPhase) || targetLineIndex < 0) return emptyLyricEmphasisTransition();
  const sampled = new Map<number, number>();
  const candidates = new Set<number>([
    ...transition.startEmphasis.keys(),
    transition.targetLineIndex,
  ]);
  candidates.forEach((lineIndex) => {
    const emphasis = lyricLineTransitionEmphasis(emphasisPhase, lineIndex, transition);
    if (emphasis > 1 / 1_024) sampled.set(lineIndex, emphasis);
  });
  return {
    startPhase: emphasisPhase,
    endPhase: emphasisPhase + 1,
    targetLineIndex,
    startEmphasis: sampled,
  };
}

export function lyricLineTransitionEmphasis(
  emphasisPhase: number,
  lineIndex: number,
  transition: LyricEmphasisTransition,
): number {
  if (!Number.isFinite(emphasisPhase) || lineIndex < 0 || transition.targetLineIndex < 0) return 0;
  const progress = transition.endPhase <= transition.startPhase
    ? 1
    : clamp(
      (emphasisPhase - transition.startPhase) / (transition.endPhase - transition.startPhase),
      0,
      1,
    );
  const initial = transition.startEmphasis.get(lineIndex) ?? 0;
  const target = lineIndex === transition.targetLineIndex ? 1 : 0;
  return clamp(initial + (target - initial) * progress, 0, 1);
}

export function lyricTransitionIsSettled(
  animatedLinePosition: number,
  emphasisPhase: number,
  lineIndex: number,
  transition: LyricEmphasisTransition,
): boolean {
  if (lineIndex < 0 || !Number.isFinite(animatedLinePosition) || !Number.isFinite(emphasisPhase)) return false;
  const phaseProgress = transition.endPhase <= transition.startPhase
    ? 1
    : clamp((emphasisPhase - transition.startPhase) / (transition.endPhase - transition.startPhase), 0, 1);
  return Math.abs(animatedLinePosition - lineIndex) <= 0.01
    && lineIndex === transition.targetLineIndex
    && phaseProgress >= 1 - 0.01;
}

/** Android's smoothstep gate prevents a word overlay flashing on the exact line boundary. */
export function smoothLyricEmphasis(value: number): number {
  const clamped = clamp(value, 0, 1);
  return clamped * clamped * (3 - 2 * clamped);
}

/** Approximation of Compose FastOutSlowIn (cubic-bezier(0.4, 0, 0.2, 1)). */
export function fastOutSlowIn(value: number): number {
  const x = clamp(value, 0, 1);
  let t = x;
  for (let iteration = 0; iteration < 5; iteration += 1) {
    const current = cubicBezier(t, 0, 0.4, 0.2, 1) - x;
    const derivative = cubicBezierDerivative(t, 0, 0.4, 0.2, 1);
    if (Math.abs(derivative) < 1e-5) break;
    t = clamp(t - current / derivative, 0, 1);
  }
  return cubicBezier(t, 0, 0, 1, 1);
}

/** Compose's critically damped low-stiffness spring (damping ratio 1, stiffness 200). */
export function noBounceSpring(elapsedSeconds: number, initialVelocity = 0): number {
  const time = Math.max(0, Number.isFinite(elapsedSeconds) ? elapsedSeconds : 0);
  const angularFrequency = Math.sqrt(200);
  const displacement = 1
    - (1 + (angularFrequency - initialVelocity) * time) * Math.exp(-angularFrequency * time);
  return clamp(displacement, 0, 1);
}

export function lyricLineTime(line: LyricLine | undefined): number | null {
  return line?.time ?? null;
}

export function canonicalLyricTargetIndex(lines: readonly LyricLine[], requestedIndex: number): number {
  const requestedTime = lines[requestedIndex]?.time;
  if (requestedTime === null || requestedTime === undefined) return requestedIndex;
  for (let index = lines.length - 1; index >= 0; index -= 1) {
    if (lines[index]?.time === requestedTime) return index;
  }
  return requestedIndex;
}

export function lyricSeekBaselineIndex(
  sourceIndex: number,
  targetIndex: number,
  currentIndex: number,
): number | null {
  if (currentIndex === targetIndex) return targetIndex;
  if (targetIndex > sourceIndex && currentIndex > targetIndex) return targetIndex;
  if (targetIndex < sourceIndex && currentIndex < targetIndex) return targetIndex;
  return null;
}

export function lyricLayoutDeltaHasSettled(
  previousDelta: number | null,
  currentDelta: number | null,
): boolean {
  return previousDelta !== null
    && currentDelta !== null
    && Number.isFinite(previousDelta)
    && Number.isFinite(currentDelta)
    && Math.abs(currentDelta - previousDelta) <= LYRIC_LAYOUT_STABILITY_EPSILON_PX;
}

function cubicBezier(t: number, p0: number, p1: number, p2: number, p3: number): number {
  const inverse = 1 - t;
  return inverse ** 3 * p0
    + 3 * inverse ** 2 * t * p1
    + 3 * inverse * t ** 2 * p2
    + t ** 3 * p3;
}

function cubicBezierDerivative(t: number, p0: number, p1: number, p2: number, p3: number): number {
  const inverse = 1 - t;
  return 3 * inverse ** 2 * (p1 - p0)
    + 6 * inverse * t * (p2 - p1)
    + 3 * t ** 2 * (p3 - p2);
}

function clamp(value: number, minimum: number, maximum: number): number {
  if (!Number.isFinite(value)) return minimum;
  return Math.max(minimum, Math.min(maximum, value));
}
