<script setup lang="ts">
import { computed, reactive, ref } from "vue";

const props = defineProps<{
  values: Partial<Record<string, number | undefined>>;
  labels?: Partial<Record<string, string>>;
}>();

const radius = 42;
const circumference = 2 * Math.PI * radius;
const chart = ref<HTMLElement>();
const activeKey = ref<string>();
const tooltip = reactive({ left: 72, top: 10 });
const statusColors: Record<string, string> = {
  PROCESSING: "#3b82f6",
  READY: "#10b981",
  ERROR: "#f43f5e",
  ARCHIVED: "#64748b",
};
const fallbackColors = ["#3b82f6", "#8b5cf6", "#f59e0b", "#06b6d4"];
const defaultLabels: Record<string, string> = {
  PROCESSING: "处理中",
  READY: "可用",
  ERROR: "异常",
  ARCHIVED: "已归档",
};
const total = computed(() => Object.values(props.values).reduce<number>((sum, value) => sum + (value ?? 0), 0));
const segments = computed(() => {
  let offset = 0;
  return Object.entries(props.values).map(([key, rawValue], index) => {
    const value = rawValue ?? 0;
    const percentage = total.value ? value / total.value * 100 : 0;
    const length = percentage / 100 * circumference;
    const segment = {
      key,
      value,
      percentage,
      color: statusColors[key] ?? fallbackColors[index % fallbackColors.length],
      label: props.labels?.[key] ?? defaultLabels[key] ?? key,
      dasharray: `${length} ${Math.max(0, circumference - length)}`,
      dashoffset: -offset,
    };
    offset += length;
    return segment;
  });
});
const activeSegment = computed(() => segments.value.find((segment) => segment.key === activeKey.value));
function showSegment(key: string, event?: Event): void {
  activeKey.value = key;
  const bounds = chart.value?.getBoundingClientRect();
  if (!bounds || !(event instanceof MouseEvent)) {
    tooltip.left = 72;
    tooltip.top = 12;
    return;
  }
  tooltip.left = Math.max(66, Math.min(bounds.width - 66, event.clientX - bounds.left));
  tooltip.top = Math.max(8, event.clientY - bounds.top - 8);
}
</script>

<template>
  <div class="grid h-44 w-full place-items-center" :aria-label="`曲目状态分布，共 ${total} 首`">
    <div ref="chart" class="relative h-36 w-36">
      <svg class="h-full w-full overflow-visible" viewBox="0 0 100 100" role="img">
        <circle cx="50" cy="50" :r="radius" fill="none" stroke="var(--surface-muted)" stroke-width="12" />
        <circle
          v-for="segment in segments"
          :key="segment.key"
          class="distribution-segment"
          :class="{ 'distribution-segment--empty': segment.value <= 0 }"
          cx="50"
          cy="50"
          :r="radius"
          fill="none"
          :stroke="segment.color"
          stroke-width="12"
          :stroke-dasharray="segment.dasharray"
          :stroke-dashoffset="segment.dashoffset"
          transform="rotate(-90 50 50)"
          :tabindex="segment.value > 0 ? 0 : -1"
          :aria-hidden="segment.value <= 0 ? 'true' : undefined"
          role="button"
          :aria-label="`${segment.label}：${segment.value} 首，占 ${segment.percentage.toFixed(1)}%`"
          @mouseenter="showSegment(segment.key, $event)"
          @mousemove="showSegment(segment.key, $event)"
          @mouseleave="activeKey = undefined"
          @focus="showSegment(segment.key)"
          @blur="activeKey = undefined"
        />
      </svg>
      <div class="pointer-events-none absolute inset-[20%] grid place-items-center rounded-full bg-[var(--surface-solid)] text-center">
        <div class="min-w-0 px-1">
          <p class="truncate text-[10px] font-semibold text-[var(--muted)]">曲目总数</p>
          <p class="mt-0.5 text-xl font-black">{{ total.toLocaleString() }}</p>
        </div>
      </div>
      <Transition name="chart-tooltip">
        <div v-if="activeSegment" class="pointer-events-none absolute z-10 w-36 -translate-x-1/2 -translate-y-full rounded-md border border-[var(--border-strong)] bg-[var(--surface-solid)] px-3 py-2 text-left shadow-lg" :style="{ left: `${tooltip.left}px`, top: `${tooltip.top}px` }" role="tooltip">
          <div class="flex items-center gap-2"><span class="h-2 w-2 rounded-full" :style="{ backgroundColor: activeSegment.color }" /><span class="text-xs font-semibold">{{ activeSegment.label }}</span></div>
          <div class="mt-1 flex items-baseline justify-between gap-3"><strong>{{ activeSegment.value.toLocaleString() }} 首</strong><span class="text-xs text-[var(--muted)]">{{ activeSegment.percentage.toFixed(1) }}%</span></div>
        </div>
      </Transition>
    </div>
  </div>
</template>

<style scoped>
.distribution-segment {
  cursor: pointer;
  opacity: 1;
  outline: none;
  transition:
    stroke-dasharray var(--motion-data) var(--motion-ease-out),
    stroke-dashoffset var(--motion-data) var(--motion-ease-out),
    filter var(--motion-fast) ease,
    opacity var(--motion-fast) ease;
}
.distribution-segment--empty { pointer-events: none; opacity: 0; }
.distribution-segment:hover,
.distribution-segment:focus-visible { filter: brightness(1.08); }
.chart-tooltip-enter-active,
.chart-tooltip-leave-active { transition: opacity var(--motion-fast) ease, scale var(--motion-fast) var(--motion-ease-out); transform-origin: bottom center; }
.chart-tooltip-enter-from,
.chart-tooltip-leave-to { opacity: 0; scale: .97; }
@media (prefers-reduced-motion: reduce) {
  .distribution-segment { transition: none; }
}
</style>
