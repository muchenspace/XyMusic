<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { Lock, Minus, Pause, Play, Plus, SkipBack, SkipForward, X } from "@lucide/vue";
import {
  DEFAULT_DESKTOP_LYRICS_HIGHLIGHT_COLOR,
  DEFAULT_DESKTOP_LYRICS_TEXT_COLOR,
} from "../application/ports/UserInterfacePreferences";
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
import { buildDesktopLyricsFrame } from "./timeline";
import {
  resolveLyricPlaybackRenderPlan,
  resolveLyricWordProgress,
  shouldReanchorLyricPlaybackClock,
} from "../domain/lyricsTimeline";

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

const initialState = isSupportedState(props.initialState) ? props.initialState : null;
const state = ref<DesktopLyricsStatePayload | null>(initialState);
const clock = ref<LocalDesktopLyricsClock | null>(initialState ? localClock(clockFromState(initialState)) : null);
const nowMs = ref(monotonicNow());
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
  return buildDesktopLyricsFrame(
    state.value?.lyrics ?? null,
    currentClock,
    state.value?.offsetSeconds ?? 0,
    nowMs.value,
  );
});
const currentLine = computed(() => frame.value?.current ?? null);
const nextLine = computed(() => frame.value?.next ?? null);
const currentWordProgresses = computed(() => {
  const line = currentLine.value?.line;
  const playbackSeconds = frame.value?.playbackSeconds ?? 0;
  if (state.value?.lyrics?.timing !== "WORD" || !line?.words?.length) return [];
  return line.words.map((word) => ({ word, progress: resolveLyricWordProgress(word, playbackSeconds) }));
});
const emptyMessage = computed(() => {
  if (!state.value?.track) return "等待播放";
  return state.value.lyrics?.lines.length ? "等待歌词" : "暂无歌词";
});

function clearProtocolState(): void {
  state.value = null;
  clock.value = null;
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
  if (
    !clock.value ||
    clock.value.trackId !== stateClock.trackId ||
    (claimTimelineSample(stateClock) &&
      shouldReanchorLyricPlaybackClock(clock.value, stateClock, monotonicNow()))
  ) {
    latestTimelineSample = stateClock;
    clock.value = stateClock;
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
  let reanchored = false;
  if (
    !clock.value ||
    clock.value.trackId !== nextClock.trackId ||
    (
      shouldReanchorLyricPlaybackClock(clock.value, nextClock, monotonicNow()))
  ) {
    clock.value = nextClock;
    reanchored = true;
  }
  if (reanchored) schedulePlaybackUpdate(true);
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

function schedulePlaybackUpdate(reset = false, frameTimestamp?: number): void {
  if (reset) stopPlaybackUpdates();
  if (animationFrame !== null || wakeTimer !== null) return;
  nowMs.value = Number.isFinite(frameTimestamp) ? frameTimestamp! : monotonicNow();
  if (!canRenderPlayback()) return;

  const playbackSeconds = frame.value?.playbackSeconds;
  if (playbackSeconds === undefined) return;
  const plan = resolveLyricPlaybackRenderPlan(state.value?.lyrics ?? null, playbackSeconds);
  if (plan.requiresAnimationFrame) {
    const generation = playbackScheduleGeneration;
    animationFrame = window.requestAnimationFrame((timestamp) => {
      if (generation !== playbackScheduleGeneration) return;
      animationFrame = null;
      if (!canRenderPlayback()) return;
      schedulePlaybackUpdate(false, timestamp);
    });
    return;
  }
  if (plan.nextChangeAtSeconds === null) return;
  const delay = Math.max(MIN_WAKE_DELAY_MS, Math.ceil((plan.nextChangeAtSeconds - playbackSeconds) * 1_000));
  const generation = playbackScheduleGeneration;
  wakeTimer = window.setTimeout(() => {
    if (generation !== playbackScheduleGeneration) return;
    wakeTimer = null;
    if (!canRenderPlayback()) return;
    nowMs.value = monotonicNow();
    schedulePlaybackUpdate();
  }, delay);
}

function stopPlaybackUpdates(): void {
  playbackScheduleGeneration += 1;
  if (animationFrame !== null) {
    window.cancelAnimationFrame(animationFrame);
    animationFrame = null;
  }
  if (wakeTimer !== null) window.clearTimeout(wakeTimer);
  wakeTimer = null;
}

function canRenderPlayback(): boolean {
  return !disposed
    && isPlaying.value
    && renderActive.value
    && (typeof document === "undefined" || document.visibilityState !== "hidden");
}

function handleVisibilityChange(): void {
  if (document.visibilityState === "hidden") {
    stopPlaybackUpdates();
    return;
  }
  schedulePlaybackUpdate(true);
}

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

      <div v-if="currentLine" class="desktop-lyrics-copy" data-tauri-drag-region aria-live="off">
        <div
          class="desktop-lyric-line desktop-lyric-line-current"
          :class="{ 'has-started': currentLine.started }"
          data-tauri-drag-region
        >
          <p class="desktop-lyric-primary" data-tauri-drag-region>
            <span v-if="state?.lyrics?.timing === 'WORD'" class="desktop-lyric-words">
              <span
                v-for="({ word, progress }, wordIndex) in currentWordProgresses"
                :key="`${word.time}-${wordIndex}`"
                v-memo="[word, progress]"
                class="desktop-lyric-word"
                :class="{ 'is-sung': progress > 0, 'is-current': progress > 0 && progress < 1 }"
                :style="{ '--desktop-lyric-word-progress': `${progress * 100}%` }"
                data-tauri-drag-region
              >{{ word.text }}</span>
            </span>
            <span v-else data-tauri-drag-region>{{ currentLine.line.text }}</span>
          </p>
          <p
            v-if="state?.showTranslation && currentLine.line.translation"
            class="desktop-lyric-translation"
            data-tauri-drag-region
          >{{ currentLine.line.translation }}</p>
        </div>

        <div v-if="nextLine" class="desktop-lyric-line desktop-lyric-line-next" data-tauri-drag-region>
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
