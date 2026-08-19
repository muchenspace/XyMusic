import { onBeforeUnmount, onMounted, ref, watch } from "vue";
import {
  LYRIC_PLAYBACK_POSITION_CORRECTION_EPSILON_SECONDS,
  LYRIC_PLAYBACK_POSITION_CORRECTION_MS,
  LYRIC_PLAYBACK_POSITION_SNAP_THRESHOLD_SECONDS,
  interpolateLyricPlaybackSeconds,
  type LyricPlaybackClock,
  type LyricPlaybackRenderPlan,
} from "../../domain/lyricsTimeline";

interface PlaybackSource {
  currentTime: () => number;
  isPlaying: () => boolean;
  isActive?: () => boolean;
  discontinuityToken?: () => unknown;
  renderPlan?: (positionSeconds: number) => LyricPlaybackRenderPlan;
  renderPlanDependencies?: () => readonly unknown[];
}

type ReanchorSources = readonly [number, boolean, readonly unknown[], unknown] | false;

interface PlaybackPositionCorrection {
  offsetSeconds: number;
  startedAtMs: number;
}

export function useSmoothLyricsPlaybackPosition(source: PlaybackSource) {
  const isActive = source.isActive ?? (() => true);
  const displayedPosition = ref(normalizePosition(source.currentTime()));
  let lastKnownNow = monotonicNow();
  let clock: LyricPlaybackClock = createClock(source, now());
  let animationFrame: number | null = null;
  let wakeTimer: number | null = null;
  let scheduleGeneration = 0;
  let disposed = false;
  let lastUpdateAt = 0;
  let correction: PlaybackPositionCorrection | null = null;

  const update = (frameTimestamp?: number) => {
    if (!canUpdate()) return;
    const nowMs = now(frameTimestamp);
    const positionSeconds = renderedPositionAt(nowMs);
    const plan = source.renderPlan?.(positionSeconds);

    if (plan) {
      displayedPosition.value = positionSeconds;
      if (plan.requiresAnimationFrame || correction !== null) {
        scheduleFrame();
      } else {
        scheduleWake(plan.nextChangeAtSeconds, positionSeconds);
      }
      return;
    }

    if (lastUpdateAt === 0 || nowMs - lastUpdateAt >= LYRICS_UPDATE_INTERVAL_MS) {
      displayedPosition.value = positionSeconds;
      lastUpdateAt = nowMs;
    }
    scheduleFrame();
  };

  const scheduleFrame = () => {
    if (animationFrame !== null) return;
    const generation = scheduleGeneration;
    animationFrame = requestFrame((timestamp) => {
      // Cancellation can race a callback that was already selected for delivery.
      if (generation !== scheduleGeneration) return;
      animationFrame = null;
      update(timestamp);
    });
  };

  const stop = () => {
    scheduleGeneration += 1;
    if (animationFrame !== null) cancelFrame(animationFrame);
    animationFrame = null;
    if (wakeTimer !== null) window.clearTimeout(wakeTimer);
    wakeTimer = null;
    lastUpdateAt = 0;
  };

  const start = () => {
    if (!canUpdate() || animationFrame !== null || wakeTimer !== null) return;
    scheduleFrame();
  };

  const reanchor = (renderPlanDependenciesChanged = false, forceSnap = false) => {
    if (!isActive()) {
      stop();
      return;
    }
    const nowMs = now();
    const nextClock = createClock(source, nowMs);
    const currentPosition = renderedPositionAt(nowMs);
    const positionError = nextClock.positionSeconds - currentPosition;
    const shouldSnap = forceSnap
      || !nextClock.isPlaying
      || clock.isPlaying !== nextClock.isPlaying
      || Math.abs(positionError) > LYRIC_PLAYBACK_POSITION_SNAP_THRESHOLD_SECONDS;
    if (shouldSnap) {
      stop();
      clock = nextClock;
      correction = null;
      displayedPosition.value = nextClock.positionSeconds;
    } else {
      clock = nextClock;
      const correctionOffset = currentPosition - nextClock.positionSeconds;
      correction = Math.abs(correctionOffset) > LYRIC_PLAYBACK_POSITION_CORRECTION_EPSILON_SECONDS
        ? { offsetSeconds: correctionOffset, startedAtMs: nowMs }
        : null;
    }
    if (!shouldSnap && (renderPlanDependenciesChanged || (correction !== null && wakeTimer !== null))) {
      // A line-timed plan can be asleep until its next visual boundary. Re-run
      // it immediately when inputs such as the lyric offset or clock correction change.
      stop();
    }
    if (clock.isPlaying) start();
    else stop();
  };

  const reanchorSources = (): ReanchorSources => {
    // LyricsView remains mounted while its overlay is closed. Avoid subscribing
    // it to the player's frequent position snapshots until the view is visible.
    if (!isActive()) return false;
    return [
      source.currentTime(),
      source.isPlaying(),
      source.renderPlanDependencies?.() ?? EMPTY_RENDER_PLAN_DEPENDENCIES,
      source.discontinuityToken?.(),
    ];
  };
  watch(reanchorSources, (nextSources, previousSources) => {
    const renderPlanDependenciesChanged = nextSources !== false
      && previousSources !== false
      && previousSources !== undefined
      && !areRenderPlanDependenciesEqual(nextSources[2], previousSources[2]);
    const discontinuityChanged = nextSources !== false
      && previousSources !== undefined
      && (previousSources === false || !Object.is(nextSources[3], previousSources[3]));
    reanchor(renderPlanDependenciesChanged, discontinuityChanged);
  }, { immediate: true });

  const handleVisibilityChange = () => {
    if (document.visibilityState === "hidden") {
      stop();
      return;
    }
    reanchor();
  };

  onMounted(() => document.addEventListener("visibilitychange", handleVisibilityChange));

  onBeforeUnmount(() => {
    disposed = true;
    stop();
    document.removeEventListener("visibilitychange", handleVisibilityChange);
  });

  return displayedPosition;

  function canUpdate(): boolean {
    return !disposed
      && isActive()
      && clock.isPlaying
      && (typeof document === "undefined" || document.visibilityState !== "hidden");
  }

  function renderedPositionAt(nowMs: number): number {
    const basePosition = interpolateLyricPlaybackSeconds(clock, nowMs);
    const activeCorrection = correction;
    if (!activeCorrection) return Math.max(displayedPosition.value, basePosition);
    const progress = Math.max(
      0,
      Math.min(1, (nowMs - activeCorrection.startedAtMs) / LYRIC_PLAYBACK_POSITION_CORRECTION_MS),
    );
    const remainingOffset = activeCorrection.offsetSeconds * (1 - fastOutSlowIn(progress));
    if (progress >= 1) correction = null;
    return Math.max(displayedPosition.value, basePosition + remainingOffset);
  }

  function scheduleWake(nextChangeAtSeconds: number | null, positionSeconds: number): void {
    if (nextChangeAtSeconds === null || !Number.isFinite(nextChangeAtSeconds)) return;
    const delayMs = Math.max(
      MIN_WAKE_DELAY_MS,
      Math.ceil((nextChangeAtSeconds - positionSeconds) * 1_000),
    );
    const generation = scheduleGeneration;
    wakeTimer = window.setTimeout(() => {
      if (generation !== scheduleGeneration) return;
      wakeTimer = null;
      if (!canUpdate()) return;
      scheduleFrame();
    }, delayMs);
  }

  function now(frameTimestamp?: number): number {
    const candidate = Number.isFinite(frameTimestamp) ? frameTimestamp! : monotonicNow();
    lastKnownNow = Math.max(lastKnownNow, candidate);
    return lastKnownNow;
  }
}

function createClock(source: PlaybackSource, anchoredAtMs: number): LyricPlaybackClock {
  return {
    positionSeconds: normalizePosition(source.currentTime()),
    anchoredAtMs,
    isPlaying: source.isPlaying(),
  };
}

function normalizePosition(value: number): number {
  return Number.isFinite(value) ? Math.max(0, value) : 0;
}

const LYRICS_UPDATE_INTERVAL_MS = 1_000 / 30;
const MIN_WAKE_DELAY_MS = 4;
const EMPTY_RENDER_PLAN_DEPENDENCIES: readonly unknown[] = [];

function areRenderPlanDependenciesEqual(
  current: readonly unknown[],
  previous: readonly unknown[],
): boolean {
  return current.length === previous.length
    && current.every((dependency, index) => Object.is(dependency, previous[index]));
}

/** Material/Compose FastOutSlowInEasing: cubic-bezier(0.4, 0, 0.2, 1). */
function fastOutSlowIn(value: number): number {
  const input = Math.max(0, Math.min(1, value));
  let lower = 0;
  let upper = 1;
  for (let iteration = 0; iteration < 16; iteration += 1) {
    const candidate = (lower + upper) / 2;
    if (cubicBezierCoordinate(candidate, 0.4, 0.2) < input) lower = candidate;
    else upper = candidate;
  }
  return cubicBezierCoordinate((lower + upper) / 2, 0, 1);
}

function cubicBezierCoordinate(time: number, firstControl: number, secondControl: number): number {
  const inverse = 1 - time;
  return 3 * inverse * inverse * time * firstControl
    + 3 * inverse * time * time * secondControl
    + time * time * time;
}

function monotonicNow(): number {
  const timestamp = typeof performance !== "undefined" ? performance.now() : Date.now();
  return Number.isFinite(timestamp) ? timestamp : Date.now();
}

function requestFrame(callback: FrameRequestCallback): number {
  return typeof window.requestAnimationFrame === "function"
    ? window.requestAnimationFrame(callback)
    : window.setTimeout(() => callback(monotonicNow()), LYRICS_UPDATE_INTERVAL_MS);
}

function cancelFrame(handle: number): void {
  if (typeof window.cancelAnimationFrame === "function") window.cancelAnimationFrame(handle);
  else window.clearTimeout(handle);
}
