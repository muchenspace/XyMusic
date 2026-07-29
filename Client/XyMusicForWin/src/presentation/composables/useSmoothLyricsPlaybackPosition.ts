import { onBeforeUnmount, ref, watch } from "vue";
import {
  interpolateLyricPlaybackSeconds,
  shouldReanchorLyricPlaybackClock,
  type LyricPlaybackClock,
} from "../../domain/lyricsTimeline";

interface PlaybackSource {
  currentTime: () => number;
  isPlaying: () => boolean;
  isActive?: () => boolean;
}

export function useSmoothLyricsPlaybackPosition(source: PlaybackSource) {
  const isActive = source.isActive ?? (() => true);
  const displayedPosition = ref(normalizePosition(source.currentTime()));
  let clock: LyricPlaybackClock = createClock(source);
  let animationFrame: number | null = null;
  let disposed = false;
  let lastUpdateAt = 0;

  const update = () => {
    animationFrame = null;
    if (disposed || !isActive() || !clock.isPlaying) return;
    const nowMs = Date.now();
    if (lastUpdateAt === 0 || nowMs - lastUpdateAt >= LYRICS_UPDATE_INTERVAL_MS) {
      displayedPosition.value = interpolateLyricPlaybackSeconds(clock, nowMs);
      lastUpdateAt = nowMs;
    }
    animationFrame = requestAnimationFrame(update);
  };

  const stop = () => {
    if (animationFrame !== null) cancelAnimationFrame(animationFrame);
    animationFrame = null;
    lastUpdateAt = 0;
  };

  const start = () => {
    if (
      disposed ||
      !isActive() ||
      !clock.isPlaying ||
      animationFrame !== null ||
      typeof window.requestAnimationFrame !== "function"
    ) return;
    animationFrame = requestAnimationFrame(update);
  };

  const reanchor = () => {
    if (!isActive()) {
      stop();
      return;
    }
    const nextClock = createClock(source);
    const nowMs = Date.now();
    const isDiscontinuous = shouldReanchorLyricPlaybackClock(clock, nextClock, nowMs);
    if (isDiscontinuous) {
      clock = nextClock;
      displayedPosition.value = nextClock.positionSeconds;
    }
    if (clock.isPlaying) start();
    else stop();
  };

  watch([source.currentTime, source.isPlaying, isActive], reanchor, { immediate: true });

  onBeforeUnmount(() => {
    disposed = true;
    stop();
  });

  return displayedPosition;
}

function createClock(source: PlaybackSource): LyricPlaybackClock {
  return {
    positionSeconds: normalizePosition(source.currentTime()),
    anchoredAtMs: Date.now(),
    isPlaying: source.isPlaying(),
  };
}

function normalizePosition(value: number): number {
  return Number.isFinite(value) ? Math.max(0, value) : 0;
}

const LYRICS_UPDATE_INTERVAL_MS = 1_000 / 30;
