<script setup lang="ts">
import { computed, inject, type Ref } from "vue";
import type { LyricLine, LyricTiming } from "../../domain/music";
import { resolveLyricWordProgress } from "../../domain/lyricsTimeline";
import { lyricsPlaybackPositionKey } from "./lyricsPlaybackPosition";

const props = defineProps<{
  activeState: Readonly<Ref<boolean>>;
  line: LyricLine;
  offset: number;
  showTranslation: boolean;
  timing: LyricTiming;
}>();

const playbackPosition = inject(lyricsPlaybackPositionKey);
if (!playbackPosition) throw new Error("LyricsLineContent requires a playback position provider.");

const EMPTY_WORD_PROGRESS: readonly number[] = [];
const active = computed(() => props.activeState.value);

const wordProgresses = computed(() => {
  if (!active.value || props.timing !== "WORD" || !props.line.words?.length) return EMPTY_WORD_PROGRESS;
  const playbackTime = playbackPosition.value + props.offset;
  return props.line.words.map((word) => resolveLyricWordProgress(word, playbackTime));
});
</script>

<template>
  <strong v-if="timing === 'WORD'" class="lyric-line-words">
    <span
      v-for="(word, wordIndex) in line.words"
      :key="`${word.time}-${wordIndex}`"
      v-memo="[word, wordProgresses[wordIndex]]"
      class="lyric-word"
      :class="{
        'is-sung': wordProgresses[wordIndex] > 0,
        'is-current': wordProgresses[wordIndex] > 0 && wordProgresses[wordIndex] < 1,
      }"
      :style="{ '--lyric-word-progress': `${(wordProgresses[wordIndex] ?? 0) * 100}%` }"
    >{{ word.text }}</span>
  </strong>
  <strong v-else>{{ line.text }}</strong>
  <span v-if="showTranslation && line.translation" class="lyric-translation">{{ line.translation }}</span>
</template>
