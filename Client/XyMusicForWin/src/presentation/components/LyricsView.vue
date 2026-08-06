<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, provide, ref, type Ref, watch } from "vue";
import { Languages, Minus, Plus, RotateCcw } from "@lucide/vue";
import type { LyricLine, Track } from "../../domain/music";
import {
  resolveLyricPlaybackPosition,
  resolveLyricPlaybackRenderPlan,
} from "../../domain/lyricsTimeline";
import { useSmoothLyricsPlaybackPosition } from "../composables/useSmoothLyricsPlaybackPosition";
import { useLyricsStore } from "../stores/lyricsStore";
import { usePlayerStore } from "../stores/playerStore";
import { useDesktopWindowStore } from "../stores/desktopWindowStore";
import LyricsLineContent from "./LyricsLineContent.vue";
import LyricsPlayerControls from "./LyricsPlayerControls.vue";
import ArtworkImage from "./ui/ArtworkImage.vue";
import { lyricsPlaybackPositionKey } from "./lyricsPlaybackPosition";

const player = usePlayerStore();
const props = withDefaults(defineProps<{ fullscreen?: boolean }>(), { fullscreen: false });
const emit = defineEmits<{ favorite: [track: Track] }>();
const lyricsStore = useLyricsStore();
const displayedPlaybackTime = useSmoothLyricsPlaybackPosition({
  currentTime: () => player.currentTime,
  isPlaying: () => player.isPlaying && !player.loading,
  isActive: () => player.lyricsOpen,
  renderPlan: (positionSeconds) => {
    const plan = resolveLyricPlaybackRenderPlan(
      lyricsStore.lyrics,
      positionSeconds + lyricsStore.offset,
    );
    return {
      ...plan,
      nextChangeAtSeconds: plan.nextChangeAtSeconds === null
        ? null
        : plan.nextChangeAtSeconds - lyricsStore.offset,
    };
  },
  renderPlanDependencies: () => [lyricsStore.lyrics, lyricsStore.offset],
});
provide(lyricsPlaybackPositionKey, displayedPlaybackTime);
const windowControls = useDesktopWindowStore();
const viewElement = ref<HTMLElement | null>(null);
const lyricsScrollElement = ref<HTMLElement | null>(null);
const lineElements = ref<Array<HTMLElement | undefined>>([]);
const lineActiveStates: Ref<boolean>[] = [];
const lineOutgoingStates: Ref<boolean>[] = [];
const outgoingHighlightTimers: Array<number | undefined> = [];
const lyricsMenuElement = ref<HTMLElement | null>(null);
const lyricsMenu = ref({ open: false, x: 0, y: 0 });
const activeIndex = ref(-1);
const draggingLyrics = ref(false);
let previouslyFocused: HTMLElement | null = null;
let lyricsMenuReturnFocus: HTMLElement | null = null;
let autoFollowPaused = false;
let autoFollowTimer = 0;
let lastAutoFollowLineIndex: number | null = null;
let lyricFollowAnimationFrame: number | null = null;
let lyricFollowAnimationGeneration = 0;
let lyricFollowTargetTop: number | null = null;
let lyricFollowLastFrameAt = 0;
let pendingSeekAutoFollowSnap = false;
let pendingSeekAutoFollowSnapTimer = 0;
let scrollPointerId: number | null = null;
let scrollPointerStartY = 0;
let scrollPointerStartTop = 0;
let scrollPointerMoved = false;
let scrollPointerCaptured = false;
let manualScrollFrame: number | null = null;
let pendingManualScrollTop: number | null = null;
let manualScrollListenersAttached = false;
let suppressLyricClick = false;
let suppressClickTimer = 0;
let activeLineIndex = -1;
let activeLyrics = lyricsStore.lyrics;
const focusableSelector = [
  "button:not([disabled])",
  "input:not([disabled])",
  "select:not([disabled])",
  "textarea:not([disabled])",
  "[href]",
  "[tabindex]:not([tabindex='-1'])",
].join(",");

function toggleMaximizeWindow(): void {
  if (props.fullscreen) return;
  void windowControls.toggleMaximize().catch(() => undefined);
}

function updateActivePosition(): void {
  const lyrics = lyricsStore.lyrics;
  if (lyrics !== activeLyrics) {
    clearOutgoingHighlightTimers();
    lineElements.value = [];
    lineActiveStates.length = 0;
    lineOutgoingStates.length = 0;
    activeLineIndex = -1;
    activeLyrics = lyrics;
  }
  const playbackTime = displayedPlaybackTime.value + lyricsStore.offset;
  const nextActiveLineIndex = resolveLyricPlaybackPosition(lyrics, playbackTime).lineIndex;
  if (nextActiveLineIndex === activeLineIndex) {
    if (activeIndex.value !== nextActiveLineIndex) activeIndex.value = nextActiveLineIndex;
    return;
  }

  const previousActiveLineIndex = activeLineIndex;
  activeLineIndex = nextActiveLineIndex;
  if (previousActiveLineIndex >= 0) {
    setLineActive(previousActiveLineIndex, false);
    setLineOutgoing(previousActiveLineIndex, true);
    scheduleOutgoingHighlightClear(previousActiveLineIndex);
  }
  setLineOutgoing(nextActiveLineIndex, false);
  setLineActive(nextActiveLineIndex, true);
  setLineElementActivity(lineElements.value[previousActiveLineIndex], false);
  setLineElementActivity(lineElements.value[nextActiveLineIndex], true);
  activeIndex.value = nextActiveLineIndex;
}

function activeStateForLine(index: number): Readonly<Ref<boolean>> {
  let activeState = lineActiveStates[index];
  if (!activeState) {
    activeState = ref(false);
    lineActiveStates[index] = activeState;
  }
  return activeState;
}

function outgoingStateForLine(index: number): Readonly<Ref<boolean>> {
  let outgoingState = lineOutgoingStates[index];
  if (!outgoingState) {
    outgoingState = ref(false);
    lineOutgoingStates[index] = outgoingState;
  }
  return outgoingState;
}

function setLineElement(index: number, element: unknown): void {
  const lineElement = element instanceof HTMLElement ? element : undefined;
  lineElements.value[index] = lineElement;
  if (lineElement) setLineElementActivity(lineElement, index === activeLineIndex);
}

function setLineActive(index: number, active: boolean): void {
  if (index < 0) return;
  let activeState = lineActiveStates[index];
  if (!activeState) {
    activeState = ref(false);
    lineActiveStates[index] = activeState;
  }
  activeState.value = active;
}

function setLineOutgoing(index: number, outgoing: boolean): void {
  if (index < 0) return;
  let outgoingState = lineOutgoingStates[index];
  if (!outgoingState) {
    outgoingState = ref(false);
    lineOutgoingStates[index] = outgoingState;
  }
  outgoingState.value = outgoing;
  if (!outgoing) clearOutgoingHighlightTimer(index);
}

function scheduleOutgoingHighlightClear(index: number): void {
  clearOutgoingHighlightTimer(index);
  outgoingHighlightTimers[index] = window.setTimeout(() => {
    outgoingHighlightTimers[index] = undefined;
    setLineOutgoing(index, false);
  }, LYRIC_OUTGOING_HIGHLIGHT_DURATION_MS);
}

function clearOutgoingHighlightTimer(index: number): void {
  const timer = outgoingHighlightTimers[index];
  if (timer === undefined) return;
  window.clearTimeout(timer);
  outgoingHighlightTimers[index] = undefined;
}

function clearOutgoingHighlightTimers(): void {
  outgoingHighlightTimers.forEach((timer, index) => {
    if (timer !== undefined) window.clearTimeout(timer);
    outgoingHighlightTimers[index] = undefined;
  });
  lineOutgoingStates.forEach((state) => { state.value = false; });
}

function setLineElementActivity(element: HTMLElement | undefined, active: boolean): void {
  if (!element) return;
  element.classList.toggle("active", active);
  if (active) element.setAttribute("aria-current", "true");
  else element.removeAttribute("aria-current");
}

watch(
  () => player.lyricsOpen
    ? [displayedPlaybackTime.value, lyricsStore.offset, lyricsStore.lyrics] as const
    : null,
  (visiblePlayback) => {
    if (visiblePlayback) updateActivePosition();
  },
  { immediate: true },
);

async function scrollToActiveLine(index = activeIndex.value): Promise<void> {
  if (!player.lyricsOpen || autoFollowPaused || index < 0) return;
  await nextTick();
  if (!player.lyricsOpen || autoFollowPaused || index !== activeIndex.value) return;
  const scroll = lyricsScrollElement.value;
  const lineElement = lineElements.value[index];
  if (!scroll || !lineElement) return;
  const reducedMotion = window.matchMedia?.("(prefers-reduced-motion: reduce)").matches ?? false;
  const previousIndex = lastAutoFollowLineIndex;
  const previousTime = previousIndex === null ? null : lyricsStore.lyrics?.lines[previousIndex]?.time ?? null;
  const nextTime = lyricsStore.lyrics?.lines[index]?.time ?? null;
  const densePlaybackCatchUp = previousTime !== null
    && nextTime !== null
    && Math.abs(nextTime - previousTime) <= DENSE_LYRIC_INTERVAL_SECONDS;
  const skippedLines = previousIndex !== null && Math.abs(index - previousIndex) > 1;
  const useInstantPositioning = reducedMotion
    || previousIndex === null
    || (skippedLines && !densePlaybackCatchUp)
    || pendingSeekAutoFollowSnap;
  const targetTop = centeredLyricScrollTop(scroll, lineElement);
  clearPendingSeekAutoFollowSnap();
  if (useInstantPositioning) {
    cancelLyricFollowAnimation();
    scroll.scrollTop = targetTop;
  } else {
    scheduleLyricFollow(targetTop);
  }
  lastAutoFollowLineIndex = index;
}

function centeredLyricScrollTop(scroll: HTMLElement, lineElement: HTMLElement): number {
  const scrollRect = scroll.getBoundingClientRect();
  const lineRect = lineElement.getBoundingClientRect();
  const target = scroll.scrollTop
    + (lineRect.top - scrollRect.top)
    - (scroll.clientHeight - lineRect.height) / 2;
  const maximum = Math.max(0, scroll.scrollHeight - scroll.clientHeight);
  return Math.max(0, Math.min(maximum, target));
}

function scheduleLyricFollow(targetTop: number): void {
  lyricFollowTargetTop = targetTop;
  if (lyricFollowAnimationFrame !== null) return;
  const generation = ++lyricFollowAnimationGeneration;
  lyricFollowLastFrameAt = 0;
  lyricFollowAnimationFrame = requestLyricFollowFrame((timestamp) => {
    runLyricFollowFrame(generation, timestamp);
  });
}

function runLyricFollowFrame(generation: number, timestamp: number): void {
  if (generation !== lyricFollowAnimationGeneration) return;
  lyricFollowAnimationFrame = null;
  const scroll = lyricsScrollElement.value;
  const targetTop = lyricFollowTargetTop;
  if (!scroll || targetTop === null) {
    lyricFollowTargetTop = null;
    lyricFollowLastFrameAt = 0;
    return;
  }

  const elapsed = lyricFollowLastFrameAt === 0
    ? LYRIC_FOLLOW_FRAME_INTERVAL_MS
    : Math.min(50, Math.max(8, timestamp - lyricFollowLastFrameAt));
  lyricFollowLastFrameAt = timestamp;
  const remaining = targetTop - scroll.scrollTop;
  if (Math.abs(remaining) <= LYRIC_FOLLOW_SETTLE_EPSILON_PX) {
    scroll.scrollTop = targetTop;
    lyricFollowTargetTop = null;
    lyricFollowLastFrameAt = 0;
    return;
  }

  const response = 1 - Math.exp(-elapsed / LYRIC_FOLLOW_RESPONSE_MS);
  scroll.scrollTop += remaining * response;
  lyricFollowAnimationFrame = requestLyricFollowFrame((nextTimestamp) => {
    runLyricFollowFrame(generation, nextTimestamp);
  });
}

function cancelLyricFollowAnimation(): void {
  lyricFollowAnimationGeneration += 1;
  if (lyricFollowAnimationFrame !== null) {
    cancelLyricFollowFrame(lyricFollowAnimationFrame);
    lyricFollowAnimationFrame = null;
  }
  lyricFollowTargetTop = null;
  lyricFollowLastFrameAt = 0;
}

function requestLyricFollowFrame(callback: FrameRequestCallback): number {
  if (typeof window.requestAnimationFrame === "function") return window.requestAnimationFrame(callback);
  return window.setTimeout(() => callback(monotonicNow()), LYRIC_FOLLOW_FRAME_INTERVAL_MS);
}

function cancelLyricFollowFrame(handle: number): void {
  if (typeof window.cancelAnimationFrame === "function") window.cancelAnimationFrame(handle);
  else window.clearTimeout(handle);
}

function markSeekForAutoFollow(): void {
  pendingSeekAutoFollowSnap = true;
  window.clearTimeout(pendingSeekAutoFollowSnapTimer);
  pendingSeekAutoFollowSnapTimer = window.setTimeout(() => {
    pendingSeekAutoFollowSnap = false;
    pendingSeekAutoFollowSnapTimer = 0;
  }, SEEK_AUTO_FOLLOW_SNAP_EXPIRY_MS);
}

function clearPendingSeekAutoFollowSnap(): void {
  pendingSeekAutoFollowSnap = false;
  window.clearTimeout(pendingSeekAutoFollowSnapTimer);
  pendingSeekAutoFollowSnapTimer = 0;
}

watch([activeIndex, () => player.lyricsOpen], ([index, open]) => {
  if (open) void scrollToActiveLine(index);
});

watch(() => player.lyricsOpen, async (open) => {
  if (open) {
    resetAutoFollow();
    previouslyFocused = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    await nextTick();
    viewElement.value?.focus();
  } else {
    lyricsStore.flushPreferences();
    closeLyricsMenu(false);
    resetAutoFollow();
    previouslyFocused?.focus();
    previouslyFocused = null;
  }
});

function seek(line: LyricLine, event: MouseEvent) {
  if (suppressLyricClick) {
    event.preventDefault();
    event.stopPropagation();
    return;
  }
  if (line.time !== null) {
    markSeekForAutoFollow();
    player.seekTo(line.time - lyricsStore.offset);
  }
}

function closeOnEscape(event: KeyboardEvent) {
  if (event.key !== "Escape") return;
  if (lyricsMenu.value.open) {
    event.preventDefault();
    closeLyricsMenu();
    return;
  }
  if (player.lyricsOpen) {
    event.preventDefault();
    player.toggleLyrics();
  }
}

function trapFocus(event: KeyboardEvent): void {
  if (event.key !== "Tab" || !viewElement.value) return;
  const focusable = Array.from(viewElement.value.querySelectorAll<HTMLElement>(focusableSelector));
  if (!focusable.length) {
    event.preventDefault();
    viewElement.value.focus();
    return;
  }
  const first = focusable[0]!;
  const last = focusable[focusable.length - 1]!;
  if (document.activeElement === viewElement.value) {
    event.preventDefault();
    (event.shiftKey ? last : first).focus();
  } else if (event.shiftKey && document.activeElement === first) {
    event.preventDefault();
    last.focus();
  } else if (!event.shiftKey && document.activeElement === last) {
    event.preventDefault();
    first.focus();
  }
}

const formatTime = (seconds: number) => {
  const value = Number.isFinite(seconds) && seconds > 0 ? seconds : 0;
  return `${Math.floor(value / 60)}:${String(Math.floor(value % 60)).padStart(2, "0")}`;
};

async function openLyricsMenu(event: MouseEvent) {
  lyricsMenuReturnFocus = event.currentTarget instanceof HTMLElement ? event.currentTarget : null;
  lyricsMenu.value = { open: true, x: event.clientX, y: event.clientY };
  await nextTick();
  const menu = lyricsMenuElement.value;
  if (!menu) return;
  const gap = 10;
  lyricsMenu.value = {
    open: true,
    x: Math.max(gap, Math.min(event.clientX, window.innerWidth - menu.offsetWidth - gap)),
    y: Math.max(gap, Math.min(event.clientY, window.innerHeight - menu.offsetHeight - gap)),
  };
  await nextTick();
  menu.querySelector<HTMLElement>("button")?.focus();
}

function closeLyricsMenu(restoreFocus = true): void {
  if (!lyricsMenu.value.open) return;
  lyricsMenu.value.open = false;
  const returnFocus = lyricsMenuReturnFocus;
  lyricsMenuReturnFocus = null;
  if (!restoreFocus || !returnFocus) return;
  void nextTick(() => {
    if (player.lyricsOpen && returnFocus.isConnected) returnFocus.focus({ preventScroll: true });
  });
}

function closeLyricsMenuFromOutside(event: PointerEvent) {
  if (!lyricsMenu.value.open || lyricsMenuElement.value?.contains(event.target as Node)) return;
  const clickedElement = event.target instanceof HTMLElement ? event.target : null;
  closeLyricsMenu(!clickedElement?.closest(focusableSelector));
}

function closeLyricsMenuOnResize(): void {
  closeLyricsMenu();
}

function pauseAutoFollow(): void {
  autoFollowPaused = true;
  window.clearTimeout(autoFollowTimer);
  cancelLyricFollowAnimation();
  autoFollowTimer = window.setTimeout(() => {
    autoFollowTimer = 0;
    autoFollowPaused = false;
    void scrollToActiveLine();
  }, AUTO_FOLLOW_RESUME_MS);
}

function handleLyricsWheel(event: WheelEvent): void {
  if (!event.ctrlKey) {
    pauseAutoFollow();
    return;
  }
  event.preventDefault();
  if (event.deltaY === 0) return;
  lyricsStore.adjustFont(event.deltaY < 0 ? 0.1 : -0.1);
}

function resetAutoFollow(): void {
  window.clearTimeout(autoFollowTimer);
  window.clearTimeout(suppressClickTimer);
  cancelLyricFollowAnimation();
  clearPendingSeekAutoFollowSnap();
  cancelPendingManualScrollFrame();
  detachManualScrollListeners();
  if (scrollPointerCaptured && scrollPointerId !== null) {
    try {
      lyricsScrollElement.value?.releasePointerCapture?.(scrollPointerId);
    } catch {
      // The element can lose capture when the lyrics view is closed mid-drag.
    }
  }
  autoFollowTimer = 0;
  suppressClickTimer = 0;
  autoFollowPaused = false;
  lastAutoFollowLineIndex = null;
  scrollPointerId = null;
  scrollPointerMoved = false;
  scrollPointerCaptured = false;
  suppressLyricClick = false;
  draggingLyrics.value = false;
}

function beginManualScroll(event: PointerEvent): void {
  const scroll = lyricsScrollElement.value;
  if (event.button !== 0 || !scroll || scrollPointerId !== null) return;
  scrollPointerId = event.pointerId;
  scrollPointerStartY = event.clientY;
  scrollPointerStartTop = scroll.scrollTop;
  scrollPointerMoved = false;
  scrollPointerCaptured = false;
  attachManualScrollListeners();
}

function trackManualScroll(event: PointerEvent): void {
  const scroll = lyricsScrollElement.value;
  if (!scroll || scrollPointerId !== event.pointerId) return;
  const delta = event.clientY - scrollPointerStartY;
  if (!scrollPointerMoved && Math.abs(delta) < DRAG_THRESHOLD_PX) return;
  if (!scrollPointerMoved) {
    scrollPointerMoved = true;
    if (typeof scroll.setPointerCapture === "function") {
      scroll.setPointerCapture(event.pointerId);
      scrollPointerCaptured = true;
    }
  }
  draggingLyrics.value = true;
  scheduleManualScroll(scrollPointerStartTop - delta);
  pauseAutoFollow();
  event.preventDefault();
}

function endManualScroll(event: PointerEvent): void {
  if (scrollPointerId !== event.pointerId) return;
  if (scrollPointerCaptured) lyricsScrollElement.value?.releasePointerCapture?.(event.pointerId);
  cancelPendingManualScrollFrame();
  if (scrollPointerMoved) {
    pauseAutoFollow();
    suppressLyricClick = true;
    window.clearTimeout(suppressClickTimer);
    suppressClickTimer = window.setTimeout(() => {
      suppressClickTimer = 0;
      suppressLyricClick = false;
    });
  }
  scrollPointerId = null;
  scrollPointerMoved = false;
  scrollPointerCaptured = false;
  draggingLyrics.value = false;
  detachManualScrollListeners();
}

function attachManualScrollListeners(): void {
  if (manualScrollListenersAttached) return;
  manualScrollListenersAttached = true;
  window.addEventListener("pointermove", trackManualScroll);
  window.addEventListener("pointerup", endManualScroll);
  window.addEventListener("pointercancel", endManualScroll);
}

function detachManualScrollListeners(): void {
  if (!manualScrollListenersAttached) return;
  manualScrollListenersAttached = false;
  window.removeEventListener("pointermove", trackManualScroll);
  window.removeEventListener("pointerup", endManualScroll);
  window.removeEventListener("pointercancel", endManualScroll);
}

function scheduleManualScroll(scrollTop: number): void {
  pendingManualScrollTop = scrollTop;
  if (manualScrollFrame !== null) return;
  manualScrollFrame = window.requestAnimationFrame(() => {
    manualScrollFrame = null;
    const nextScrollTop = pendingManualScrollTop;
    pendingManualScrollTop = null;
    const scroll = lyricsScrollElement.value;
    if (!scroll || scrollPointerId === null || !scrollPointerMoved || nextScrollTop === null) return;
    scroll.scrollTop = nextScrollTop;
  });
}

function cancelPendingManualScrollFrame(): void {
  if (manualScrollFrame !== null) window.cancelAnimationFrame(manualScrollFrame);
  manualScrollFrame = null;
  pendingManualScrollTop = null;
}

onMounted(() => {
  window.addEventListener("keydown", closeOnEscape);
  window.addEventListener("pointerdown", closeLyricsMenuFromOutside);
  window.addEventListener("resize", closeLyricsMenuOnResize);
});
onBeforeUnmount(() => {
  lyricsStore.flushPreferences();
  window.removeEventListener("keydown", closeOnEscape);
  window.removeEventListener("pointerdown", closeLyricsMenuFromOutside);
  window.removeEventListener("resize", closeLyricsMenuOnResize);
  resetAutoFollow();
  clearOutgoingHighlightTimers();
  previouslyFocused?.focus();
});

const AUTO_FOLLOW_RESUME_MS = 4_000;
const DRAG_THRESHOLD_PX = 4;
const DENSE_LYRIC_INTERVAL_SECONDS = 0.45;
const LYRIC_FOLLOW_FRAME_INTERVAL_MS = 16;
const LYRIC_FOLLOW_RESPONSE_MS = 180;
const LYRIC_FOLLOW_SETTLE_EPSILON_PX = 0.5;
const LYRIC_OUTGOING_HIGHLIGHT_DURATION_MS = 240;
const SEEK_AUTO_FOLLOW_SNAP_EXPIRY_MS = 1_000;

function monotonicNow(): number {
  const timestamp = typeof performance !== "undefined" ? performance.now() : Date.now();
  return Number.isFinite(timestamp) ? timestamp : Date.now();
}
</script>

<template>
  <Transition name="lyrics-view">
    <section v-if="player.lyricsOpen && player.currentTrack" ref="viewElement" class="lyrics-view" role="dialog" aria-modal="true" aria-label="歌词全屏视图" tabindex="-1" @keydown="trapFocus">
      <div class="lyrics-titlebar-drag-region" data-tauri-drag-region aria-hidden="true" @dblclick="toggleMaximizeWindow"></div>
      <div class="lyrics-stage">
        <div class="lyrics-album">
          <ArtworkImage :src="player.currentTrack.coverUrl" :alt="`${player.currentTrack.title}封面`" kind="track" loading="eager" />
          <p>正在播放</p>
          <h2>{{ player.currentTrack.title }}</h2>
          <span>{{ player.currentTrack.artist }} · {{ player.currentTrack.album || "未知专辑" }}</span>
        </div>

        <div v-if="lyricsStore.loading" class="lyrics-loading" role="status" aria-label="正在加载歌词"><span></span><span></span><span></span><span></span></div>
        <div
          v-else-if="lyricsStore.lyrics?.lines.length"
          ref="lyricsScrollElement"
          class="lyrics-scroll"
          :class="{ plain: !lyricsStore.lyrics.synchronized, dragging: draggingLyrics }"
          :style="{
            '--lyric-scale': lyricsStore.fontScale,
            '--playback-lyric-text-dark': lyricsStore.colors.dark.textColor,
            '--playback-lyric-highlight-dark': lyricsStore.colors.dark.highlightColor,
            '--playback-lyric-text-light': lyricsStore.colors.light.textColor,
            '--playback-lyric-highlight-light': lyricsStore.colors.light.highlightColor,
          }"
          tabindex="0"
          aria-label="歌词，右键可调整显示设置"
          @wheel="handleLyricsWheel"
          @pointerdown="beginManualScroll"
          @contextmenu.capture.prevent="openLyricsMenu"
        >
          <button
            v-for="(line, index) in lyricsStore.lyrics.lines"
            :key="`${lyricsStore.lyrics.trackId}-${line.time ?? 'plain'}-${index}`"
            v-memo="[line, lyricsStore.showTranslation, lyricsStore.lyrics.timing, lyricsStore.offset]"
            :ref="(element) => setLineElement(index, element)"
            type="button"
            class="lyric-line"
            :disabled="line.time === null"
            :aria-label="line.time === null ? line.text : `${line.text}，跳转到${formatTime(line.time)}`"
            @click="seek(line, $event)"
          >
            <LyricsLineContent
              :active-state="activeStateForLine(index)"
              :outgoing-state="outgoingStateForLine(index)"
              :line="line"
              :offset="lyricsStore.offset"
              :show-translation="lyricsStore.showTranslation"
              :timing="lyricsStore.lyrics.timing"
            />
          </button>
        </div>
        <div v-else class="lyrics-empty" role="status"><Languages :size="30" /><p>{{ lyricsStore.error || "这首歌曲暂无歌词" }}</p></div>
      </div>

      <div
        v-if="lyricsMenu.open"
        ref="lyricsMenuElement"
        class="lyrics-context-menu"
        role="menu"
        aria-label="歌词字号"
        :style="{ left: `${lyricsMenu.x}px`, top: `${lyricsMenu.y}px` }"
        @contextmenu.prevent
        @keydown.esc.stop.prevent="closeLyricsMenu()"
      >
        <span>歌词显示</span>
        <div class="lyrics-menu-row">
          <span>字号</span>
          <div class="lyrics-size-control">
            <button type="button" title="减小歌词" aria-label="减小歌词" :disabled="lyricsStore.fontScale <= 0.85" @click="lyricsStore.adjustFont(-0.1)"><Minus :size="16" /></button>
            <strong>{{ Math.round(lyricsStore.fontScale * 100) }}%</strong>
            <button type="button" title="增大歌词" aria-label="增大歌词" :disabled="lyricsStore.fontScale >= 1.25" @click="lyricsStore.adjustFont(0.1)"><Plus :size="16" /></button>
          </div>
        </div>
        <div class="lyrics-menu-row">
          <span>时间偏移</span>
          <div class="lyrics-size-control">
            <button type="button" title="歌词提前" aria-label="歌词提前0.1秒" :disabled="lyricsStore.offset <= -5" @click="lyricsStore.adjustOffset(-0.1)"><Minus :size="16" /></button>
            <strong>{{ lyricsStore.offset > 0 ? "+" : "" }}{{ lyricsStore.offset.toFixed(1) }}s</strong>
            <button type="button" title="歌词延后" aria-label="歌词延后0.1秒" :disabled="lyricsStore.offset >= 5" @click="lyricsStore.adjustOffset(0.1)"><Plus :size="16" /></button>
          </div>
        </div>
        <button type="button" class="lyrics-menu-action" role="menuitemcheckbox" :aria-checked="lyricsStore.showTranslation" @click="lyricsStore.setTranslationVisible(!lyricsStore.showTranslation)"><Languages :size="16" /><span>显示翻译</span><strong>{{ lyricsStore.showTranslation ? "开" : "关" }}</strong></button>
        <button type="button" class="lyrics-menu-action" role="menuitem" @click="lyricsStore.resetOffset"><RotateCcw :size="16" /><span>重置时间偏移</span></button>
      </div>

      <LyricsPlayerControls @favorite="emit('favorite', $event)" />
    </section>
  </Transition>
</template>
