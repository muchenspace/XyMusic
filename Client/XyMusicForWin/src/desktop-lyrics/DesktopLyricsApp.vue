<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { Lock, Minus, Pause, Play, Plus, SkipBack, SkipForward, X } from "@lucide/vue";
import {
  DEFAULT_DESKTOP_LYRICS_HIGHLIGHT_COLOR,
  DEFAULT_DESKTOP_LYRICS_TEXT_COLOR,
} from "../application/ports/UserInterfacePreferences";
import type { Lyrics } from "../domain/music";
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
  DESKTOP_LYRICS_TRANSITION_LINE_DISTANCE_PX,
  buildDesktopLyricsFrame,
  estimatePlaybackSeconds,
  fastOutSlowIn,
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

const initialState = isSupportedState(props.initialState) ? props.initialState : null;
const state = ref<DesktopLyricsStatePayload | null>(initialState);
const clock = ref<LocalDesktopLyricsClock | null>(initialState ? localClock(clockFromState(initialState)) : null);
const nowMs = ref(monotonicNow());
const lyricTransition = ref<DesktopLyricsTransition | null>(null);
const desktopLyricsCopyElement = ref<HTMLElement | null>(null);
const currentLineElement = ref<HTMLElement | null>(null);
const topLineElement = ref<HTMLElement | null>(null);
const bottomLineElement = ref<HTMLElement | null>(null);
const transitionLineDistancePx = ref(DESKTOP_LYRICS_TRANSITION_LINE_DISTANCE_PX);
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
let playbackPositionCorrection: DesktopPlaybackPositionCorrection | null = null;
let lastRenderedPlaybackSeconds = clock.value?.positionSeconds ?? 0;
let lyricLayoutObserver: ResizeObserver | null = null;
let transitionDistanceMeasureQueued = false;

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
const topLine = computed(() => frame.value?.top ?? null);
const bottomLine = computed(() => frame.value?.bottom ?? null);
const activeSlot = computed(() => frame.value?.activeSlot ?? null);
const topWordProgresses = computed(() => {
  const lineFrame = topLine.value;
  if (state.value?.lyrics?.timing !== "WORD" || !lineFrame?.line.words?.length || (activeSlot.value !== "top" && activeSlot.value !== null)) return [];
  return lineFrame.line.words.map((word, wordIndex) => ({
    word,
    progress: desktopLyricWordProgress(lineFrame.index, wordIndex),
  }));
});
const bottomWordProgresses = computed(() => {
  const lineFrame = bottomLine.value;
  if (state.value?.lyrics?.timing !== "WORD" || !lineFrame?.line.words?.length || activeSlot.value !== "bottom") return [];
  return lineFrame.line.words.map((word, wordIndex) => ({
    word,
    progress: desktopLyricWordProgress(lineFrame.index, wordIndex),
  }));
});

function slotLineStyle(slot: "top" | "bottom"): Record<string, string | number> {
  const isActive = activeSlot.value === slot || (activeSlot.value === null && slot === "top");
  return {
    "--desktop-lyric-line-emphasis": isActive ? 1 : 0,
    "--desktop-lyric-word-emphasis": isActive ? 1 : 0,
    "--desktop-lyric-transition-opacity": isActive ? 1 : 0.82,
    "--desktop-lyric-transition-shift": "0px",
    "--desktop-lyric-transition-scale": 1,
  };
}
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

function syncDesktopLyricsTransition(_identity: string, _targetIndex: number): void {
  lyricTransition.value = null;
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
  if (plan.requiresAnimationFrame || playbackPositionCorrection !== null) {
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
  lyricTransition.value = null;
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
    && isPlaying.value
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
watch(
  [
    () => currentLine.value?.index ?? -1,
    fontScale,
    () => state.value?.showTranslation ?? false,
  ],
  queueTransitionDistanceMeasure,
  { flush: "post" },
);

onMounted(() => {
  if (typeof ResizeObserver !== "undefined") {
    lyricLayoutObserver = new ResizeObserver(measureTransitionLineDistance);
  }
  window.addEventListener("resize", queueTransitionDistanceMeasure);
  queueTransitionDistanceMeasure();
  void connectBridgeListeners();
});

onBeforeUnmount(() => {
  disposed = true;
  stopPlaybackUpdates();
  stopReadyRetries();
  lyricLayoutObserver?.disconnect();
  window.removeEventListener("resize", queueTransitionDistanceMeasure);
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

function queueTransitionDistanceMeasure(): void {
  if (transitionDistanceMeasureQueued || disposed) return;
  transitionDistanceMeasureQueued = true;
  void nextTick(() => {
    transitionDistanceMeasureQueued = false;
    if (disposed) return;
    observeDesktopLyricLayout();
    measureTransitionLineDistance();
  });
}

function observeDesktopLyricLayout(): void {
  const observer = lyricLayoutObserver;
  if (!observer) return;
  observer.disconnect();
  const elements = [
    desktopLyricsCopyElement.value,
    currentLineElement.value,
    topLineElement.value,
    bottomLineElement.value,
  ];
  elements.forEach((element) => {
    if (element) observer.observe(element);
  });
}

function measureTransitionLineDistance(): void {
  if (disposed) return;
  const lineElements = [
    topLineElement.value,
    bottomLineElement.value,
    currentLineElement.value,
  ].filter((element): element is HTMLElement => element !== null);
  const tallestLineHeight = lineElements.reduce(
    (height, element) => Math.max(height, element.offsetHeight),
    0,
  );
  const rowGap = desktopLyricsRowGapPx(desktopLyricsCopyElement.value);
  transitionLineDistancePx.value = Math.max(
    DESKTOP_LYRICS_TRANSITION_LINE_DISTANCE_PX,
    tallestLineHeight > 0 ? tallestLineHeight + rowGap : 0,
  );
}

function desktopLyricsRowGapPx(element: HTMLElement | null): number {
  if (!element || typeof window === "undefined") return 0;
  const style = window.getComputedStyle(element);
  for (const value of [style.rowGap, style.gap]) {
    const pixels = Number.parseFloat(value);
    if (Number.isFinite(pixels)) return Math.max(0, pixels);
  }
  return 0;
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
        ref="desktopLyricsCopyElement"
        class="desktop-lyrics-copy"
        data-tauri-drag-region
        aria-live="off"
      >

        <div
          v-if="topLine"
          :ref="(el) => { topLineElement = el as HTMLElement | null; if (activeSlot === 'top' || activeSlot === null) currentLineElement = el as HTMLElement | null; }"
          class="desktop-lyric-line desktop-lyric-slot-top"
          :class="{
            'desktop-lyric-line-current is-active': activeSlot === 'top' || (activeSlot === null && topLine.index === 0),
            'desktop-lyric-line-next is-upcoming': activeSlot === 'bottom',
            'has-started': topLine.started,
          }"
          :style="slotLineStyle('top')"
          data-tauri-drag-region
        >
          <p class="desktop-lyric-primary" data-tauri-drag-region>
            <WordByWordLyricText
              v-if="state?.lyrics?.timing === 'WORD' && (activeSlot === 'top' || activeSlot === null) && topWordProgresses.length"
              :text="topLine.line.text"
              :words="topLine.line.words ?? []"
              :progresses="topWordProgresses.map(({ progress }) => progress)"
              container-class="desktop-lyric-words"
              highlight-color="color-mix(in srgb, var(--desktop-lyric-highlight) calc(var(--desktop-lyric-word-emphasis, 0) * 100%), var(--desktop-lyric-idle))"
            >
              <span
                v-for="({ word, progress }, wordIndex) in topWordProgresses"
                :key="`${word.time}-${wordIndex}`"
                v-memo="[word, progress]"
                class="desktop-lyric-word"
                dir="auto"
                :class="{ 'is-sung': progress > 0, 'is-current': progress > 0 && progress < 1 }"
                :style="{ '--desktop-lyric-word-progress': `${progress * 100}%` }"
                data-tauri-drag-region
              >{{ word.text }}</span>
            </WordByWordLyricText>
            <span v-else data-tauri-drag-region>{{ topLine.line.text }}</span>
          </p>
          <p
            v-if="state?.showTranslation && topLine.line.translation"
            class="desktop-lyric-translation"
            data-tauri-drag-region
          >{{ topLine.line.translation }}</p>
        </div>
        <div v-else class="desktop-lyric-line desktop-lyric-slot-top desktop-lyric-placeholder" aria-hidden="true">&nbsp;</div>

        <div
          v-if="bottomLine"
          :ref="(el) => { bottomLineElement = el as HTMLElement | null; if (activeSlot === 'bottom') currentLineElement = el as HTMLElement | null; }"
          class="desktop-lyric-line desktop-lyric-slot-bottom"
          :class="{
            'desktop-lyric-line-current is-active': activeSlot === 'bottom',
            'desktop-lyric-line-next is-upcoming': activeSlot !== 'bottom',
            'has-started': bottomLine.started,
          }"
          :style="slotLineStyle('bottom')"
          data-tauri-drag-region
        >
          <p class="desktop-lyric-primary" data-tauri-drag-region>
            <WordByWordLyricText
              v-if="state?.lyrics?.timing === 'WORD' && activeSlot === 'bottom' && bottomWordProgresses.length"
              :text="bottomLine.line.text"
              :words="bottomLine.line.words ?? []"
              :progresses="bottomWordProgresses.map(({ progress }) => progress)"
              container-class="desktop-lyric-words"
              highlight-color="color-mix(in srgb, var(--desktop-lyric-highlight) calc(var(--desktop-lyric-word-emphasis, 0) * 100%), var(--desktop-lyric-idle))"
            >
              <span
                v-for="({ word, progress }, wordIndex) in bottomWordProgresses"
                :key="`${word.time}-${wordIndex}`"
                v-memo="[word, progress]"
                class="desktop-lyric-word"
                dir="auto"
                :class="{ 'is-sung': progress > 0, 'is-current': progress > 0 && progress < 1 }"
                :style="{ '--desktop-lyric-word-progress': `${progress * 100}%` }"
                data-tauri-drag-region
              >{{ word.text }}</span>
            </WordByWordLyricText>
            <span v-else data-tauri-drag-region>{{ bottomLine.line.text }}</span>
          </p>
          <p
            v-if="state?.showTranslation && bottomLine.line.translation"
            class="desktop-lyric-translation"
            data-tauri-drag-region
          >{{ bottomLine.line.translation }}</p>
        </div>
        <div v-else class="desktop-lyric-line desktop-lyric-slot-bottom desktop-lyric-placeholder" aria-hidden="true">&nbsp;</div>
      </div>

      <div v-else class="desktop-lyrics-empty" data-tauri-drag-region role="status">
        <strong data-tauri-drag-region>{{ emptyMessage }}</strong>
        <span v-if="state?.track" data-tauri-drag-region>{{ state.track.title }} · {{ state.track.artist }}</span>
      </div>
    </section>
  </main>
</template>

<style src="../styles/desktop-lyrics.css"></style>
