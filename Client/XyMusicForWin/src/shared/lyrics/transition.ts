/**
 * Single source of truth for lyric motion geometry shared by the normal lyrics
 * view and the desktop-lyrics window. Values are the Android-aligned constants
 * previously duplicated in both surfaces.
 */
export const LYRIC_TRANSITION_MIN_DURATION_MS = 300;
export const LYRIC_TRANSITION_MAX_DURATION_MS = 520;
export const LYRIC_TRANSITION_LINE_DISTANCE_PX = 56;
export const LYRIC_TRANSITION_PIXELS_PER_SECOND = 185;

export interface LyricTimingSample {
  value: number;
  slope: number;
}

/**
 * Material/Compose FastOutSlowInEasing: cubic-bezier(0.4, 0, 0.2, 1).
 * Solves the curve's x coordinate by bisection, matching the desktop-lyrics
 * implementation that was the most precise of the duplicated variants.
 */
export function fastOutSlowInTiming(value: number): LyricTimingSample {
  const input = clamp01(value);
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

export function fastOutSlowIn(value: number): number {
  return fastOutSlowInTiming(value).value;
}

export interface LyricCorrectionSample {
  remainingOffsetSeconds: number;
  finished: boolean;
}

/**
 * Decays a lyric position correction offset with FastOutSlowIn. Shared by the
 * normal lyrics view and the desktop-lyrics window so both surfaces converge
 * on the same curve and finish semantics.
 */
export function sampleLyricCorrection(
  offsetSeconds: number,
  startedAtMs: number,
  nowMs: number,
  durationMs: number,
): LyricCorrectionSample {
  const progress = clamp01((nowMs - startedAtMs) / durationMs);
  return {
    remainingOffsetSeconds: offsetSeconds * (1 - fastOutSlowIn(progress)),
    finished: progress >= 1,
  };
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

function clamp01(value: number): number {
  if (!Number.isFinite(value)) return 0;
  return Math.max(0, Math.min(1, value));
}
