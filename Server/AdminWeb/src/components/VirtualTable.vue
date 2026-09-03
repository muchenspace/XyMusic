<script setup lang="ts" generic="T">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";

const props = withDefaults(defineProps<{
  items: readonly T[];
  columns: number;
  rowHeight?: number;
  overscan?: number;
  height?: string;
  minWidth?: string;
  rowKey?: (item: T, index: number) => string | number;
}>(), {
  rowHeight: 72,
  overscan: 8,
  height: "min(72vh, 760px)",
  minWidth: "0",
  rowKey: undefined,
});

defineSlots<{
  header?: () => unknown;
  default?: (props: { item: T; index: number }) => unknown;
}>();

const viewport = ref<HTMLElement>();
const scrollTop = ref(0);
const viewportHeight = ref(0);
let resizeObserver: ResizeObserver | undefined;

const startIndex = computed(() => {
  const firstVisible = Math.floor(scrollTop.value / props.rowHeight);
  return Math.max(0, firstVisible - props.overscan);
});
const endIndex = computed(() => {
  const lastVisible = Math.ceil((scrollTop.value + viewportHeight.value) / props.rowHeight);
  return Math.min(props.items.length, Math.max(startIndex.value + 1, lastVisible + props.overscan));
});
const visibleItems = computed(() => props.items.slice(startIndex.value, endIndex.value));
const topSpacerHeight = computed(() => startIndex.value * props.rowHeight);
const bottomSpacerHeight = computed(() => Math.max(0, (props.items.length - endIndex.value) * props.rowHeight));

function updateViewportMetrics(): void {
  const element = viewport.value;
  if (!element) return;
  viewportHeight.value = element.clientHeight;
  scrollTop.value = element.scrollTop;
}

function onScroll(event: Event): void {
  const element = event.currentTarget as HTMLElement;
  scrollTop.value = element.scrollTop;
  if (viewportHeight.value <= 0) {
    viewportHeight.value = element.clientHeight;
  }
}

const datasetMarker = computed(() => {
  if (!props.items.length) return "0:";
  const first = props.items[0]!;
  const key = props.rowKey?.(first, 0) ?? first;
  return `${props.items.length}:${String(key)}`;
});

watch(datasetMarker, () => {
  const element = viewport.value;
  if (!element) return;
  // A new page/filter represents a new logical dataset. Do not leave the
  // virtual viewport scrolled to the old page's middle. Background status
  // refreshes keep the same marker and therefore preserve the user's scroll.
  element.scrollTop = 0;
  updateViewportMetrics();
});

watch(() => props.items.length, () => {
  const element = viewport.value;
  if (!element) return;
  const maximumScrollTop = Math.max(0, element.scrollHeight - element.clientHeight);
  if (element.scrollTop > maximumScrollTop) element.scrollTop = maximumScrollTop;
  updateViewportMetrics();
});

onMounted(() => {
  updateViewportMetrics();
  if (typeof ResizeObserver !== "undefined" && viewport.value) {
    resizeObserver = new ResizeObserver(updateViewportMetrics);
    resizeObserver.observe(viewport.value);
  }
});

onBeforeUnmount(() => resizeObserver?.disconnect());
</script>

<template>
  <div
    ref="viewport"
    class="overflow-auto overscroll-contain"
    :style="{ height, maxHeight: height, willChange: 'scroll-position' }"
    :aria-rowcount="items.length"
    @scroll="onScroll"
  >
    <table class="data-table" :style="{ minWidth }">
      <thead class="sticky top-0 z-10 bg-[var(--surface-solid)]">
        <slot name="header" />
      </thead>
      <tbody>
        <tr v-if="topSpacerHeight" aria-hidden="true" class="pointer-events-none">
          <td :colspan="columns" class="!p-0 !border-0" :style="{ height: `${topSpacerHeight}px` }" />
        </tr>
        <template v-for="(item, index) in visibleItems" :key="rowKey?.(item, startIndex + index) ?? startIndex + index">
          <slot :item="item" :index="startIndex + index" />
        </template>
        <tr v-if="bottomSpacerHeight" aria-hidden="true" class="pointer-events-none">
          <td :colspan="columns" class="!p-0 !border-0" :style="{ height: `${bottomSpacerHeight}px` }" />
        </tr>
      </tbody>
    </table>
  </div>
</template>
