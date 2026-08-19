<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { Lock, Minus, Pause, Play, Plus, SkipBack, SkipForward, X } from "@lucide/vue";
import {
  DEFAULT_DESKTOP_LYRICS_HIGHLIGHT_COLOR,
  DEFAULT_DESKTOP_LYRICS_TEXT_COLOR,
} from "../application/ports/UserInterfacePreferences";
import type { LyricLine, Lyrics } from "../domain/music";
import type { DesktopLyricsBridge, DesktopLyricsUnlisten } from "./bridge";
import { createDesktopLyricsBridge } from "./bridge";
import type {
  DesktopLyricsActionPayload,
  DesktopLyricsClockPayload,
  DesktopLyricsStatePayload,
} from "./protocol";
import {
  DESKTOP_LYRICS_PROTOCOL_VERSION,
  clockFromState,
  createDesktopLyricsColorAction,
  createDesktopLyricsAction,
  createDesktopLyricsFontScaleAction,
  createDesktopLyricsLockAction,
} from "./protocol";
import {
  buildDesktopLyricsFrame,
  createDesktopLyricsTransition,
  desktopLyricsLineShiftPx,
  desktopLyricsTransitionWeight,
  estimatePlaybackSeconds,
  fastOutSlowIn,
  resolveDesktopLyricsTransitionMode,
  retargetDesktopLyricsTransition,
  sampleDesktopLyricsTransition,
  smoothstep,
  type DesktopLyricsTransition,
} from "./timeline";
import {
  LYRIC_PLAYBACK_POSITION_CORRECTION_EPSILON_SECONDS,
  LYRIC_PLAYBACK_POSITION_CORRECTION_MS,
  LYRIC_PLAYBACK_POSITION_SNAP_THRESHOLD_SECONDS,
  resolveLyricPlaybackRenderPlan,
  resolveLyricWordProgress,
} from "../domain/lyricsTimeline";
import WordByWordLyricText from "../shared/lyrics/WordByWordLyricText.vue";

const MIN_WAKE_DELAY_MS = 4;
const READY_RETRY_INITIAL_DELAY_MS = 250;
const READY_RETRY_MAX_DELAY_MS = 4_000;

const props = defineProps<{
  bridge?: DesktopLyricsBridge;
  initialState?: DesktopLyricsStatePayload | null;
}>();

interface LocalDesktopLyricsClock extends DesktopLyricsClockPayload {
  sourceAnchoredAtMs: number;
  sourceRevision: number | null;
}

interface DesktopPlaybackPositionCorrection {
  offsetSeconds: number;
  startedAtMs: number;
}

interface DesktopLyricsOutgoingLine {
  index: number;
  line: LyricLine;
  weight: number;
}

const initialState = isSupportedState(props.initialState) ? props.initialState : null;
const state = ref<DesktopLyricsStatePayload | null>(initialState);
const clock = ref<LocalDesktopLyricsClock | null>(initialState ? localClock(clockFromState(initialState)) : null);
const nowMs = ref(monotonicNow());
const lyricTransition = ref<DesktopLyricsTransition | null>(null);
const optimisticLocked = ref(false);
const optimisticFontScale = ref<number | null>(null);
const bridge = props.bridge ?? createDesktopLyricsBridge();
const unlisteners: DesktopLyricsUnlisten[] = [];
let disposed = false;
let animationFrame: number | null = null;
let wakeTimer: number | null = null;
let readyRetryTimer: number | null = null;
let readyRetryDelay = READY_RETRY_INITIAL_DELAY_MS;
let playbackScheduleGeneration = 0;
let stateRevision = initialState ? finiteRevision(initialState.revision) ?? 0 : -1;
let latestTimelineSample: LocalDesktopLyricsClock | null = clock.value;
let activeTransportEpoch: string | null = initialState?.transportEpoch ?? null;
const retiredTransportEpochs = new Set<string>();
let stateHandshakeComplete = initialState !== null;
let bridgeConnected = false;
let observedLyricsIdentity: string | null = null;
let observedActiveIndex: number | null = null;
let playbackPositionCorrection: DesktopPlaybackPositionCorrection | null = null;
let lastRenderedPlaybackSeconds = clock.value?.positionSeconds ?? 0;

const locked = computed(() => Boolean(state.value?.locked || optimisticLocked.value));
const isPlaying = computed(() => Boolean(clock.value?.isPlaying ?? state.value?.isPlaying));
const renderActive = computed(() => state.value?.renderActive !== false);
const fontScale = computed(() => clamp(optimisticFontScale.value ?? state.value?.fontScale ?? 1, 0.75, 1.5));
const rootStyle = computed<Record<string, string | number>>(() => ({
  "--desktop-lyric-scale": fontScale.value,
  "--desktop-lyric-text": state.value?.textColor || DEFAULT_DESKTOP_LYRICS_TEXT_COLOR,
  "--desktop-lyric-highlight": state.value?.highlightColor || DEFAULT_DESKTOP_LYRICS_HIGHLIGHT_COLOR,
}));
const frame = computed(() => {
  const currentClock = clock.value;
  if (!currentClock) return null;
  const playbackSeconds = renderedDesktopPlaybackSeconds(currentClock, nowMs.value);
  return buildDesktopLyricsFrame(
    state.value?.lyrics ?? null,
    {
      ...currentClock,
      isPlaying: false,
      positionSeconds: playbackSeconds,
      anchoredAtMs: nowMs.value,
    },
    state.value?.offsetSeconds ?? 0,
    nowMs.value,
  );
});
const lyricsIdentity = computed(() => desktopLyricsIdentity(
  state.value?.lyrics ?? null,
  clock.value?.trackId ?? null,
));
const currentLine = computed(() => frame.value?.current ?? null);
const nextLine = computed(() => frame.value?.next ?? null);
const lyricTransitionSample = computed(() => {
  const transition = lyricTransition.value;
  return transition ? sampleDesktopLyricsTransition(transition, nowMs.value) : null;
});
const transitionInProgress = computed(() => {
  const sample = lyricTransitionSample.value;
  return Boolean(sample && !sample.done);
});
const outgoingLines = computed<readonly DesktopLyricsOutgoingLine[]>(() => {
  const sample = lyricTransitionSample.value;
  const lyrics = state.value?.lyrics;
  const currentIndex = currentLine.value?.index;
  if (!sample || sample.done || !lyrics || currentIndex === undefined) return [];
  return sample.weights.flatMap(({ index, value }) => {
    if (index === currentIndex) return [];
    const line = lyrics.lines[index];
    return line ? [{ index, line, weight: value }] : [];
  });
});
const currentLineTransitionStyle = computed<Record<string, string | number>>(() => {
  const current = currentLine.value;
  const sample = lyricTransitionSample.value;
  if (!current || !sample || sample.done) return desktopLyricLineStyle(current?.started ? 1 : 0, 0);
  const weight = desktopLyricsTransitionWeight(sample, current.index);
  return desktopLyricLineStyle(
    weight,
    desktopLyricsLineShiftPx(current.index, sample.linePosition),
  );
});
const nextLineTransitionStyle = computed<Record<string, string | number>>(() => {
  const sample = lyricTransitionSample.value;
  const next = nextLine.value;
  const shift = sample && !sample.done && next
    ? desktopLyricsLineShiftPx(next.index, sample.linePosition, 1)
    : 0;
  return { "--desktop-lyric-next-shift": `${shift}px` };
});
const currentWordProgresses = computed(() => {
  const lineFrame = currentLine.value;
  if (state.value?.lyrics?.timing !== "WORD" || !lineFrame?.line.words?.length) return [];
  return lineFrame.line.words.map((word, wordIndex) => ({
    word,
    progress: desktopLyricWordProgress(lineFrame.index, wordIndex),
  }));
});
const emptyMessage = computed(() => {
  if (!state.value?.track) return "等待播放";
  return state.value.lyrics?.lines.length ? "等待歌词" : "暂无歌词";
});

function clearProtocolState(): void {
  state.value = null;
  clock.value = null;
  playbackPositionCorrection = null;
  lastRenderedPlaybackSeconds = 0;
  lyricTransition.value = null;
  observedLyricsIdentity = null;
  observedActiveIndex = null;
  stateRevision = -1;
  latestTimelineSample = null;
  activeTransportEpoch = null;
  optimisticLocked.value = false;
  optimisticFontScale.value = null;
  stateHandshakeComplete = false;
  stopPlaybackUpdates();
  nowMs.value = monotonicNow();
  if (bridgeConnected) requestReady();
}

function acceptTransportEpoch(incomingEpoch: string): boolean {
  if (incomingEpoch === activeTransportEpoch) return true;
  if (retiredTransportEpochs.has(incomingEpoch)) return false;

  if (activeTransportEpoch) retiredTransportEpochs.add(activeTransportEpoch);
  activeTransportEpoch = incomingEpoch;
  stateRevision = -1;
  latestTimelineSample = null;
  clock.value = null;
  playbackPositionCorrection = null;
  lastRenderedPlaybackSeconds = 0;
  return true;
}

function applyState(payload: DesktopLyricsStatePayload): void {
  if (disposed) return;
  if (payload.version !== DESKTOP_LYRICS_PROTOCOL_VERSION) {
    clearProtocolState();
    return;
  }
  const transportEpoch = transportEpochFrom(payload.transportEpoch);
  if (!transportEpoch || !acceptTransportEpoch(transportEpoch)) return;
  const incomingRevision = finiteRevision(payload.revision) ?? 0;
  if (incomingRevision < stateRevision) return;
  stateRevision = incomingRevision;
  state.value = payload;
  stateHandshakeComplete = true;
  stopReadyRetries();
  const stateClock = localClock(clockFromState(payload));
  if (!clock.value || clock.value.trackId !== stateClock.trackId) {
    latestTimelineSample = stateClock;
    applyTimelineClock(stateClock, true);
  } else if (claimTimelineSample(stateClock)) {
    applyTimelineClock(stateClock);
  }
  optimisticLocked.value = false;
  optimisticFontScale.value = null;
  schedulePlaybackUpdate(true);
}

function applyClock(payload: DesktopLyricsClockPayload): void {
  if (disposed) return;
  if (payload.version !== DESKTOP_LYRICS_PROTOCOL_VERSION) {
    clearProtocolState();
    return;
  }
  const transportEpoch = transportEpochFrom(payload.transportEpoch);
  if (!transportEpoch || transportEpoch !== activeTransportEpoch) return;
  const expectedTrackId = state.value?.track?.id ?? state.value?.lyrics?.trackId ?? null;
  if (payload.trackId !== expectedTrackId) return;
  const nextClock = localClock(payload);
  if (!claimTimelineSample(nextClock)) return;
  applyTimelineClock(nextClock, clock.value?.trackId !== nextClock.trackId);
  schedulePlaybackUpdate(true);
}

function applyTimelineClock(nextClock: LocalDesktopLyricsClock, forceSnap = false): void {
  const timestamp = monotonicNow();
  const previousClock = clock.value;
  const displayedPosition = previousClock
    ? renderedDesktopPlaybackSeconds(previousClock, timestamp)
    : estimatePlaybackSeconds(nextClock, timestamp);
  const targetPosition = estimatePlaybackSeconds(nextClock, timestamp);
  const positionError = targetPosition - displayedPosition;
  const shouldSnap = forceSnap
    || !previousClock
    || previousClock.positionDiscontinuityVersion !== nextClock.positionDiscontinuityVersion
    || !nextClock.isPlaying
    || previousClock.isPlaying !== nextClock.isPlaying
    || Math.abs(positionError) > LYRIC_PLAYBACK_POSITION_SNAP_THRESHOLD_SECONDS;

  if (shouldSnap) {
    playbackPositionCorrection = null;
    lastRenderedPlaybackSeconds = targetPosition;
  } else {
    const correctionOffset = displayedPosition - targetPosition;
    playbackPositionCorrection = Math.abs(correctionOffset) > LYRIC_PLAYBACK_POSITION_CORRECTION_EPSILON_SECONDS
      ? { offsetSeconds: correctionOffset, startedAtMs: timestamp }
      : null;
    lastRenderedPlaybackSeconds = displayedPosition;
  }
  clock.value = nextClock;
  nowMs.value = timestamp;
}

function renderedDesktopPlaybackSeconds(
  playbackClock: LocalDesktopLyricsClock,
  timestamp: number,
): number {
  const basePosition = estimatePlaybackSeconds(playbackClock, timestamp);
  const activeCorrection = playbackPositionCorrection;
  let candidate = basePosition;
  if (activeCorrection) {
    const progress = clamp(
      (timestamp - activeCorrection.startedAtMs) / LYRIC_PLAYBACK_POSITION_CORRECTION_MS,
      0,
      1,
    );
    candidate += activeCorrection.offsetSeconds * (1 - fastOutSlowIn(progress));
    if (progress >= 1) playbackPositionCorrection = null;
  }
  const rendered = playbackClock.isPlaying
    ? Math.max(lastRenderedPlaybackSeconds, candidate)
    : candidate;
  lastRenderedPlaybackSeconds = Math.max(0, rendered);
  return lastRenderedPlaybackSeconds;
}

function sendAction(action: DesktopLyricsActionPayload): void {
  void Promise.resolve().then(() => bridge.emitAction(action)).catch(() => undefined);
}

function requestReady(): void {
  if (disposed) return;
  sendAction(createDesktopLyricsAction("ready"));
  if (!stateHandshakeComplete) scheduleReadyRetry();
}

function scheduleReadyRetry(): void {
  if (readyRetryTimer !== null || disposed || stateHandshakeComplete) return;
  const delay = readyRetryDelay;
  readyRetryDelay = Math.min(readyRetryDelay * 2, READY_RETRY_MAX_DELAY_MS);
  readyRetryTimer = window.setTimeout(() => {
    readyRetryTimer = null;
    if (!stateHandshakeComplete) requestReady();
  }, delay);
}

function stopReadyRetries(): void {
  if (readyRetryTimer !== null) window.clearTimeout(readyRetryTimer);
  readyRetryTimer = null;
  readyRetryDelay = READY_RETRY_INITIAL_DELAY_MS;
}

function requestPrevious(): void {
  sendAction(createDesktopLyricsAction("previous"));
}

function requestTogglePlayback(): void {
  sendAction(createDesktopLyricsAction("toggle-playback"));
}

function requestNext(): void {
  sendAction(createDesktopLyricsAction("next"));
}

function requestFontScale(delta: number): void {
  const nextScale = clamp(fontScale.value + delta, 0.75, 1.5);
  optimisticFontScale.value = nextScale;
  sendAction(createDesktopLyricsFontScaleAction(nextScale));
}

function requestColor(action: "set-text-color" | "set-highlight-color", event: Event): void {
  sendAction(createDesktopLyricsColorAction(action, (event.target as HTMLInputElement).value));
}

function requestLock(): void {
  optimisticLocked.value = true;
  sendAction(createDesktopLyricsLockAction());
}

function requestClose(): void {
  sendAction(createDesktopLyricsAction("close"));
}

function syncDesktopLyricsTransition(identity: string, targetIndex: number): void {
  if (identity !== observedLyricsIdentity) {
    observedLyricsIdentity = identity;
    observedActiveIndex = targetIndex;
    lyricTransition.value = null;
    return;
  }
  const previousIndex = observedActiveIndex;
  if (previousIndex === targetIndex) return;
  observedActiveIndex = targetIndex;
  const lines = state.value?.lyrics?.lines;
  const previousTime = previousIndex === null || previousIndex < 0
    ? null
    : lines?.[previousIndex]?.time ?? null;
  const targetTime = targetIndex < 0 ? null : lines?.[targetIndex]?.time ?? null;
  const mode = resolveDesktopLyricsTransitionMode(
    previousIndex,
    targetIndex,
    previousTime,
    targetTime,
  );
  if (mode === "SNAP" || prefersReducedMotion()) {
    lyricTransition.value = null;
    return;
  }

  const timestamp = nowMs.value;
  const previousTransition = lyricTransition.value;
  lyricTransition.value = previousTransition
    && !sampleDesktopLyricsTransition(previousTransition, timestamp).done
    ? retargetDesktopLyricsTransition(previousTransition, targetIndex, timestamp)
    : createDesktopLyricsTransition(previousIndex ?? targetIndex, targetIndex, timestamp);
}

function schedulePlaybackUpdate(reset = false, frameTimestamp?: number): void {
  if (reset) stopPlaybackUpdates();
  if (animationFrame !== null || wakeTimer !== null) return;
  nowMs.value = Number.isFinite(frameTimestamp) ? frameTimestamp! : monotonicNow();
  settleCompletedLyricTransition();
  if (!canRenderTimeline()) return;

  const playbackSeconds = frame.value?.playbackSeconds;
  if (playbackSeconds === undefined) return;
  const plan = resolveLyricPlaybackRenderPlan(state.value?.lyrics ?? null, playbackSeconds);
  if (plan.requiresAnimationFrame || transitionInProgress.value || playbackPositionCorrection !== null) {
    const generation = playbackScheduleGeneration;
    animationFrame = requestPlaybackFrame((timestamp) => {
      if (generation !== playbackScheduleGeneration) return;
      animationFrame = null;
      if (!canRenderTimeline()) return;
      schedulePlaybackUpdate(false, timestamp);
    });
    return;
  }
  if (!isPlaying.value || plan.nextChangeAtSeconds === null) return;
  const delay = Math.max(MIN_WAKE_DELAY_MS, Math.ceil((plan.nextChangeAtSeconds - playbackSeconds) * 1_000));
  const generation = playbackScheduleGeneration;
  wakeTimer = window.setTimeout(() => {
    if (generation !== playbackScheduleGeneration) return;
    wakeTimer = null;
    if (!canRenderTimeline()) return;
    nowMs.value = monotonicNow();
    schedulePlaybackUpdate();
  }, delay);
}

function settleCompletedLyricTransition(): void {
  const sample = lyricTransitionSample.value;
  if (sample?.done) lyricTransition.value = null;
}

function stopPlaybackUpdates(): void {
  playbackScheduleGeneration += 1;
  if (animationFrame !== null) {
    cancelPlaybackFrame(animationFrame);
    animationFrame = null;
  }
  if (wakeTimer !== null) window.clearTimeout(wakeTimer);
  wakeTimer = null;
}

function canRenderTimeline(): boolean {
  return !disposed
    && renderActive.value
    && (isPlaying.value || transitionInProgress.value)
    && (typeof document === "undefined" || document.visibilityState !== "hidden");
}

function handleVisibilityChange(): void {
  if (document.visibilityState === "hidden") {
    stopPlaybackUpdates();
    return;
  }
  schedulePlaybackUpdate(true);
}

watch(
  [lyricsIdentity, () => frame.value?.activeIndex ?? -1],
  ([identity, targetIndex]) => syncDesktopLyricsTransition(identity, targetIndex),
  { immediate: true, flush: "sync" },
);
watch(isPlaying, () => schedulePlaybackUpdate(true));

onMounted(() => {
  void connectBridgeListeners();
});

onBeforeUnmount(() => {
  disposed = true;
  stopPlaybackUpdates();
  stopReadyRetries();
  document.removeEventListener("visibilitychange", handleVisibilityChange);
  unlisteners.splice(0).forEach((unlisten) => unlisten());
});

async function connectBridgeListeners(): Promise<void> {
  const registrations = [
    bridge.onState(applyState).then(retainUnlistener),
    bridge.onClock(applyClock).then(retainUnlistener),
  ];
  await Promise.allSettled(registrations);
  if (disposed) return;
  bridgeConnected = true;
  document.addEventListener("visibilitychange", handleVisibilityChange);
  schedulePlaybackUpdate(true);
  requestReady();
}

function retainUnlistener(unlisten: DesktopLyricsUnlisten): void {
  if (disposed) {
    unlisten();
    return;
  }
  unlisteners.push(unlisten);
}

function clamp(value: number, minimum: number, maximum: number): number {
  if (!Number.isFinite(value)) return minimum;
  return Math.max(minimum, Math.min(maximum, value));
}

function localClock(clock: DesktopLyricsClockPayload): LocalDesktopLyricsClock {
  return {
    ...clock,
    anchoredAtMs: monotonicNow(),
    sourceAnchoredAtMs: finiteTimestamp(clock.anchoredAtMs),
    sourceRevision: finiteRevision(clock.revision),
  };
}

function claimTimelineSample(next: LocalDesktopLyricsClock): boolean {
  const previous = latestTimelineSample;
  if (!previous) {
    latestTimelineSample = next;
    return true;
  }
  if (sameDesktopPlaybackSample(previous, next)) {
    // A newer state envelope can repeat the exact clock sample while changing
    // only lyric preferences. Advance the transport watermark without
    // starting a playback-position correction.
    if ((next.sourceRevision ?? -1) >= (previous.sourceRevision ?? -1)) latestTimelineSample = next;
    return false;
  }
  if (next.sourceRevision !== null && previous.sourceRevision !== null) {
    if (next.sourceRevision > previous.sourceRevision) {
      latestTimelineSample = next;
      return true;
    }
    if (next.sourceRevision < previous.sourceRevision) return false;
  }
  if (next.sourceAnchoredAtMs <= previous.sourceAnchoredAtMs) return false;
  latestTimelineSample = next;
  return true;
}

function sameDesktopPlaybackSample(
  current: LocalDesktopLyricsClock,
  next: LocalDesktopLyricsClock,
): boolean {
  return current.trackId === next.trackId
    && current.isPlaying === next.isPlaying
    && current.positionSeconds === next.positionSeconds
    && current.sourceAnchoredAtMs === next.sourceAnchoredAtMs
    && current.positionDiscontinuityVersion === next.positionDiscontinuityVersion;
}

function finiteTimestamp(value: number): number {
  return Number.isFinite(value) ? value : 0;
}

function finiteRevision(value: number | undefined): number | null {
  return Number.isFinite(value) ? Math.max(0, value!) : null;
}

function isSupportedState(
  value: DesktopLyricsStatePayload | null | undefined,
): value is DesktopLyricsStatePayload {
  return value?.version === DESKTOP_LYRICS_PROTOCOL_VERSION
    && transportEpochFrom(value.transportEpoch) !== null;
}

function transportEpochFrom(value: unknown): string | null {
  return typeof value === "string" && value.trim().length > 0 ? value : null;
}

function desktopLyricsIdentity(lyrics: Lyrics | null, clockTrackId: string | null): string {
  if (!lyrics) return JSON.stringify([clockTrackId, null]);
  return JSON.stringify([
    clockTrackId,
    lyrics.trackId,
    lyrics.synchronized,
    lyrics.timing,
    lyrics.lines.map((line) => [
      line.time,
      line.text,
      line.translation ?? null,
      line.words?.map((word) => [word.time, word.endTime ?? null, word.text]) ?? null,
    ]),
  ]);
}

function desktopLyricLineStyle(weight: number, shiftPx: number): Record<string, string | number> {
  const emphasis = clamp(weight, 0, 1);
  return {
    "--desktop-lyric-line-emphasis": emphasis,
    "--desktop-lyric-word-emphasis": smoothstep(emphasis),
    "--desktop-lyric-transition-shift": `${Number.isFinite(shiftPx) ? shiftPx : 0}px`,
    "--desktop-lyric-transition-scale": 1 + 0.012 * emphasis,
  };
}

function outgoingLineTransitionStyle(line: DesktopLyricsOutgoingLine): Record<string, string | number> {
  const sample = lyricTransitionSample.value;
  return desktopLyricLineStyle(
    line.weight,
    sample ? desktopLyricsLineShiftPx(line.index, sample.linePosition) : 0,
  );
}

function desktopLyricWordProgress(lineIndex: number, wordIndex: number): number {
  const lyrics = state.value?.lyrics;
  const line = lyrics?.lines[lineIndex];
  const word = line?.words?.[wordIndex];
  if (!word) return 0;
  return resolveLyricWordProgress(
    word,
    frame.value?.playbackSeconds ?? 0,
    line.words?.[wordIndex + 1]?.time,
    lyrics?.lines[lineIndex + 1]?.time,
  );
}

function prefersReducedMotion(): boolean {
  return typeof window !== "undefined"
    && Boolean(window.matchMedia?.("(prefers-reduced-motion: reduce)").matches);
}

function requestPlaybackFrame(callback: FrameRequestCallback): number {
  return typeof window.requestAnimationFrame === "function"
    ? window.requestAnimationFrame(callback)
    : window.setTimeout(() => callback(monotonicNow()), 16);
}

function cancelPlaybackFrame(handle: number): void {
  if (typeof window.cancelAnimationFrame === "function") window.cancelAnimationFrame(handle);
  else window.clearTimeout(handle);
}

function monotonicNow(): number {
  const timestamp = typeof performance !== "undefined" ? performance.now() : Date.now();
  return Number.isFinite(timestamp) ? timestamp : Date.now();
}

</script>

<template>
  <main
    class="desktop-lyrics-app"
    :class="{ 'is-locked': locked, 'is-empty': !currentLine }"
    :style="rootStyle"
    :aria-label="locked ? '桌面歌词，已锁定' : '桌面歌词'"
  >
    <section class="desktop-lyrics-surface">
      <div v-if="!locked" class="desktop-lyrics-drag-region" data-tauri-drag-region aria-hidden="true"></div>
      <nav v-if="!locked" class="desktop-lyrics-toolbar" aria-label="桌面歌词控制">
        <button type="button" title="上一首" aria-label="上一首" @click="requestPrevious">
          <SkipBack :size="17" fill="currentColor" aria-hidden="true" />
        </button>
        <button
          type="button"
          :title="isPlaying ? '暂停' : '播放'"
          :aria-label="isPlaying ? '暂停' : '播放'"
          @click="requestTogglePlayback"
        >
          <Pause v-if="isPlaying" :size="18" fill="currentColor" aria-hidden="true" />
          <Play v-else :size="18" fill="currentColor" aria-hidden="true" />
        </button>
        <button type="button" title="下一首" aria-label="下一首" @click="requestNext">
          <SkipForward :size="17" fill="currentColor" aria-hidden="true" />
        </button>
        <span class="desktop-lyrics-toolbar-divider" aria-hidden="true"></span>
        <button type="button" title="减小字号" aria-label="减小桌面歌词字号" @click="requestFontScale(-0.05)">
          <Minus :size="17" aria-hidden="true" />
        </button>
        <button type="button" title="增大字号" aria-label="增大桌面歌词字号" @click="requestFontScale(0.05)">
          <Plus :size="17" aria-hidden="true" />
        </button>
        <input class="desktop-lyrics-color-input" type="color" :value="state?.textColor || DEFAULT_DESKTOP_LYRICS_TEXT_COLOR" title="普通文字颜色" aria-label="普通文字颜色" @input="requestColor('set-text-color', $event)" />
        <input class="desktop-lyrics-color-input is-highlight" type="color" :value="state?.highlightColor || DEFAULT_DESKTOP_LYRICS_HIGHLIGHT_COLOR" title="高亮文字颜色" aria-label="高亮文字颜色" @input="requestColor('set-highlight-color', $event)" />
        <span class="desktop-lyrics-toolbar-divider" aria-hidden="true"></span>
        <button type="button" title="锁定并启用鼠标穿透" aria-label="锁定桌面歌词" @click="requestLock">
          <Lock :size="16" aria-hidden="true" />
        </button>
        <button type="button" title="隐藏桌面歌词" aria-label="隐藏桌面歌词" @click="requestClose">
          <X :size="18" aria-hidden="true" />
        </button>
      </nav>

      <div
        v-if="currentLine"
        class="desktop-lyrics-copy"
        :class="{ 'is-transitioning': transitionInProgress }"
        data-tauri-drag-region
        aria-live="off"
      >
        <div
          v-for="outgoingLine in outgoingLines"
          :key="`outgoing-${outgoingLine.index}`"
          class="desktop-lyric-line desktop-lyric-line-outgoing"
          :style="outgoingLineTransitionStyle(outgoingLine)"
          data-tauri-drag-region
          aria-hidden="true"
        >
          <p class="desktop-lyric-primary" data-tauri-drag-region>
            <WordByWordLyricText
              v-if="state?.lyrics?.timing === 'WORD' && outgoingLine.line.words?.length"
              :text="outgoingLine.line.text"
              :words="outgoingLine.line.words"
              :progresses="outgoingLine.line.words.map((_, wordIndex) => desktopLyricWordProgress(outgoingLine.index, wordIndex))"
              container-class="desktop-lyric-words"
              highlight-color="color-mix(in srgb, var(--desktop-lyric-highlight) calc(var(--desktop-lyric-word-emphasis, 0) * 100%), var(--desktop-lyric-idle))"
            >
              <span
                v-for="(word, wordIndex) in outgoingLine.line.words"
                :key="`${word.time}-${wordIndex}`"
                v-memo="[word, desktopLyricWordProgress(outgoingLine.index, wordIndex)]"
                class="desktop-lyric-word"
                dir="auto"
                :class="{
                  'is-sung': desktopLyricWordProgress(outgoingLine.index, wordIndex) > 0,
                  'is-current': desktopLyricWordProgress(outgoingLine.index, wordIndex) > 0 && desktopLyricWordProgress(outgoingLine.index, wordIndex) < 1,
                }"
                :style="{ '--desktop-lyric-word-progress': `${desktopLyricWordProgress(outgoingLine.index, wordIndex) * 100}%` }"
                data-tauri-drag-region
              >{{ word.text }}</span>
            </WordByWordLyricText>
            <span v-else data-tauri-drag-region>{{ outgoingLine.line.text }}</span>
          </p>
          <p
            v-if="state?.showTranslation && outgoingLine.line.translation"
            class="desktop-lyric-translation"
            data-tauri-drag-region
          >{{ outgoingLine.line.translation }}</p>
        </div>

        <div
          class="desktop-lyric-line desktop-lyric-line-current"
          :class="{ 'has-started': currentLine.started, 'is-transitioning': transitionInProgress }"
          :style="currentLineTransitionStyle"
          data-tauri-drag-region
        >
          <p class="desktop-lyric-primary" data-tauri-drag-region>
            <WordByWordLyricText
              v-if="state?.lyrics?.timing === 'WORD' && currentWordProgresses.length"
              :text="currentLine.line.text"
              :words="currentLine.line.words ?? []"
              :progresses="currentWordProgresses.map(({ progress }) => progress)"
              container-class="desktop-lyric-words"
              highlight-color="color-mix(in srgb, var(--desktop-lyric-highlight) calc(var(--desktop-lyric-word-emphasis, 0) * 100%), var(--desktop-lyric-idle))"
            >
              <span
                v-for="({ word, progress }, wordIndex) in currentWordProgresses"
                :key="`${word.time}-${wordIndex}`"
                v-memo="[word, progress]"
                class="desktop-lyric-word"
                dir="auto"
                :class="{ 'is-sung': progress > 0, 'is-current': progress > 0 && progress < 1 }"
                :style="{ '--desktop-lyric-word-progress': `${progress * 100}%` }"
                data-tauri-drag-region
              >{{ word.text }}</span>
            </WordByWordLyricText>
            <span v-else data-tauri-drag-region>{{ currentLine.line.text }}</span>
          </p>
          <p
            v-if="state?.showTranslation && currentLine.line.translation"
            class="desktop-lyric-translation"
            data-tauri-drag-region
          >{{ currentLine.line.translation }}</p>
        </div>

        <div v-if="nextLine" class="desktop-lyric-line desktop-lyric-line-next" :style="nextLineTransitionStyle" data-tauri-drag-region>
          <p class="desktop-lyric-primary" data-tauri-drag-region>{{ nextLine.line.text }}</p>
          <p
            v-if="state?.showTranslation && nextLine.line.translation"
            class="desktop-lyric-translation"
            data-tauri-drag-region
          >{{ nextLine.line.translation }}</p>
        </div>
        <div v-else class="desktop-lyric-line desktop-lyric-line-next desktop-lyric-placeholder" aria-hidden="true">&nbsp;</div>
      </div>

      <div v-else class="desktop-lyrics-empty" data-tauri-drag-region role="status">
        <strong data-tauri-drag-region>{{ emptyMessage }}</strong>
        <span v-if="state?.track" data-tauri-drag-region>{{ state.track.title }} · {{ state.track.artist }}</span>
      </div>
    </section>
  </main>
</template>

<style src="../styles/desktop-lyrics.css"></style>
