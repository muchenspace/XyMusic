<script setup lang="ts">
import { ChevronLeft, ChevronRight } from "lucide-vue-next";
import { computed, watch } from "vue";

const props = withDefaults(defineProps<{
  page: number;
  pageSize: number;
  total: number;
  totalPages?: number;
  pageSizeOptions?: number[];
}>(), { pageSizeOptions: () => [10, 25, 50, 100] });
const emit = defineEmits<{
  change: [page: number];
  pageSizeChange: [pageSize: number];
}>();
const MAX_OFFSET_ROWS = 10_000;
const actualPages = computed(() => Math.ceil(props.total / props.pageSize));
const reportedPages = computed(() => props.totalPages ?? actualPages.value);
const maxOffsetPages = computed(() => Math.floor(MAX_OFFSET_ROWS / props.pageSize) + 1);
const pages = computed(() => Math.max(1, Math.min(reportedPages.value, maxOffsetPages.value)));
const isCapped = computed(() => Math.max(actualPages.value, reportedPages.value) > pages.value);
const availablePageSizes = computed(() => [...new Set([
  props.pageSize,
  ...props.pageSizeOptions,
].filter((value) => Number.isSafeInteger(value) && value > 0 && value <= 100))].sort((left, right) => left - right));

function changePageSize(event: Event): void {
  const value = Number((event.target as HTMLSelectElement).value);
  if (value === props.pageSize || !availablePageSizes.value.includes(value)) return;
  emit("pageSizeChange", value);
}

watch([() => props.page, pages], ([page, totalPages]) => {
  const normalized = Math.min(totalPages, Math.max(1, Number.isSafeInteger(page) ? page : 1));
  if (normalized !== page) emit("change", normalized);
}, { immediate: true, flush: "post" });
</script>

<template>
  <nav class="flex flex-col gap-3 border-t border-[var(--border)] px-4 py-3 text-sm text-[var(--muted)] sm:flex-row sm:items-center sm:justify-between" aria-label="分页">
    <div class="flex flex-wrap items-center gap-x-4 gap-y-2">
      <span aria-live="polite">
        共 {{ total.toLocaleString() }} 条，第 {{ page }} / {{ pages }} 页
        <span v-if="isCapped">（仅开放前 {{ pages }} 页，请缩小筛选范围查看其余结果）</span>
      </span>
      <label class="flex items-center gap-2 whitespace-nowrap">
        每页
        <select class="ui-select !w-20 !py-1.5" :value="pageSize" aria-label="每页数量" @change="changePageSize">
          <option v-for="size in availablePageSizes" :key="size" :value="size">{{ size }}</option>
        </select>
        条
      </label>
    </div>
    <div class="flex items-center gap-2">
      <button class="btn btn-secondary btn-icon" type="button" :disabled="page <= 1" aria-label="上一页" @click="emit('change', page - 1)"><ChevronLeft :size="16" /></button>
      <button class="btn btn-secondary btn-icon" type="button" :disabled="page >= pages" aria-label="下一页" @click="emit('change', page + 1)"><ChevronRight :size="16" /></button>
    </div>
  </nav>
</template>
