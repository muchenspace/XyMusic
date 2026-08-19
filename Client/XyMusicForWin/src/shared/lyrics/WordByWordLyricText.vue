<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import type { LyricWord } from "../../domain/music";
import {
  localWordHighlightFragment,
  validateTimedWordText,
  wordHighlightRects,
  type WordHighlightFragment,
} from "./wordByWordLyric";

const props = defineProps<{
  text: string;
  words: readonly LyricWord[];
  progresses: readonly number[];
  containerClass: string;
  highlightColor: string;
}>();

const root = ref<HTMLElement | null>(null);
const fragments = ref<readonly WordHighlightFragment[]>([]);
const bounds = ref({ width: 0, height: 0 });
const layout = computed(() => validateTimedWordText(props.text, props.words));
const clipId = `word-reveal-${Math.random().toString(36).slice(2)}`;
let observer: ResizeObserver | null = null;
let resizeListener: (() => void) | null = null;
let measureQueued = false;

const overlayRects = computed(() => {
  return wordHighlightRects(fragments.value, props.progresses);
});

function queueMeasure(): void {
  if (measureQueued) return;
  measureQueued = true;
  void nextTick(() => {
    measureQueued = false;
    measureTextFragments();
  });
}

function measureTextFragments(): void {
  const element = root.value;
  const validated = layout.value;
  if (!element || !validated || typeof document === "undefined") {
    clearMeasuredLayout();
    return;
  }

  const rootRect = element.getBoundingClientRect();
  const layoutWidth = element.offsetWidth;
  const layoutHeight = element.offsetHeight;
  if (rootRect.width <= 0 || rootRect.height <= 0 || layoutWidth <= 0 || layoutHeight <= 0) {
    clearMeasuredLayout();
    return;
  }
  const nodes: { node: Text; start: number; end: number; rtl: boolean }[] = [];
  const walker = document.createTreeWalker(element, NodeFilter.SHOW_TEXT);
  let offset = 0;
  let node: Node | null;
  while ((node = walker.nextNode())) {
    const textNode = node as Text;
    // The SVG overlay contains a duplicate copy of the line. Exclude it from
    // source-range accounting or the next measurement would count the copy a
    // second time and invalidate every highlight fragment.
    if (textNode.parentElement?.closest(".word-by-word-lyric-overlay")) continue;
    const end = offset + textNode.data.length;
    nodes.push({
      node: textNode,
      start: offset,
      end,
      rtl: getComputedStyle(textNode.parentElement ?? element).direction === "rtl",
    });
    offset = end;
  }
  if (offset !== validated.text.length) {
    clearMeasuredLayout();
    return;
  }

  const nextFragments: WordHighlightFragment[] = [];
  for (const word of validated.words) {
    for (const grapheme of word.graphemes) {
      const target = nodes.find((candidate) => grapheme.startOffset >= candidate.start && grapheme.endOffset <= candidate.end);
      if (!target) {
        clearMeasuredLayout();
        return;
      }
      const range = document.createRange();
      range.setStart(target.node, grapheme.startOffset - target.start);
      range.setEnd(target.node, grapheme.endOffset - target.start);
      if (typeof range.getClientRects !== "function") {
        clearMeasuredLayout();
        return;
      }
      for (const rect of Array.from(range.getClientRects())) {
        if (rect.width > 0 && rect.height > 0) {
          nextFragments.push(localWordHighlightFragment(
            word.wordIndex,
            rect,
            rootRect,
            layoutWidth,
            layoutHeight,
            target.rtl,
          ));
        }
      }
    }
  }
  fragments.value = nextFragments;
  bounds.value = { width: layoutWidth, height: layoutHeight };
}

function clearMeasuredLayout(): void {
  if (fragments.value.length) fragments.value = [];
  if (bounds.value.width !== 0 || bounds.value.height !== 0) bounds.value = { width: 0, height: 0 };
}

watch(() => [props.text, props.words], queueMeasure, { deep: true });
onMounted(() => {
  queueMeasure();
  if (typeof ResizeObserver !== "undefined" && root.value) {
    observer = new ResizeObserver(queueMeasure);
    observer.observe(root.value);
  } else if (typeof window !== "undefined") {
    resizeListener = queueMeasure;
    window.addEventListener("resize", resizeListener);
  }
  void document.fonts?.ready.then(queueMeasure);
});
onBeforeUnmount(() => {
  observer?.disconnect();
  if (resizeListener) window.removeEventListener("resize", resizeListener);
});
</script>

<template>
  <span
    ref="root"
    :class="[containerClass, 'word-by-word-lyric-text']"
    :style="{ '--word-reveal-highlight': highlightColor }"
  >
    <template v-if="layout">
      <slot />
    </template>
    <span v-else class="word-by-word-lyric-fallback">{{ text }}</span>
    <svg
      v-if="layout && overlayRects.length"
      class="word-by-word-lyric-overlay"
      aria-hidden="true"
      focusable="false"
      :viewBox="`0 0 ${bounds.width} ${bounds.height}`"
      preserveAspectRatio="none"
    >
      <defs>
        <clipPath :id="clipId" clipPathUnits="userSpaceOnUse">
          <rect v-for="(rect, index) in overlayRects" :key="index" v-bind="rect" />
        </clipPath>
      </defs>
      <foreignObject width="100%" height="100%" :clip-path="`url(#${clipId})`">
        <div xmlns="http://www.w3.org/1999/xhtml" class="word-by-word-lyric-overlay-copy">
          <span
            v-for="(word, wordIndex) in words"
            :key="`${word.time}-${wordIndex}`"
            class="word-by-word-lyric-overlay-word"
            dir="auto"
          >{{ word.text }}</span>
        </div>
      </foreignObject>
    </svg>
  </span>
</template>
