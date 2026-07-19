<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { Languages, Minus, Plus, RotateCcw } from "@lucide/vue";
import type { LyricLine, Track } from "../../domain/music";
import { resolveLyricPlaybackPosition } from "../../domain/lyricsTimeline";
import { useApplicationServices } from "../services";
import { useLyricsStore } from "../stores/lyricsStore";
import { usePlayerStore } from "../stores/playerStore";
import LyricsPlayerControls from "./LyricsPlayerControls.vue";
import ArtworkImage from "./ui/ArtworkImage.vue";

const player = usePlayerStore();
const emit = defineEmits<{ favorite: [track: Track] }>();
const lyricsStore = useLyricsStore();
const desktopWindow = useApplicationServices().desktopWindow;
const viewElement = ref<HTMLElement | null>(null);
const lyricsScrollElement = ref<HTMLElement | null>(null);
const lineElements = ref<HTMLElement[]>([]);
const lyricsMenuElement = ref<HTMLElement | null>(null);
const lyricsMenu = ref({ open: false, x: 0, y: 0 });
const activeIndex = ref(-1);
const activeLineProgress = ref(0);
const draggingLyrics = ref(false);
let previouslyFocused: HTMLElement | null = null;
let lyricsMenuReturnFocus: HTMLElement | null = null;
let autoFollowPaused = false;
let autoFollowTimer = 0;
let scrollPointerId: number | null = null;
let scrollPointerStartY = 0;
let scrollPointerStartTop = 0;
let scrollPointerMoved = false;
let scrollPointerCaptured = false;
let suppressLyricClick = false;
let suppressClickTimer = 0;
const focusableSelector = [
  "button:not([disabled])",
  "input:not([disabled])",
  "select:not([disabled])",
  "textarea:not([disabled])",
  "[href]",
  "[tabindex]:not([tabindex='-1'])",
].join(",");

function toggleMaximizeWindow(): void {
  void desktopWindow.toggleMaximize().catch(() => undefined);
}

function updateActivePosition(): void {
  const playbackTime = player.currentTime + lyricsStore.offset;
  const position = resolveLyricPlaybackPosition(lyricsStore.lyrics, playbackTime);
  activeIndex.value = position.lineIndex;
  activeLineProgress.value = position.lineProgress;
}

function lineProgressStyle(lineIndex: number): Record<string, string> {
  const progress = lineIndex === activeIndex.value
    ? (lyricsStore.wordLyricsEnabled ? activeLineProgress.value : 1)
    : 0;
  return { "--lyric-line-progress": `${Math.round(progress * 10_000) / 100}%` };
}

watch(
  () => player.lyricsOpen
    ? [player.currentTime, lyricsStore.offset, lyricsStore.lyrics] as const
    : null,
  (visiblePlayback) => {
    if (visiblePlayback) updateActivePosition();
  },
  { immediate: true },
);

async function scrollToActiveLine(index = activeIndex.value): Promise<void> {
  if (!player.lyricsOpen || autoFollowPaused || index < 0) return;
  await nextTick();
  if (!player.lyricsOpen || autoFollowPaused) return;
  const reducedMotion = window.matchMedia?.("(prefers-reduced-motion: reduce)").matches ?? false;
  lineElements.value[index]?.scrollIntoView({ behavior: reducedMotion ? "auto" : "smooth", block: "center" });
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
  if (line.time !== null) player.seekTo(line.time - lyricsStore.offset);
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
  autoFollowTimer = 0;
  suppressClickTimer = 0;
  autoFollowPaused = false;
  scrollPointerId = null;
  scrollPointerMoved = false;
  scrollPointerCaptured = false;
  suppressLyricClick = false;
  draggingLyrics.value = false;
}

function beginManualScroll(event: PointerEvent): void {
  const scroll = lyricsScrollElement.value;
  if (event.button !== 0 || !scroll) return;
  scrollPointerId = event.pointerId;
  scrollPointerStartY = event.clientY;
  scrollPointerStartTop = scroll.scrollTop;
  scrollPointerMoved = false;
  scrollPointerCaptured = false;
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
  scroll.scrollTop = scrollPointerStartTop - delta;
  pauseAutoFollow();
  event.preventDefault();
}

function endManualScroll(event: PointerEvent): void {
  if (scrollPointerId !== event.pointerId) return;
  if (scrollPointerCaptured) lyricsScrollElement.value?.releasePointerCapture?.(event.pointerId);
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
}

onMounted(() => {
  window.addEventListener("keydown", closeOnEscape);
  window.addEventListener("pointerdown", closeLyricsMenuFromOutside);
  window.addEventListener("pointermove", trackManualScroll);
  window.addEventListener("pointerup", endManualScroll);
  window.addEventListener("pointercancel", endManualScroll);
  window.addEventListener("resize", closeLyricsMenuOnResize);
});
onBeforeUnmount(() => {
  window.removeEventListener("keydown", closeOnEscape);
  window.removeEventListener("pointerdown", closeLyricsMenuFromOutside);
  window.removeEventListener("pointermove", trackManualScroll);
  window.removeEventListener("pointerup", endManualScroll);
  window.removeEventListener("pointercancel", endManualScroll);
  window.removeEventListener("resize", closeLyricsMenuOnResize);
  resetAutoFollow();
  previouslyFocused?.focus();
});

const AUTO_FOLLOW_RESUME_MS = 4_000;
const DRAG_THRESHOLD_PX = 4;
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
            v-memo="[line, lyricsStore.showTranslation, lyricsStore.wordLyricsEnabled, index === activeIndex, index === activeIndex ? activeLineProgress : -1]"
            ref="lineElements"
            type="button"
            class="lyric-line"
            :class="{ active: index === activeIndex }"
            :disabled="line.time === null"
            :aria-current="index === activeIndex ? 'true' : undefined"
            :aria-label="line.time === null ? line.text : `${line.text}，跳转到${formatTime(line.time)}`"
            @click="seek(line, $event)"
          >
            <strong>
              <span
                class="lyric-line-text"
                :style="lineProgressStyle(index)"
              ><span class="lyric-line-base">{{ line.text }}</span><span class="lyric-line-fill" aria-hidden="true">{{ line.text }}</span></span>
            </strong>
            <span v-if="lyricsStore.showTranslation && line.translation" class="lyric-translation">{{ line.translation }}</span>
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
