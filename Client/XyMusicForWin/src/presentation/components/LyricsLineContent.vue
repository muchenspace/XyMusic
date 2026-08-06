<script setup lang="ts">
import { computed, inject, ref, type Ref, watch } from "vue";
import type { LyricLine, LyricTiming } from "../../domain/music";
import { resolveLyricWordProgress } from "../../domain/lyricsTimeline";
import { lyricsPlaybackPositionKey } from "./lyricsPlaybackPosition";

const props = defineProps<{
  activeState: Readonly<Ref<boolean>>;
  outgoingState: Readonly<Ref<boolean>>;
  line: LyricLine;
  offset: number;
  showTranslation: boolean;
  timing: LyricTiming;
}>();

const playbackPosition = inject(lyricsPlaybackPositionKey);
if (!playbackPosition) throw new Error("LyricsLineContent requires a playback position provider.");

const EMPTY_WORD_PROGRESS: readonly number[] = [];
const active = computed(() => props.activeState.value);
const outgoing = computed(() => props.outgoingState.value);
const outgoingWordProgresses = ref<readonly number[]>(EMPTY_WORD_PROGRESS);

function wordProgressesAt(playbackTime: number): number[] {
  return (props.line.words ?? []).map((word) => {
    const progress = resolveLyricWordProgress(word, playbackTime);
    if (progress === 0 && word.endTime === undefined && playbackTime > word.time) return 1;
    return progress;
  });
}

watch(outgoing, (isOutgoing) => {
  outgoingWordProgresses.value = isOutgoing
    ? wordProgressesAt(playbackPosition.value + props.offset)
    : EMPTY_WORD_PROGRESS;
}, { immediate: true });

const wordProgresses = computed(() => {
  if (props.timing !== "WORD" || !props.line.words?.length) {
    return EMPTY_WORD_PROGRESS;
  }
  if (outgoing.value && !active.value) return outgoingWordProgresses.value;
  if (!active.value) return EMPTY_WORD_PROGRESS;
  return wordProgressesAt(playbackPosition.value + props.offset);
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
