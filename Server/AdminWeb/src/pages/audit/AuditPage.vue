<script setup lang="ts">
import { Braces, Download, FilterX, RefreshCw, Search } from "lucide-vue-next";
import { keepPreviousData, useQuery } from "@tanstack/vue-query";
import { computed, ref, watch } from "vue";
import { refDebounced } from "@vueuse/core";
import AppButton from "@/components/AppButton.vue";
import AppPagination from "@/components/AppPagination.vue";
import BaseDialog from "@/components/BaseDialog.vue";
import PageHeader from "@/components/PageHeader.vue";
import StatePanel from "@/components/StatePanel.vue";
import StatusBadge from "@/components/StatusBadge.vue";
import type { AuditEntry } from "@/features/audit/domain/models";
import {
  auditActionLabel,
  auditResultLabel,
  auditTargetTypeLabel,
} from "@/shared/presentation/audit";
import { useAuditAdmin } from "@/app/services/audit";
import { formatDate } from "@/utils/format";
import { quoteCsvCell } from "@/utils/csv";
import { apiErrorMessage } from "@/shared/application/api-error";

const page = ref(1);
const auditAdmin = useAuditAdmin();
const pageSize = ref(25);
const search = ref("");
const action = ref("");
const debouncedSearch = refDebounced(search, 300);
const debouncedAction = refDebounced(action, 300);
const result = ref("");
const from = ref("");
const to = ref("");
const detailOpen = ref(false);
const selected = ref<AuditEntry>();
const query = useQuery({
  queryKey: computed(() => ["admin", "audit", { page: page.value, pageSize: pageSize.value, search: debouncedSearch.value, action: debouncedAction.value, result: result.value, from: from.value, to: to.value }]),
  queryFn: ({ signal }) => auditAdmin.list({ page: page.value, pageSize: pageSize.value, search: debouncedSearch.value, action: debouncedAction.value, result: result.value, from: from.value || undefined, to: to.value || undefined, sort: "createdAt", order: "desc" }, signal),
  placeholderData: keepPreviousData,
});
watch([debouncedSearch, debouncedAction], () => { page.value = 1; });

function clear(): void { search.value = ""; action.value = ""; result.value = ""; from.value = ""; to.value = ""; page.value = 1; }
function changePageSize(value: number): void { pageSize.value = value; page.value = 1; }
function details(entry: AuditEntry): void { selected.value = entry; detailOpen.value = true; }
function exportCurrentPage(): void {
  const items = query.data.value?.items ?? [];
  const rows = [
    ["时间", "操作人", "操作", "操作代码", "资源类型", "资源类型代码", "资源 ID", "结果", "结果代码", "Trace ID"],
    ...items.map((entry) => [
      entry.createdAt,
      entry.actor?.username ?? "system",
      auditActionLabel(entry.action),
      entry.action,
      auditTargetTypeLabel(entry.resourceType),
      entry.resourceType,
      entry.resourceId ?? "",
      auditResultLabel(entry.result),
      entry.result,
      entry.traceId,
    ]),
  ];
  const blob = new Blob(["\uFEFF", rows.map((row) => row.map(quoteCsvCell).join(",")).join("\r\n")], { type: "text/csv;charset=utf-8" });
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement("a"); anchor.href = url; anchor.download = `xymusic-audit-${new Date().toISOString().slice(0, 10)}.csv`; anchor.hidden = true; document.body.append(anchor); anchor.click(); anchor.remove(); window.setTimeout(() => URL.revokeObjectURL(url), 0);
}
</script>

<template>
  <div class="space-y-6 page-enter">
    <PageHeader title="审计日志" description="追踪管理操作、执行结果、来源地址与请求链路。">
      <template #eyebrow>安全与合规</template>
      <template #actions><AppButton :disabled="!query.data.value?.items.length" @click="exportCurrentPage"><template #icon><Download :size="16" /></template>导出当前页</AppButton><AppButton :loading="query.isFetching.value" @click="query.refetch()"><template #icon><RefreshCw :size="16" /></template>刷新</AppButton></template>
    </PageHeader>

    <section class="ui-card overflow-hidden">
      <div class="grid gap-3 border-b border-[var(--border)] p-4 md:grid-cols-2 xl:grid-cols-[minmax(260px,1fr)_180px_150px_160px_160px_auto]">
        <div class="relative"><Search :size="16" class="absolute left-3.5 top-1/2 -translate-y-1/2 text-[var(--muted)]" /><input v-model="search" class="ui-input !pl-10" type="search" placeholder="搜索操作、资源或 Trace ID" @change="page = 1" /></div><input v-model="action" class="ui-input" placeholder="操作代码前缀" @change="page = 1" /><select v-model="result" class="ui-select" @change="page = 1"><option value="">全部结果</option><option value="SUCCESS">成功</option><option value="FAILURE">失败</option></select><input v-model="from" class="ui-input" type="date" aria-label="开始日期" @change="page = 1" /><input v-model="to" class="ui-input" type="date" aria-label="结束日期" @change="page = 1" /><AppButton variant="ghost" @click="clear"><template #icon><FilterX :size="15" /></template>清除</AppButton>
      </div>
      <StatePanel v-if="query.isPending.value" state="loading" /><StatePanel v-else-if="query.isError.value" state="error" :detail="apiErrorMessage(query.error.value, '无法读取审计日志。')" @retry="query.refetch()" /><StatePanel v-else-if="!query.data.value?.items.length" state="empty" title="没有符合条件的审计记录" />
      <template v-else><div class="overflow-x-auto"><table class="data-table min-w-[860px]"><thead><tr><th>时间</th><th>操作人</th><th>操作</th><th>资源</th><th>结果</th><th>Trace ID</th></tr></thead><tbody><tr v-for="entry in query.data.value.items" :key="entry.id" class="cursor-pointer" tabindex="0" :aria-label="`查看审计记录：${auditActionLabel(entry.action)}`" @click="details(entry)" @keydown.enter="details(entry)" @keydown.space.prevent="details(entry)"><td class="whitespace-nowrap text-xs text-[var(--muted)]">{{ formatDate(entry.createdAt) }}</td><td><p class="font-semibold">{{ entry.actor?.displayName ?? '系统' }}</p><p class="text-xs text-[var(--muted)]">{{ entry.actor ? `@${entry.actor.username}` : '后台任务' }}</p></td><td><p class="font-semibold">{{ auditActionLabel(entry.action) }}</p><code v-if="auditActionLabel(entry.action) !== entry.action" class="mt-1 block text-[10px] text-[var(--muted)]">{{ entry.action }}</code></td><td><p class="font-semibold">{{ auditTargetTypeLabel(entry.resourceType) }}</p><p v-if="auditTargetTypeLabel(entry.resourceType) !== entry.resourceType" class="text-[10px] text-[var(--muted)]">{{ entry.resourceType }}</p><p class="max-w-40 truncate font-mono text-[10px] text-[var(--muted)]">{{ entry.resourceId ?? '—' }}</p></td><td><StatusBadge :status="entry.result" dot /></td><td><span class="block max-w-32 truncate font-mono text-[10px] text-[var(--muted)]" :title="entry.traceId">{{ entry.traceId }}</span></td></tr></tbody></table></div><AppPagination :page="page" :page-size="pageSize" :total="query.data.value.total" @change="page = $event" @page-size-change="changePageSize" /></template>
    </section>
    <BaseDialog v-model="detailOpen" title="审计记录详情" :description="selected ? formatDate(selected.createdAt) : ''" width="lg">
      <template v-if="selected"><div class="flex items-center justify-between rounded-xl bg-[var(--surface-muted)] p-4"><div><p class="text-sm font-bold">{{ auditActionLabel(selected.action) }}</p><code v-if="auditActionLabel(selected.action) !== selected.action" class="mt-1 block text-[10px] text-[var(--muted)]">{{ selected.action }}</code><p class="mt-1 text-xs text-[var(--muted)]">{{ selected.actor?.displayName ?? '系统' }}<span v-if="selected.actor"> · @{{ selected.actor.username }}</span></p></div><StatusBadge :status="selected.result" dot /></div><dl class="mt-5 grid gap-4 sm:grid-cols-2"><div><dt class="text-xs font-semibold text-[var(--muted)]">资源类型</dt><dd class="mt-1 font-semibold">{{ auditTargetTypeLabel(selected.resourceType) }}</dd><dd v-if="auditTargetTypeLabel(selected.resourceType) !== selected.resourceType" class="mt-1 font-mono text-[10px] text-[var(--muted)]">{{ selected.resourceType }}</dd></div><div><dt class="text-xs font-semibold text-[var(--muted)]">资源 ID</dt><dd class="mt-1 break-all font-mono text-xs">{{ selected.resourceId ?? '—' }}</dd></div><div class="sm:col-span-2"><dt class="text-xs font-semibold text-[var(--muted)]">Trace ID</dt><dd class="mt-1 break-all font-mono text-xs">{{ selected.traceId }}</dd></div></dl><div class="mt-6"><h3 class="flex items-center gap-2 font-bold"><Braces :size="17" />操作元数据</h3><pre class="mt-3 max-h-80 overflow-auto rounded-xl bg-[#0b1020] p-4 text-xs leading-6 text-slate-300">{{ JSON.stringify(selected.metadata ?? {}, null, 2) }}</pre></div></template>
      <template #footer><AppButton @click="detailOpen = false">关闭</AppButton></template>
    </BaseDialog>
  </div>
</template>
