import { onBeforeUnmount, onMounted, ref, watch } from "vue";
import {
  interpolateLyricPlaybackSeconds,
  shouldReanchorLyricPlaybackClock,
  type LyricPlaybackClock,
  type LyricPlaybackRenderPlan,
} from "../../domain/lyricsTimeline";

interface PlaybackSource {
  currentTime: () => number;
  isPlaying: () => boolean;
  isActive?: () => boolean;
  renderPlan?: (positionSeconds: number) => LyricPlaybackRenderPlan;
  renderPlanDependencies?: () => readonly unknown[];
}

type ReanchorSources = readonly [number, boolean, readonly unknown[]] | false;

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

  const update = (frameTimestamp?: number) => {
    if (!canUpdate()) return;
    const nowMs = now(frameTimestamp);
    const positionSeconds = interpolateLyricPlaybackSeconds(clock, nowMs);
    const plan = source.renderPlan?.(positionSeconds);

    if (plan) {
      displayedPosition.value = positionSeconds;
      if (plan.requiresAnimationFrame) {
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

  const reanchor = (renderPlanDependenciesChanged = false) => {
    if (!isActive()) {
      stop();
      return;
    }
    const nowMs = now();
    const nextClock = createClock(source, nowMs);
    const isDiscontinuous = shouldReanchorLyricPlaybackClock(clock, nextClock, nowMs);
    if (isDiscontinuous) {
      stop();
      clock = nextClock;
      displayedPosition.value = nextClock.positionSeconds;
    } else if (renderPlanDependenciesChanged) {
      // A line-timed plan can be asleep until its next visual boundary. Re-run
      // it immediately when inputs such as the lyric offset change.
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
    ];
  };
  watch(reanchorSources, (nextSources, previousSources) => {
    const renderPlanDependenciesChanged = nextSources !== false
      && previousSources !== false
      && previousSources !== undefined
      && !areRenderPlanDependenciesEqual(nextSources[2], previousSources[2]);
    reanchor(renderPlanDependenciesChanged);
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
