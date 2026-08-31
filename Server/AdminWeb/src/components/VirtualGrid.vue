<script setup lang="ts" generic="T">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";

const props = withDefaults(defineProps<{
  items: readonly T[];
  itemHeight?: number;
  minItemWidth?: number;
  gap?: number;
  overscan?: number;
  height?: string;
  rowKey?: (item: T, index: number) => string | number;
}>(), {
  itemHeight: 156,
  minItemWidth: 280,
  gap: 12,
  overscan: 2,
  height: "min(72vh, 760px)",
  rowKey: undefined,
});

defineSlots<{
  default?: (props: { item: T; index: number }) => unknown;
}>();

const viewport = ref<HTMLElement>();
const viewportWidth = ref(0);
const viewportHeight = ref(0);
const scrollTop = ref(0);
let resizeObserver: ResizeObserver | undefined;

const columns = computed(() => {
  if (viewportWidth.value <= 0) return 1;
  return Math.max(1, Math.floor((viewportWidth.value + props.gap) / (props.minItemWidth + props.gap)));
});
const rowCount = computed(() => Math.ceil(props.items.length / columns.value));
const startRow = computed(() => {
  const firstVisible = Math.floor(scrollTop.value / props.itemHeight);
  return Math.max(0, firstVisible - props.overscan);
});
const endRow = computed(() => {
  const visibleRows = Math.ceil((scrollTop.value + viewportHeight.value) / props.itemHeight);
  return Math.min(rowCount.value, Math.max(startRow.value + 1, visibleRows + props.overscan));
});
const startIndex = computed(() => startRow.value * columns.value);
const endIndex = computed(() => Math.min(props.items.length, endRow.value * columns.value));
const visibleItems = computed(() => props.items.slice(startIndex.value, endIndex.value));
const contentHeight = computed(() => Math.max(0, rowCount.value * props.itemHeight));
const cellHeight = computed(() => Math.max(1, props.itemHeight - props.gap));

function updateMetrics(): void {
  const element = viewport.value;
  if (!element) return;
  viewportWidth.value = element.clientWidth;
  viewportHeight.value = element.clientHeight;
  scrollTop.value = element.scrollTop;
}

function onScroll(event: Event): void {
  const element = event.currentTarget as HTMLElement;
  scrollTop.value = element.scrollTop;
  viewportWidth.value = element.clientWidth;
  viewportHeight.value = element.clientHeight;
}

const datasetMarker = computed(() => {
  if (!props.items.length) return "0:";
  const first = props.items[0]!;
  const key = props.rowKey?.(first, 0) ?? first;
  return `${props.items.length}:${String(key)}`;
});

watch(datasetMarker, () => {
  if (viewport.value) viewport.value.scrollTop = 0;
  updateMetrics();
});
watch(columns, () => {
  if (viewport.value) viewport.value.scrollTop = 0;
  updateMetrics();
});

onMounted(() => {
  updateMetrics();
  if (typeof ResizeObserver !== "undefined" && viewport.value) {
    resizeObserver = new ResizeObserver(updateMetrics);
    resizeObserver.observe(viewport.value);
  }
});

onBeforeUnmount(() => resizeObserver?.disconnect());
</script>

<template>
  <div
    ref="viewport"
    class="overflow-auto overscroll-contain"
    :style="{ height, maxHeight: height }"
    :aria-rowcount="items.length"
    @scroll="onScroll"
  >
    <div class="relative w-full" :style="{ height: `${contentHeight}px` }">
      <div
        class="absolute left-0 right-0 top-0 grid"
        :style="{
          transform: `translateY(${startRow * itemHeight}px)`,
          gridTemplateColumns: `repeat(${columns}, minmax(0, 1fr))`,
          gridAutoRows: `${cellHeight}px`,
          gap: `${gap}px`,
        }"
      >
        <div
          v-for="(item, index) in visibleItems"
          :key="rowKey?.(item, startIndex + index) ?? startIndex + index"
          class="min-w-0"
          :style="{ height: `${cellHeight}px` }"
        >
          <slot :item="item" :index="startIndex + index" />
        </div>
      </div>
    </div>
  </div>
</template>
