<script setup lang="ts">
import { computed } from "vue";
import type { StatusTone } from "@/shared/presentation/audio-status";

const props = defineProps<{ status: string; label?: string; dot?: boolean; tone?: StatusTone }>();

const kind = computed(() => {
  if (props.tone) return props.tone;
  const value = props.status.toUpperCase();
  if (["ACTIVE", "READY", "PUBLISHED", "SUCCEEDED", "SUCCESS", "HEALTHY", "ONLINE", "ORIGINAL", "COMPLETED"].includes(value)) return "success";
  if (["RUNNING", "PROCESSING", "SCANNING", "PENDING_WRITE", "QUEUED", "PENDING", "DRAFT"].includes(value)) return "info";
  if (["SUSPENDED", "DEGRADED", "WRITE_FAILED", "MISSING"].includes(value)) return "warning";
  if (["FAILED", "FAILURE", "UNAVAILABLE", "OFFLINE", "DELETED", "ARCHIVED", "CANCELED", "CANCELLED", "ERROR", "DISABLED"].includes(value)) return "danger";
  return "neutral";
});

const text: Record<string, string> = {
  ACTIVE: "正常", SUSPENDED: "已停用", DELETED: "已删除",
  READY: "就绪", ERROR: "异常", PUBLISHED: "已发布", DRAFT: "草稿", ARCHIVED: "已归档",
  QUEUED: "处理中", PENDING: "处理中", PROCESSING: "处理中", RUNNING: "进行中", SUCCEEDED: "已完成", COMPLETED: "已完成", FAILED: "失败", CANCELED: "已取消", CANCELLED: "已取消",
  SUCCESS: "成功", FAILURE: "失败", HEALTHY: "正常", DEGRADED: "降级", UNAVAILABLE: "不可用",
  ONLINE: "在线", OFFLINE: "离线", SCANNING: "扫描中", UNKNOWN: "待检查", DISABLED: "已停用",
  ORIGINAL: "原始", OVERRIDDEN: "已修改", PENDING_WRITE: "等待写回", WRITE_FAILED: "写回失败",
  READ_ONLY: "只读", READ_WRITE: "可写",
};
</script>

<template>
  <span class="inline-flex items-center gap-1.5 rounded-md border px-2 py-0.5 text-xs font-medium"
    :class="{
      'border-emerald-500/25 bg-emerald-500/8 text-emerald-700 dark:text-emerald-400': kind === 'success',
      'border-blue-500/25 bg-blue-500/8 text-blue-700 dark:text-blue-300': kind === 'info',
      'border-amber-500/25 bg-amber-500/8 text-amber-700 dark:text-amber-300': kind === 'warning',
      'border-rose-500/25 bg-rose-500/8 text-rose-700 dark:text-rose-400': kind === 'danger',
      'border-[var(--border)] bg-[var(--surface-muted)] text-[var(--muted)]': kind === 'neutral',
    }">
    <span v-if="dot" class="h-1.5 w-1.5 rounded-full bg-current" />
    {{ label ?? text[status.toUpperCase()] ?? status }}
  </span>
</template>
