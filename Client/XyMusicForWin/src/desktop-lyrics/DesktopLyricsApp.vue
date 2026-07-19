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

const props = defineProps<{
  bridge?: DesktopLyricsBridge;
  initialState?: DesktopLyricsStatePayload | null;
}>();

const state = ref<DesktopLyricsStatePayload | null>(props.initialState ?? null);
const clock = ref<DesktopLyricsClockPayload | null>(props.initialState ? clockFromState(props.initialState) : null);
const nowMs = ref(Date.now());
const optimisticLocked = ref(false);
const optimisticFontScale = ref<number | null>(null);
const bridge = props.bridge ?? createDesktopLyricsBridge();
const unlisteners: DesktopLyricsUnlisten[] = [];
let disposed = false;
let animationFrame = 0;
let previousAnimationTime = 0;
let stateRevision = -1;

const locked = computed(() => Boolean(state.value?.locked || optimisticLocked.value));
const isPlaying = computed(() => Boolean(clock.value?.isPlaying ?? state.value?.isPlaying));
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
const currentLineProgress = computed(() => {
  const current = currentLine.value;
  if (!current?.started) return 0;
  return state.value?.wordLyricsEnabled ? current.progress : 1;
});
const emptyMessage = computed(() => {
  if (!state.value?.track) return "等待播放";
  return state.value.lyrics?.lines.length ? "等待歌词" : "暂无歌词";
});

function applyState(payload: DesktopLyricsStatePayload): void {
  if (payload.version !== DESKTOP_LYRICS_PROTOCOL_VERSION) return;
  const incomingRevision = Number.isFinite(payload.revision) ? payload.revision! : 0;
  if (incomingRevision < stateRevision) return;
  stateRevision = incomingRevision;
  state.value = payload;
  const stateClock = clockFromState(payload);
  if (!clock.value || clock.value.trackId !== stateClock.trackId || stateClock.anchoredAtMs >= clock.value.anchoredAtMs) {
    clock.value = stateClock;
  }
  optimisticLocked.value = false;
  optimisticFontScale.value = null;
  reanchorAnimation();
}

function applyClock(payload: DesktopLyricsClockPayload): void {
  if (payload.version !== DESKTOP_LYRICS_PROTOCOL_VERSION) return;
  const expectedTrackId = state.value?.track?.id ?? state.value?.lyrics?.trackId ?? null;
  if (payload.trackId !== expectedTrackId) return;
  clock.value = payload;
  reanchorAnimation();
}

function reanchorAnimation(): void {
  nowMs.value = Date.now();
  previousAnimationTime = 0;
}

function sendAction(action: DesktopLyricsActionPayload): void {
  void Promise.resolve().then(() => bridge.emitAction(action)).catch(() => undefined);
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

function progressStyle(progress: number): Record<string, string> {
  return { "--desktop-lyric-progress": `${Math.round(clamp(progress, 0, 1) * 10_000) / 100}%` };
}

function updateAnimationFrame(timestamp: number): void {
  animationFrame = 0;
  if (!isPlaying.value) return;
  if (!previousAnimationTime || timestamp - previousAnimationTime >= FRAME_INTERVAL_MS) {
    previousAnimationTime = timestamp;
    nowMs.value = Date.now();
  }
  animationFrame = window.requestAnimationFrame(updateAnimationFrame);
}

function synchronizeAnimationLoop(playing: boolean): void {
  if (animationFrame) {
    window.cancelAnimationFrame(animationFrame);
    animationFrame = 0;
  }
  if (playing) animationFrame = window.requestAnimationFrame(updateAnimationFrame);
  else reanchorAnimation();
}

watch(isPlaying, synchronizeAnimationLoop);

onMounted(async () => {
  const listenerResults = await Promise.allSettled([
    bridge.onState(applyState),
    bridge.onClock(applyClock),
  ]);
  const listeners = listenerResults.flatMap((result) => result.status === "fulfilled" ? [result.value] : []);
  if (disposed) {
    listeners.forEach((unlisten) => unlisten());
    return;
  }
  unlisteners.push(...listeners);
  synchronizeAnimationLoop(isPlaying.value);
  sendAction(createDesktopLyricsAction("ready"));
});

onBeforeUnmount(() => {
  disposed = true;
  if (animationFrame) window.cancelAnimationFrame(animationFrame);
  unlisteners.splice(0).forEach((unlisten) => unlisten());
});

function clamp(value: number, minimum: number, maximum: number): number {
  if (!Number.isFinite(value)) return minimum;
  return Math.max(minimum, Math.min(maximum, value));
}

const FRAME_INTERVAL_MS = 1_000 / 30;
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
            <span
              class="desktop-lyric-fill"
              :style="progressStyle(currentLineProgress)"
              data-tauri-drag-region
            ><span class="desktop-lyric-base" data-tauri-drag-region>{{ currentLine.line.text }}</span><span class="desktop-lyric-progress" data-tauri-drag-region aria-hidden="true">{{ currentLine.line.text }}</span></span>
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
