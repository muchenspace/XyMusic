<script setup lang="ts">
import { computed, ref } from "vue";
import { usePlayerStore } from "../stores/playerStore";

const props = withDefaults(defineProps<{
  fallbackDuration?: number;
  showTimes?: boolean;
}>(), {
  fallbackDuration: 0,
  showTimes: false,
});

const player = usePlayerStore();
const duration = computed(() => player.duration || props.fallbackDuration);
const draftProgress = ref<number | null>(null);
const displayedProgress = computed(() => draftProgress.value ?? player.progress);
const displayedTime = computed(() => duration.value * displayedProgress.value / 100);

function formatTime(seconds: number): string {
  const value = Number.isFinite(seconds) && seconds > 0 ? seconds : 0;
  return `${Math.floor(value / 60)}:${String(Math.floor(value % 60)).padStart(2, "0")}`;
}

function previewProgress(event: Event): void {
  draftProgress.value = clampProgress(Number((event.target as HTMLInputElement).value));
}

function commitProgress(event: Event): void {
  const value = clampProgress(Number((event.target as HTMLInputElement).value));
  draftProgress.value = null;
  player.seek(value);
}

function clampProgress(value: number): number {
  return Number.isFinite(value) ? Math.max(0, Math.min(100, value)) : player.progress;
}
</script>

<template>
  <div v-if="showTimes" class="progress-row">
    <span>{{ formatTime(displayedTime) }}</span>
    <input
      :value="displayedProgress"
      type="range"
      min="0"
      max="100"
      step="0.1"
      aria-label="播放进度"
      :aria-valuetext="`${formatTime(displayedTime)} / ${formatTime(duration)}`"
      :style="{ '--range-progress': `${displayedProgress}%` }"
      @input="previewProgress"
      @change="commitProgress"
    />
    <span>{{ formatTime(duration) }}</span>
  </div>
  <input
    v-else
    :value="displayedProgress"
    type="range"
    min="0"
    max="100"
    step="0.1"
    aria-label="播放进度"
    :aria-valuetext="`${formatTime(displayedTime)} / ${formatTime(duration)}`"
    :style="{ '--range-progress': `${displayedProgress}%` }"
    @input="previewProgress"
    @change="commitProgress"
  />
</template>
