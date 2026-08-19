<script setup lang="ts">
import { computed, inject, type Ref } from "vue";
import type { LyricLine, LyricTiming } from "../../domain/music";
import { resolveLyricWordProgress } from "../../domain/lyricsTimeline";
import { smoothLyricEmphasis } from "./lyricsTransition";
import { lyricsPlaybackPositionKey } from "./lyricsPlaybackPosition";
import WordByWordLyricText from "../../shared/lyrics/WordByWordLyricText.vue";

const props = defineProps<{
  activeState: Readonly<Ref<boolean>>;
  outgoingState: Readonly<Ref<boolean>>;
  line: LyricLine;
  offset: number;
  showTranslation: boolean;
  timing: LyricTiming;
  lineEmphasisState?: Readonly<Ref<number>>;
  nextLineTime?: number | null;
}>();

const playbackPosition = inject(lyricsPlaybackPositionKey);
if (!playbackPosition) throw new Error("LyricsLineContent requires a playback position provider.");

const EMPTY_WORD_PROGRESS: readonly number[] = [];
const active = computed(() => props.activeState.value);
const outgoing = computed(() => props.outgoingState.value);
const lineEmphasis = computed(() => {
  const explicit = props.lineEmphasisState?.value;
  if (explicit !== undefined && Number.isFinite(explicit)) return Math.max(0, Math.min(1, explicit));
  return active.value || outgoing.value ? 1 : 0;
});

function wordProgressesAt(playbackTime: number): number[] {
  const words = props.line.words ?? [];
  return words.map((word, index) => {
    const nextWord = words[index + 1];
    return resolveLyricWordProgress(word, playbackTime, nextWord?.time, props.nextLineTime);
  });
}

const wordProgresses = computed(() => {
  if (props.timing !== "WORD" || !props.line.words?.length) {
    return EMPTY_WORD_PROGRESS;
  }
  // The transition clock is the visibility gate. Both the entering line and the outgoing line
  // use the live playback position; a future line never flashes its already-progressed words.
  if (!active.value && !outgoing.value && lineEmphasis.value <= 0.001) return EMPTY_WORD_PROGRESS;
  if (lineEmphasis.value <= 0.001) return EMPTY_WORD_PROGRESS;
  return wordProgressesAt(playbackPosition.value + props.offset);
});

</script>

<template>
  <strong
    v-if="timing === 'WORD' && line.words?.length"
    :style="{ '--lyric-line-emphasis': smoothLyricEmphasis(lineEmphasis) }"
  >
    <WordByWordLyricText
      :text="line.text"
      :words="line.words ?? []"
      :progresses="wordProgresses"
      container-class="lyric-line-words"
      highlight-color="color-mix(in srgb, var(--playback-lyric-highlight) calc(var(--lyric-line-emphasis, 0) * 100%), var(--playback-lyric-text))"
    >
      <span
        v-for="(word, wordIndex) in line.words"
        :key="`${word.time}-${wordIndex}`"
        v-memo="[word, wordProgresses[wordIndex]]"
        class="lyric-word"
        dir="auto"
        :class="{
          'is-sung': wordProgresses[wordIndex] > 0,
          'is-current': wordProgresses[wordIndex] > 0 && wordProgresses[wordIndex] < 1,
        }"
        :style="{ '--lyric-word-progress': `${(wordProgresses[wordIndex] ?? 0) * 100}%` }"
      >{{ word.text }}</span>
    </WordByWordLyricText>
  </strong>
  <strong v-else class="lyric-line-text">{{ line.text }}</strong>
  <span
    v-if="showTranslation && line.translation"
    class="lyric-translation"
  >{{ line.translation }}</span>
</template>
