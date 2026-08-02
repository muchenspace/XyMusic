<script setup lang="ts">
import { computed } from "vue";
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

function formatTime(seconds: number): string {
  const value = Number.isFinite(seconds) && seconds > 0 ? seconds : 0;
  return `${Math.floor(value / 60)}:${String(Math.floor(value % 60)).padStart(2, "0")}`;
}

function updateProgress(event: Event): void {
  player.seek(Number((event.target as HTMLInputElement).value));
}
</script>

<template>
  <div v-if="showTimes" class="progress-row">
    <span>{{ formatTime(player.currentTime) }}</span>
    <input
      :value="player.progress"
      type="range"
      min="0"
      max="100"
      step="0.1"
      aria-label="播放进度"
      :aria-valuetext="`${formatTime(player.currentTime)} / ${formatTime(duration)}`"
      :style="{ '--range-progress': `${player.progress}%` }"
      @input="updateProgress"
    />
    <span>{{ formatTime(duration) }}</span>
  </div>
  <input
    v-else
    :value="player.progress"
    type="range"
    min="0"
    max="100"
    step="0.1"
    aria-label="播放进度"
    :aria-valuetext="`${formatTime(player.currentTime)} / ${formatTime(duration)}`"
    :style="{ '--range-progress': `${player.progress}%` }"
    @input="updateProgress"
  />
</template>
