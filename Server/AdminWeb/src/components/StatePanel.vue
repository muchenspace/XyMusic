<script setup lang="ts">
import { AlertTriangle, Inbox, LoaderCircle, RefreshCw } from "lucide-vue-next";

withDefaults(defineProps<{
  state: "loading" | "error" | "empty";
  title?: string;
  detail?: string;
  compact?: boolean;
}>(), { compact: false });
defineEmits<{ retry: [] }>();
</script>

<template>
  <div
    class="motion-item flex flex-col items-center justify-center px-5 text-center"
    :class="compact ? 'py-8' : 'min-h-64 py-14'"
    :role="state === 'error' ? 'alert' : 'status'"
    :aria-live="state === 'error' ? 'assertive' : 'polite'"
    aria-atomic="true"
  >
    <span class="mb-3 grid h-10 w-10 place-items-center rounded-md border border-[var(--border)] bg-[var(--surface-muted)] text-[var(--muted)]">
      <LoaderCircle v-if="state === 'loading'" :size="21" class="animate-spin" aria-hidden="true" />
      <AlertTriangle v-else-if="state === 'error'" :size="21" class="text-[var(--danger)]" aria-hidden="true" />
      <Inbox v-else :size="21" aria-hidden="true" />
    </span>
    <p class="font-medium text-[var(--text)]">{{ title ?? (state === 'loading' ? '正在加载数据' : state === 'error' ? '数据加载失败' : '暂无数据') }}</p>
    <p v-if="detail" class="mt-1 max-w-md whitespace-pre-line text-sm leading-5 text-[var(--muted)]">{{ detail }}</p>
    <button v-if="state === 'error'" type="button" class="btn btn-secondary mt-4" @click="$emit('retry')">
      <RefreshCw :size="15" aria-hidden="true" />重新加载
    </button>
  </div>
</template>
