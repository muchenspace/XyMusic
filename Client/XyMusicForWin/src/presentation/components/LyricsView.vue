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
import {
  LYRIC_BASELINE_MAX_FRAME_COUNT,
  LYRIC_LAYOUT_STABILITY_EPSILON_PX,
  LYRIC_REQUIRED_STABLE_FRAMES,
  LYRIC_SEEK_ACK_TIMEOUT_MS,
  canonicalLyricTargetIndex,
  correctionDurationMs,
  fastOutSlowIn,
  lyricLayoutDeltaHasSettled,
  lyricLineTransitionEmphasis,
  lyricSeekBaselineIndex,
  lyricTransitionDurationMs,
  lyricTransitionMode,
  noBounceSpring,
  retargetLyricEmphasis,
  settledLyricEmphasis,
  type LyricEmphasisTransition,
  type LyricTransitionMode,
} from "./lyricsTransition";

const player = usePlayerStore();
const props = withDefaults(defineProps<{ fullscreen?: boolean }>(), { fullscreen: false });
const emit = defineEmits<{ favorite: [track: Track] }>();
const lyricsStore = useLyricsStore();
const lineTransitionActive = ref(false);
const displayedPlaybackTime = useSmoothLyricsPlaybackPosition({
  currentTime: () => player.currentTime,
  isPlaying: () => player.isPlaying && !player.loading,
  isActive: () => player.lyricsOpen,
  discontinuityToken: () => `${player.positionDiscontinuityVersion}:${player.currentTrack?.id ?? ""}`,
  renderPlan: (positionSeconds) => {
    const plan = resolveLyricPlaybackRenderPlan(
      lyricsStore.lyrics,
      positionSeconds + lyricsStore.offset,
    );
    return {
      ...plan,
      requiresAnimationFrame: plan.requiresAnimationFrame || lineTransitionActive.value,
      nextChangeAtSeconds: plan.nextChangeAtSeconds === null
        ? null
        : plan.nextChangeAtSeconds - lyricsStore.offset,
    };
  },
  renderPlanDependencies: () => [lyricsStore.lyrics, lyricsStore.offset, lineTransitionActive.value],
});
provide(lyricsPlaybackPositionKey, displayedPlaybackTime);
const windowControls = useDesktopWindowStore();
const viewElement = ref<HTMLElement | null>(null);
const lyricsScrollElement = ref<HTMLElement | null>(null);
const lineElements = ref<Array<HTMLElement | undefined>>([]);
const lineActiveStates: Ref<boolean>[] = [];
const lineOutgoingStates: Ref<boolean>[] = [];
const lineEmphasisStates: Ref<number>[] = [];
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
let lyricLayoutStabilizationFrame: number | null = null;
let lyricLayoutStabilizationGeneration = 0;
let lyricLayoutStabilization: LyricLayoutStabilization | null = null;
let pendingSeekAutoFollowSnap = false;
let pendingLyricSeek: PendingLyricSeek | null = null;
let pendingLyricSeekTimer = 0;
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
let observedLineIndex = -1;
let activeLineIndex = -1;
let activeLyrics = lyricsStore.lyrics;
let emphasisTransition: LyricEmphasisTransition = settledLyricEmphasis(-1);
let emphasisPhase = 0;
let animatedLinePosition = -1;
let settledLinePosition = -1;
let transitionPhaseVelocity = 0;
let transitionPreviousProgress = 0;
let sharedTransition: SharedLineTransition | null = null;
let alignmentCorrection: AlignmentCorrection | null = null;
const emphasizedLineIndices = new Set<number>();

interface SharedLineTransition {
  generation: number;
  startedAt: number | null;
  lastFrameAt: number | null;
  durationMs: number;
  startEmphasisPhase: number;
  targetEmphasisPhase: number;
  startLinePosition: number;
  targetLineIndex: number;
  startScrollTop: number;
  targetScrollTop: number;
  preserveVelocity: boolean;
  initialPhaseVelocity: number;
}

interface AlignmentCorrection {
  generation: number;
  targetLineIndex: number;
  startedAt: number | null;
  durationMs: number;
  startScrollTop: number;
  targetScrollTop: number;
  pass: number;
}

interface LyricLayoutStabilization {
  generation: number;
  targetLineIndex: number;
  frameCount: number;
  stableFrameCount: number;
  previousDelta: number | null;
}

interface PendingLyricSeek {
  sourceIndex: number;
  targetIndex: number;
  requestedAtMs: number;
}

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
    resetLineTransitionState();
    lineElements.value = [];
    lineActiveStates.length = 0;
    lineOutgoingStates.length = 0;
    lineEmphasisStates.length = 0;
    observedLineIndex = -1;
    activeLineIndex = -1;
    activeLyrics = lyrics;
  }
  const playbackTime = displayedPlaybackTime.value + lyricsStore.offset;
  const nextObservedLineIndex = resolveLyricPlaybackPosition(lyrics, playbackTime).lineIndex;
  observedLineIndex = nextObservedLineIndex;
  if (lyricLayoutStabilization) return;
  const hadPendingSeek = pendingLyricSeek !== null;
  const visualIndex = visualIndexAfterPendingSeek(nextObservedLineIndex);
  if (visualIndex === null) return;
  const activeIndexChanged = commitActiveLineIndex(visualIndex);
  if (hadPendingSeek && !pendingLyricSeek && pendingSeekAutoFollowSnap && !activeIndexChanged) {
    void scrollToActiveLine(visualIndex);
  }
  if (visualIndex !== nextObservedLineIndex && !pendingLyricSeek && !lyricLayoutStabilization) {
    void nextTick(() => {
      if (!pendingLyricSeek && !lyricLayoutStabilization && observedLineIndex === nextObservedLineIndex) {
        commitActiveLineIndex(nextObservedLineIndex);
      }
    });
  }
}

function commitActiveLineIndex(nextActiveLineIndex: number): boolean {
  const previousActiveLineIndex = activeLineIndex;
  if (nextActiveLineIndex !== previousActiveLineIndex) {
    activeLineIndex = nextActiveLineIndex;
    if (previousActiveLineIndex >= 0) {
      setLineActive(previousActiveLineIndex, false);
      setLineOutgoing(previousActiveLineIndex, true);
    }
    setLineOutgoing(nextActiveLineIndex, false);
    setLineActive(nextActiveLineIndex, true);
    setLineElementActivity(lineElements.value[previousActiveLineIndex], false);
    setLineElementActivity(lineElements.value[nextActiveLineIndex], true);
  }
  if (activeIndex.value === nextActiveLineIndex) return false;
  activeIndex.value = nextActiveLineIndex;
  return true;
}

function visualIndexAfterPendingSeek(currentIndex: number): number | null {
  const request = pendingLyricSeek;
  if (!request) return currentIndex;
  const baselineIndex = lyricSeekBaselineIndex(request.sourceIndex, request.targetIndex, currentIndex);
  if (baselineIndex !== null) {
    clearPendingLyricSeek();
    if (!autoFollowPaused) prepareLyricLayoutStabilization(baselineIndex);
    pendingSeekAutoFollowSnap = true;
    return baselineIndex;
  }
  if (monotonicNow() - request.requestedAtMs < LYRIC_SEEK_ACK_TIMEOUT_MS) return null;
  clearPendingLyricSeek();
  return currentIndex;
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

function emphasisStateForLine(index: number): Readonly<Ref<number>> {
  let emphasisState = lineEmphasisStates[index];
  if (!emphasisState) {
    emphasisState = ref(lyricLineTransitionEmphasis(emphasisPhase, index, emphasisTransition));
    lineEmphasisStates[index] = emphasisState;
  }
  return emphasisState;
}

function setLineElement(index: number, element: unknown): void {
  const lineElement = element instanceof HTMLElement ? element : undefined;
  lineElements.value[index] = lineElement;
  if (lineElement) {
    setLineElementActivity(lineElement, index === activeLineIndex);
    applyLineElementEmphasis(lineElement, emphasisStateForLine(index).value);
  }
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
}

function setLineElementActivity(element: HTMLElement | undefined, active: boolean): void {
  if (!element) return;
  element.classList.toggle("active", active);
  if (active) element.setAttribute("aria-current", "true");
  else element.removeAttribute("aria-current");
}

function setLineEmphasis(index: number, emphasis: number): void {
  if (index < 0) return;
  const value = Math.max(0, Math.min(1, Number.isFinite(emphasis) ? emphasis : 0));
  let state = lineEmphasisStates[index];
  if (!state) {
    state = ref(value);
    lineEmphasisStates[index] = state;
  } else if (Math.abs(state.value - value) > 0.0001) {
    state.value = value;
  }
  const element = lineElements.value[index];
  if (element) applyLineElementEmphasis(element, value);
  if (value > 1 / 1_024) {
    emphasizedLineIndices.add(index);
    if (index !== activeLineIndex) setLineOutgoing(index, true);
  } else {
    emphasizedLineIndices.delete(index);
    if (index !== activeLineIndex) setLineOutgoing(index, false);
  }
}

function applyLineElementEmphasis(element: HTMLElement, emphasis: number): void {
  element.style.setProperty("--lyric-line-emphasis", String(emphasis));
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
  if (!player.lyricsOpen || index < 0) return;
  await nextTick();
  if (!player.lyricsOpen || index !== activeIndex.value) return;
  const stabilization = lyricLayoutStabilization;
  const isSeekBaselineSnap = pendingSeekAutoFollowSnap
    && stabilization?.targetLineIndex === index;
  if (stabilization && !isSeekBaselineSnap) return;
  const scroll = lyricsScrollElement.value;
  const lineElement = lineElements.value[index];
  if (!scroll || !lineElement) {
    snapLineTransition(index, null);
    if (isSeekBaselineSnap && !autoFollowPaused) {
      clearPendingSeekAutoFollowSnap();
      startLyricLayoutStabilization(stabilization);
    }
    return;
  }
  const reducedMotion = window.matchMedia?.("(prefers-reduced-motion: reduce)").matches ?? false;
  const previousIndex = lastAutoFollowLineIndex;
  const previousTime = previousIndex === null ? null : lyricsStore.lyrics?.lines[previousIndex]?.time ?? null;
  const nextTime = lyricsStore.lyrics?.lines[index]?.time ?? null;
  const mode: LyricTransitionMode = reducedMotion || pendingSeekAutoFollowSnap
    ? "snap"
    : lyricTransitionMode(previousIndex, index, previousTime, nextTime);
  const targetTop = centeredLyricScrollTop(scroll, lineElement);
  clearPendingSeekAutoFollowSnap();
  if (mode === "snap") {
    snapLineTransition(index, autoFollowPaused ? null : targetTop);
    if (isSeekBaselineSnap && !autoFollowPaused) startLyricLayoutStabilization(stabilization);
  } else {
    scheduleLyricLineTransition(index, autoFollowPaused ? null : targetTop);
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

function snapLineTransition(index: number, targetTop: number | null): void {
  cancelLyricFollowFrameOnly();
  sharedTransition = null;
  alignmentCorrection = null;
  emphasisTransition = settledLyricEmphasis(index);
  emphasisPhase = 0;
  animatedLinePosition = index;
  settledLinePosition = index;
  transitionPhaseVelocity = 0;
  transitionPreviousProgress = 0;
  if (targetTop !== null && lyricsScrollElement.value) lyricsScrollElement.value.scrollTop = targetTop;
  applyTransitionEmphasis(index);
  refreshLineTransitionActivity();
}

function scheduleLyricLineTransition(index: number, targetTop: number | null): void {
  if (lyricLayoutStabilization) return;
  const scroll = lyricsScrollElement.value;
  if (!scroll) {
    snapLineTransition(index, null);
    return;
  }
  lineTransitionActive.value = true;
  const startLinePosition = Number.isFinite(animatedLinePosition)
    ? animatedLinePosition
    : index;
  const wasInterrupted = sharedTransition !== null
    || Math.abs(settledLinePosition - startLinePosition) > 0.01;
  emphasisTransition = retargetLyricEmphasis(emphasisTransition, emphasisPhase, index);
  const startScrollTop = scroll.scrollTop;
  const resolvedTargetTop = targetTop ?? startScrollTop;
  const durationMs = lyricTransitionDurationMs(index - startLinePosition, resolvedTargetTop - startScrollTop);
  const generation = ++lyricFollowAnimationGeneration;
  cancelLyricFollowFrameOnly();
  alignmentCorrection = null;
  sharedTransition = {
    generation,
    startedAt: null,
    lastFrameAt: null,
    durationMs,
    startEmphasisPhase: emphasisTransition.startPhase,
    targetEmphasisPhase: emphasisTransition.endPhase,
    startLinePosition,
    targetLineIndex: index,
    startScrollTop,
    targetScrollTop: resolvedTargetTop,
    preserveVelocity: wasInterrupted,
    initialPhaseVelocity: transitionPhaseVelocity,
  };
  transitionPreviousProgress = 0;
  requestSharedTransitionFrame(generation);
}

function requestSharedTransitionFrame(generation: number): void {
  if (lyricFollowAnimationFrame !== null) return;
  lyricFollowAnimationFrame = requestLyricFollowFrame((timestamp) => runSharedTransitionFrame(generation, timestamp));
}

function runSharedTransitionFrame(generation: number, timestamp: number): void {
  if (generation !== lyricFollowAnimationGeneration) return;
  lyricFollowAnimationFrame = null;
  if (sharedTransition) {
    runLineTransitionFrame(sharedTransition, timestamp);
  } else if (alignmentCorrection) {
    runAlignmentCorrectionFrame(alignmentCorrection, timestamp);
  }
}

function runLineTransitionFrame(transition: SharedLineTransition, timestamp: number): void {
  if (transition.generation !== lyricFollowAnimationGeneration || sharedTransition !== transition) return;
  if (transition.startedAt === null) transition.startedAt = timestamp - LYRIC_FOLLOW_FRAME_INTERVAL_MS;
  const frameDeltaMs = transition.lastFrameAt === null
    ? LYRIC_FOLLOW_FRAME_INTERVAL_MS
    : Math.min(50, Math.max(1, timestamp - transition.lastFrameAt));
  transition.lastFrameAt = timestamp;
  const elapsedMs = Math.max(0, timestamp - transition.startedAt);
  const rawProgress = Math.min(1, elapsedMs / transition.durationMs);
  const easedProgress = transition.preserveVelocity
    ? noBounceSpring(elapsedMs / 1_000, transition.initialPhaseVelocity)
    : fastOutSlowIn(rawProgress);
  const previousProgress = transitionPreviousProgress;
  transitionPreviousProgress = easedProgress;
  transitionPhaseVelocity = Math.max(0, easedProgress - previousProgress)
    / Math.max(frameDeltaMs / 1_000, 0.001);
  emphasisPhase = transition.startEmphasisPhase
    + (transition.targetEmphasisPhase - transition.startEmphasisPhase) * easedProgress;
  animatedLinePosition = transition.startLinePosition
    + (transition.targetLineIndex - transition.startLinePosition) * easedProgress;
  applyTransitionEmphasis(transition.targetLineIndex);

  const scroll = lyricsScrollElement.value;
  if (scroll && !autoFollowPaused) {
    scroll.scrollTop = transition.startScrollTop
      + (transition.targetScrollTop - transition.startScrollTop) * easedProgress;
  }
  const completed = transition.preserveVelocity
    ? (elapsedMs >= 300 && 1 - easedProgress <= 0.001) || elapsedMs >= 1_000
    : rawProgress >= 1;
  if (!completed) {
    requestSharedTransitionFrame(transition.generation);
    return;
  }

  const targetIndex = transition.targetLineIndex;
  sharedTransition = null;
  emphasisTransition = settledLyricEmphasis(targetIndex);
  emphasisPhase = 0;
  animatedLinePosition = targetIndex;
  settledLinePosition = targetIndex;
  transitionPhaseVelocity = 0;
  transitionPreviousProgress = 0;
  applyTransitionEmphasis(targetIndex);
  if (!autoFollowPaused) startAlignmentCorrection(targetIndex, transition.durationMs, transition.generation);
}

function applyTransitionEmphasis(targetIndex: number): void {
  const candidates = new Set<number>([
    ...emphasizedLineIndices,
    ...emphasisTransition.startEmphasis.keys(),
    emphasisTransition.targetLineIndex,
    targetIndex,
  ]);
  candidates.forEach((index) => {
    if (index >= 0) setLineEmphasis(index, lyricLineTransitionEmphasis(emphasisPhase, index, emphasisTransition));
  });
}

function startAlignmentCorrection(targetIndex: number, transitionDuration: number, generation: number, pass = 0): void {
  const scroll = lyricsScrollElement.value;
  const line = lineElements.value[targetIndex];
  if (!scroll || !line || autoFollowPaused || pass >= LYRIC_LAYOUT_CORRECTION_MAX_PASSES) {
    refreshLineTransitionActivity();
    return;
  }
  const targetTop = centeredLyricScrollTop(scroll, line);
  if (Math.abs(targetTop - scroll.scrollTop) <= LYRIC_LAYOUT_STABILITY_EPSILON_PX) {
    scroll.scrollTop = targetTop;
    refreshLineTransitionActivity();
    return;
  }
  alignmentCorrection = {
    generation,
    targetLineIndex: targetIndex,
    startedAt: null,
    durationMs: correctionDurationMs(transitionDuration),
    startScrollTop: scroll.scrollTop,
    targetScrollTop: targetTop,
    pass,
  };
  requestSharedTransitionFrame(generation);
  refreshLineTransitionActivity();
}

function runAlignmentCorrectionFrame(correction: AlignmentCorrection, timestamp: number): void {
  if (correction.generation !== lyricFollowAnimationGeneration || alignmentCorrection !== correction) return;
  const scroll = lyricsScrollElement.value;
  if (!scroll || autoFollowPaused) {
    alignmentCorrection = null;
    refreshLineTransitionActivity();
    return;
  }
  if (correction.startedAt === null) correction.startedAt = timestamp - LYRIC_FOLLOW_FRAME_INTERVAL_MS;
  const progress = Math.min(1, Math.max(0, timestamp - correction.startedAt) / correction.durationMs);
  scroll.scrollTop = correction.startScrollTop
    + (correction.targetScrollTop - correction.startScrollTop) * fastOutSlowIn(progress);
  if (progress < 1) {
    requestSharedTransitionFrame(correction.generation);
    return;
  }
  scroll.scrollTop = correction.targetScrollTop;
  alignmentCorrection = null;
  startAlignmentCorrection(
    correction.targetLineIndex,
    correction.durationMs,
    correction.generation,
    correction.pass + 1,
  );
}

function cancelLyricFollowFrameOnly(): void {
  if (lyricFollowAnimationFrame !== null) {
    cancelLyricFollowFrame(lyricFollowAnimationFrame);
    lyricFollowAnimationFrame = null;
  }
}

function cancelLyricFollowAnimation(): void {
  lyricFollowAnimationGeneration += 1;
  cancelLyricFollowFrameOnly();
  sharedTransition = null;
  alignmentCorrection = null;
  transitionPreviousProgress = 0;
  refreshLineTransitionActivity();
}

function prepareLyricLayoutStabilization(targetLineIndex: number): void {
  cancelLyricLayoutStabilization();
  lyricLayoutStabilization = {
    generation: lyricLayoutStabilizationGeneration,
    targetLineIndex,
    frameCount: 0,
    stableFrameCount: 0,
    previousDelta: null,
  };
  refreshLineTransitionActivity();
}

function startLyricLayoutStabilization(stabilization: LyricLayoutStabilization): void {
  if (lyricLayoutStabilization !== stabilization) return;
  requestLyricLayoutStabilizationFrame(stabilization);
}

function requestLyricLayoutStabilizationFrame(stabilization: LyricLayoutStabilization): void {
  if (lyricLayoutStabilizationFrame !== null || lyricLayoutStabilization !== stabilization) return;
  const generation = stabilization.generation;
  lyricLayoutStabilizationFrame = requestLyricFollowFrame(() => {
    if (
      generation !== lyricLayoutStabilizationGeneration
      || lyricLayoutStabilization !== stabilization
    ) return;
    lyricLayoutStabilizationFrame = null;
    runLyricLayoutStabilizationFrame(stabilization);
  });
}

function runLyricLayoutStabilizationFrame(stabilization: LyricLayoutStabilization): void {
  const currentDelta = centeredLyricDelta(stabilization.targetLineIndex);
  stabilization.frameCount += 1;
  if (lyricLayoutDeltaHasSettled(stabilization.previousDelta, currentDelta)) {
    stabilization.stableFrameCount += 1;
  } else {
    stabilization.stableFrameCount = 0;
  }
  stabilization.previousDelta = currentDelta;
  if (
    stabilization.stableFrameCount >= LYRIC_REQUIRED_STABLE_FRAMES
    || stabilization.frameCount >= LYRIC_BASELINE_MAX_FRAME_COUNT
  ) {
    finishLyricLayoutStabilization(stabilization);
    return;
  }
  requestLyricLayoutStabilizationFrame(stabilization);
}

function centeredLyricDelta(targetLineIndex: number): number | null {
  const scroll = lyricsScrollElement.value;
  const line = lineElements.value[targetLineIndex];
  if (!scroll || !line) return null;
  const scrollRect = scroll.getBoundingClientRect();
  const lineRect = line.getBoundingClientRect();
  const delta = lineRect.top + lineRect.height / 2
    - (scrollRect.top + scroll.clientHeight / 2);
  return Number.isFinite(delta) ? delta : null;
}

function finishLyricLayoutStabilization(stabilization: LyricLayoutStabilization): void {
  if (lyricLayoutStabilization !== stabilization) return;
  lyricLayoutStabilization = null;
  lyricLayoutStabilizationFrame = null;
  refreshLineTransitionActivity();
  updateActivePosition();
}

function cancelLyricLayoutStabilization(): void {
  lyricLayoutStabilizationGeneration += 1;
  if (lyricLayoutStabilizationFrame !== null) {
    cancelLyricFollowFrame(lyricLayoutStabilizationFrame);
    lyricLayoutStabilizationFrame = null;
  }
  lyricLayoutStabilization = null;
  refreshLineTransitionActivity();
}

function refreshLineTransitionActivity(): void {
  lineTransitionActive.value = sharedTransition !== null
    || alignmentCorrection !== null
    || lyricLayoutStabilization !== null;
}

function requestLyricFollowFrame(callback: FrameRequestCallback): number {
  if (typeof window.requestAnimationFrame === "function") return window.requestAnimationFrame(callback);
  return window.setTimeout(() => callback(monotonicNow()), LYRIC_FOLLOW_FRAME_INTERVAL_MS);
}

function cancelLyricFollowFrame(handle: number): void {
  if (typeof window.cancelAnimationFrame === "function") window.cancelAnimationFrame(handle);
  else window.clearTimeout(handle);
}

function markSeekForAutoFollow(targetIndex: number): void {
  cancelLyricLayoutStabilization();
  clearPendingLyricSeek();
  pendingLyricSeek = {
    sourceIndex: activeLineIndex >= 0 ? activeLineIndex : targetIndex,
    targetIndex,
    requestedAtMs: monotonicNow(),
  };
  pendingLyricSeekTimer = window.setTimeout(() => {
    pendingLyricSeekTimer = 0;
    if (!pendingLyricSeek) return;
    pendingLyricSeek = null;
    if (observedLineIndex >= 0) commitActiveLineIndex(observedLineIndex);
  }, LYRIC_SEEK_ACK_TIMEOUT_MS);
}

function clearPendingSeekAutoFollowSnap(): void {
  pendingSeekAutoFollowSnap = false;
}

function clearPendingLyricSeek(): void {
  pendingLyricSeek = null;
  if (pendingLyricSeekTimer !== 0) window.clearTimeout(pendingLyricSeekTimer);
  pendingLyricSeekTimer = 0;
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

function seek(line: LyricLine, index: number, event: MouseEvent) {
  if (suppressLyricClick) {
    event.preventDefault();
    event.stopPropagation();
    return;
  }
  if (line.time !== null) {
    const targetIndex = canonicalLyricTargetIndex(lyricsStore.lyrics?.lines ?? [], index);
    markSeekForAutoFollow(targetIndex);
    autoFollowPaused = false;
    window.clearTimeout(autoFollowTimer);
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
  cancelLyricLayoutStabilization();
  if (alignmentCorrection) {
    alignmentCorrection = null;
    cancelLyricFollowFrameOnly();
  }
  autoFollowTimer = window.setTimeout(() => {
    autoFollowTimer = 0;
    autoFollowPaused = false;
    void snapAutoFollowToActiveLine();
  }, AUTO_FOLLOW_RESUME_MS);
}

async function snapAutoFollowToActiveLine(): Promise<void> {
  const index = activeIndex.value;
  if (!player.lyricsOpen || index < 0) return;
  await nextTick();
  const scroll = lyricsScrollElement.value;
  const line = lineElements.value[index];
  if (scroll && line) scroll.scrollTop = centeredLyricScrollTop(scroll, line);
  lastAutoFollowLineIndex = index;
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
  cancelLyricLayoutStabilization();
  clearPendingSeekAutoFollowSnap();
  clearPendingLyricSeek();
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
  if (activeLineIndex >= 0) snapLineTransition(activeLineIndex, null);
}

function resetLineTransitionState(): void {
  cancelLyricFollowAnimation();
  cancelLyricLayoutStabilization();
  clearPendingLyricSeek();
  clearPendingSeekAutoFollowSnap();
  [...emphasizedLineIndices].forEach((index) => setLineEmphasis(index, 0));
  emphasizedLineIndices.clear();
  lineOutgoingStates.forEach((state) => { state.value = false; });
  emphasisTransition = settledLyricEmphasis(-1);
  emphasisPhase = 0;
  animatedLinePosition = -1;
  settledLinePosition = -1;
  observedLineIndex = -1;
  transitionPhaseVelocity = 0;
  transitionPreviousProgress = 0;
  lastAutoFollowLineIndex = null;
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
  resetLineTransitionState();
  previouslyFocused?.focus();
});

const AUTO_FOLLOW_RESUME_MS = 4_000;
const DRAG_THRESHOLD_PX = 4;
const LYRIC_FOLLOW_FRAME_INTERVAL_MS = 16;
const LYRIC_LAYOUT_CORRECTION_MAX_PASSES = 4;

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
            @click="seek(line, index, $event)"
          >
            <LyricsLineContent
              :active-state="activeStateForLine(index)"
              :outgoing-state="outgoingStateForLine(index)"
              :line-emphasis-state="emphasisStateForLine(index)"
              :line="line"
              :next-line-time="lyricsStore.lyrics.lines[index + 1]?.time"
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
