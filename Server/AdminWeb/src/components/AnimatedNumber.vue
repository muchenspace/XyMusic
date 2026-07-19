<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";

const props = withDefaults(defineProps<{
  value: number;
  duration?: number;
}>(), { duration: 360 });

const displayed = ref(normalize(props.value));
let frame: number | undefined;

const formatted = computed(() => Math.round(displayed.value).toLocaleString());

watch(() => props.value, (value) => animateTo(normalize(value)));
onBeforeUnmount(cancelAnimation);

function normalize(value: number): number {
  return Number.isFinite(value) ? value : 0;
}

function cancelAnimation(): void {
  if (frame === undefined) return;
  window.cancelAnimationFrame(frame);
  frame = undefined;
}

function prefersReducedMotion(): boolean {
  return typeof window.matchMedia === "function" && window.matchMedia("(prefers-reduced-motion: reduce)").matches;
}

function animateTo(target: number): void {
  cancelAnimation();
  const startValue = displayed.value;
  const distance = target - startValue;
  if (!distance || props.duration <= 0 || prefersReducedMotion()) {
    displayed.value = target;
    return;
  }

  const startedAt = performance.now();
  const tick = (now: number): void => {
    const progress = Math.min(1, Math.max(0, (now - startedAt) / props.duration));
    const eased = 1 - Math.pow(1 - progress, 3);
    displayed.value = startValue + distance * eased;
    if (progress < 1) frame = window.requestAnimationFrame(tick);
    else frame = undefined;
  };
  frame = window.requestAnimationFrame(tick);
}
</script>

<template>
  <span>{{ formatted }}</span>
</template>
