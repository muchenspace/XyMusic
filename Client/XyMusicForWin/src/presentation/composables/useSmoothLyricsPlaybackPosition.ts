import { onBeforeUnmount, ref, watch } from "vue";
import {
  interpolateLyricPlaybackSeconds,
  shouldReanchorLyricPlaybackClock,
  type LyricPlaybackClock,
} from "../../domain/lyricsTimeline";

interface PlaybackSource {
  currentTime: () => number;
  isPlaying: () => boolean;
}

export function useSmoothLyricsPlaybackPosition(source: PlaybackSource) {
  const displayedPosition = ref(normalizePosition(source.currentTime()));
  let clock: LyricPlaybackClock = createClock(source);
  let animationFrame: number | null = null;
  let disposed = false;

  const update = () => {
    animationFrame = null;
    if (disposed || !clock.isPlaying) return;
    displayedPosition.value = interpolateLyricPlaybackSeconds(clock, Date.now());
    animationFrame = requestAnimationFrame(update);
  };

  const stop = () => {
    if (animationFrame !== null) cancelAnimationFrame(animationFrame);
    animationFrame = null;
  };

  const start = () => {
    if (
      disposed ||
      !clock.isPlaying ||
      animationFrame !== null ||
      typeof window.requestAnimationFrame !== "function"
    ) return;
    animationFrame = requestAnimationFrame(update);
  };

  const reanchor = () => {
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

  watch([source.currentTime, source.isPlaying], reanchor, { immediate: true });

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
