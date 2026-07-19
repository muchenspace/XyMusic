<script setup lang="ts">
import { AlertTriangle, Ban, Clock3, Eye, FileCog, RefreshCw, RotateCcw, Search, Wifi, WifiOff } from "lucide-vue-next";
import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/vue-query";
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { refDebounced } from "@vueuse/core";
import { ApiError, apiErrorMessage } from "@/shared/application/api-error";
import AppButton from "@/components/AppButton.vue";
import AppPagination from "@/components/AppPagination.vue";
import BaseDialog from "@/components/BaseDialog.vue";
import PageHeader from "@/components/PageHeader.vue";
import StatePanel from "@/components/StatePanel.vue";
import StatusBadge from "@/components/StatusBadge.vue";
import type { JobEventSubscription } from "@/features/jobs/application/job-admin-gateway";
import type { JobDetail, JobSummary, MetadataWritebackJob } from "@/features/jobs/domain/models";
import { useJobAdmin } from "@/app/services/jobs";
import { useUiStore } from "@/stores/ui";
import { formatDate, humanize } from "@/utils/format";

const queryClient = useQueryClient();
const ui = useUiStore();
const jobAdmin = useJobAdmin();
const page = ref(1);
const pageSize = ref(20);
const status = ref("");
const type = ref("");
const search = ref("");
const debouncedSearch = refDebounced(search, 300);
const selectedId = ref("");
const detailOpen = ref(false);
const streamConnected = ref(false);
const writebackPage = ref(1);
const writebackPageSize = ref(10);
const writebackStatus = ref("");
const selectedWriteback = ref<MetadataWritebackJob>();
const writebackAction = ref<"retry" | "cancel">("retry");
const writebackReason = ref("");
const writebackActionOpen = ref(false);
let eventSource: JobEventSubscription | undefined;
let invalidationTimer: number | undefined;
let allowWritebackActionClose = false;

const query = useQuery({
  queryKey: computed(() => ["admin", "jobs", { page: page.value, pageSize: pageSize.value, status: status.value, type: type.value, search: debouncedSearch.value }]),
  queryFn: ({ signal }) => jobAdmin.list({ page: page.value, pageSize: pageSize.value, status: status.value, type: type.value, search: debouncedSearch.value, sort: "createdAt", order: "desc" }, signal),
  placeholderData: keepPreviousData,
  refetchInterval: (state) => streamConnected.value ? false : state.state.data?.items.some((job) => ["QUEUED", "RUNNING"].includes(job.status)) ? 5_000 : 60_000,
});
const selectedSummary = computed(() => query.data.value?.items.find((job) => job.id === selectedId.value));
const detailQuery = useQuery({
  queryKey: computed(() => ["admin", "jobs", "detail", selectedId.value]),
  queryFn: ({ signal }) => jobAdmin.detail(selectedId.value, signal),
  enabled: computed(() => detailOpen.value && Boolean(selectedId.value)),
  refetchInterval: (state) => ["QUEUED", "RUNNING"].includes(state.state.data?.status ?? "") ? 5_000 : false,
});
const selected = computed<JobDetail | JobSummary | undefined>(() => detailQuery.data.value ?? selectedSummary.value);
watch([status, type, debouncedSearch], () => { page.value = 1; });
const running = computed(() => query.data.value?.items.filter((job) => job.status === "RUNNING").length ?? 0);
const writebackQuery = useQuery({
  queryKey: computed(() => ["admin", "metadata-writeback-jobs", writebackPage.value, writebackPageSize.value, writebackStatus.value]),
  queryFn: ({ signal }) => jobAdmin.listWritebacks(writebackPage.value, writebackPageSize.value, writebackStatus.value, signal),
  placeholderData: keepPreviousData,
  refetchInterval: (state) => state.state.data?.items.some((job) => ["PENDING", "PROCESSING"].includes(job.status)) ? 5_000 : 60_000,
});

function queueRefresh(): void {
  if (invalidationTimer) return;
  invalidationTimer = window.setTimeout(() => { invalidationTimer = undefined; void queryClient.invalidateQueries({ queryKey: ["admin", "jobs"] }); void queryClient.invalidateQueries({ queryKey: ["admin", "metadata-writeback-jobs"] }); void queryClient.invalidateQueries({ queryKey: ["admin", "dashboard"] }); }, 500);
}
onMounted(() => {
  eventSource = jobAdmin.watch(
    () => { streamConnected.value = true; },
    queueRefresh,
    () => { streamConnected.value = false; },
  );
});
onBeforeUnmount(() => { eventSource?.close(); if (invalidationTimer) window.clearTimeout(invalidationTimer); });
function details(job: JobSummary): void { selectedId.value = job.id; detailOpen.value = true; }
function changePageSize(value: number): void { pageSize.value = value; page.value = 1; }
function changeWritebackPageSize(value: number): void { writebackPageSize.value = value; writebackPage.value = 1; }
async function refresh(): Promise<void> { await Promise.all([queryClient.invalidateQueries({ queryKey: ["admin", "jobs"] }), queryClient.invalidateQueries({ queryKey: ["admin", "dashboard"] }), queryClient.invalidateQueries({ queryKey: ["admin", "audit"] })]); }
const retryMutation = useMutation({ mutationFn: (job: JobSummary) => jobAdmin.retry(job.id), onSuccess: async () => { ui.notify("success", "任务已重新入队"); await refresh(); }, onError: (error) => ui.notify("error", "任务重试失败", error instanceof ApiError ? error.message : undefined) });
const cancelMutation = useMutation({ mutationFn: (job: JobSummary) => jobAdmin.cancel(job.id), onSuccess: async () => { ui.notify("success", "任务取消请求已提交"); await refresh(); }, onError: (error) => ui.notify("error", "取消任务失败", error instanceof ApiError ? error.message : undefined) });
function percent(job: JobSummary): number { return Math.max(0, Math.min(100, job.progress)); }
function retrySelected(): void { if (selected.value) retryMutation.mutate(selected.value); }
function askWriteback(job: MetadataWritebackJob, action: "retry" | "cancel"): void { selectedWriteback.value = job; writebackAction.value = action; writebackReason.value = ""; writebackActionOpen.value = true; }
const writebackMutation = useMutation({
  mutationFn: () => {
    if (!writebackReason.value.trim()) throw new Error("请填写操作原因");
    const job = selectedWriteback.value!;
    return jobAdmin.changeWriteback(
      job.id,
      job.version,
      writebackAction.value,
      writebackReason.value.trim(),
    );
  },
  onSuccess: async () => { allowWritebackActionClose = true; writebackActionOpen.value = false; ui.notify("success", writebackAction.value === "retry" ? "Tag 写回任务已重试" : "Tag 写回取消请求已提交"); await writebackQuery.refetch(); },
  onError: (error) => ui.notify("error", "Tag 写回操作失败", error instanceof Error ? error.message : undefined),
});
watch(writebackActionOpen, (value) => {
  if (!value && writebackMutation.isPending.value && !allowWritebackActionClose) writebackActionOpen.value = true;
  allowWritebackActionClose = false;
});
</script>

<template>
  <div class="space-y-6 page-enter">
    <PageHeader title="后台任务" description="跟踪扫描、Tag 写回、封面与媒体处理进度。">
      <template #actions><span class="inline-flex items-center gap-2 rounded-xl border border-[var(--border)] bg-[var(--surface-solid)] px-3 py-2 text-xs font-semibold" :class="streamConnected ? 'text-emerald-600 dark:text-emerald-400' : 'text-[var(--muted)]'"><Wifi v-if="streamConnected" :size="14" /><WifiOff v-else :size="14" />{{ streamConnected ? '实时更新已连接' : '使用定时刷新' }}</span><AppButton :loading="query.isFetching.value" @click="query.refetch()"><template #icon><RefreshCw :size="16" /></template>刷新</AppButton></template>
    </PageHeader>

    <section class="ui-card grid overflow-hidden bg-[var(--border)] sm:grid-cols-3"><article class="bg-[var(--surface-solid)] p-4"><p class="text-xs font-medium text-[var(--muted)]">当前页运行</p><p class="mt-1 text-xl font-bold">{{ running }}</p></article><article class="bg-[var(--surface-solid)] p-4"><p class="text-xs font-medium text-[var(--muted)]">当前页等待</p><p class="mt-1 text-xl font-bold">{{ query.data.value?.items.filter((job) => job.status === 'QUEUED').length ?? 0 }}</p></article><article class="bg-[var(--surface-solid)] p-4"><p class="text-xs font-medium text-[var(--muted)]">当前页失败</p><p class="mt-1 text-xl font-bold" :class="query.data.value?.items.some((job) => job.status === 'FAILED') && 'text-[var(--danger)]'">{{ query.data.value?.items.filter((job) => job.status === 'FAILED').length ?? 0 }}</p></article></section>

    <section class="ui-card overflow-hidden" :class="{ 'data-refreshing': query.isFetching.value && !query.isPending.value }" :aria-busy="query.isFetching.value">
      <div class="flex flex-col gap-3 border-b border-[var(--border)] p-4 lg:flex-row"><div class="relative flex-1"><Search :size="16" class="absolute left-3.5 top-1/2 -translate-y-1/2 text-[var(--muted)]" /><input v-model="search" class="ui-input !pl-10" type="search" placeholder="搜索任务标题" @change="page = 1" /></div><select v-model="type" class="ui-select lg:w-52" @change="page = 1"><option value="">全部任务类型</option><option value="SOURCE_SCAN">音源扫描</option><option value="MEDIA_PROCESS">媒体处理</option><option value="TAG_WRITE">Tag 写回</option></select><select v-model="status" class="ui-select lg:w-44" @change="page = 1"><option value="">全部状态</option><option value="QUEUED">等待中</option><option value="RUNNING">进行中</option><option value="SUCCEEDED">已完成</option><option value="FAILED">失败</option><option value="CANCELED">已取消</option></select></div>
      <StatePanel v-if="query.isPending.value" state="loading" /><StatePanel v-else-if="query.isError.value" state="error" :detail="apiErrorMessage(query.error.value, '无法读取后台任务。')" @retry="query.refetch()" /><StatePanel v-else-if="!query.data.value?.items.length" state="empty" title="没有符合条件的任务" />
      <template v-else><div class="overflow-x-auto"><table class="data-table min-w-[1000px]"><thead><tr><th>任务</th><th>状态</th><th>进度</th><th>处理数量</th><th>尝试次数</th><th>创建时间</th><th>操作</th></tr></thead><tbody><tr v-for="job in query.data.value.items" :key="job.id" class="cursor-pointer" tabindex="0" :aria-label="`查看任务：${job.title}`" @click="details(job)" @keydown.enter="details(job)" @keydown.space.prevent="details(job)"><td><div class="flex items-center gap-3"><span class="grid h-10 w-10 place-items-center rounded-xl bg-[var(--surface-muted)] text-[var(--primary)]"><FileCog :size="18" /></span><div><p class="max-w-80 truncate font-semibold">{{ job.title }}</p><p class="mt-1 text-[10px] font-bold text-[var(--muted)]">{{ humanize(job.type) }} · {{ job.id.slice(0, 8) }}</p></div></div></td><td><StatusBadge :status="job.status" dot /></td><td class="w-52"><div class="flex items-center gap-3"><div class="h-1.5 flex-1 overflow-hidden rounded-full bg-[var(--surface-muted)]"><div class="progress-fill h-full rounded-full" :class="job.status === 'FAILED' ? 'bg-rose-500' : 'bg-[var(--primary)]'" :style="{ width: `${Math.max(job.status === 'QUEUED' ? 2 : 0, percent(job))}%` }" /></div><span class="w-9 text-right text-xs font-semibold">{{ Math.round(percent(job)) }}%</span></div></td><td><span class="font-semibold">{{ job.processed.toLocaleString() }}</span><span class="text-[var(--muted)]"> / {{ job.total.toLocaleString() }}</span></td><td>{{ job.attempts }}</td><td class="text-xs text-[var(--muted)]">{{ formatDate(job.createdAt) }}</td><td @click.stop @keydown.stop><div class="flex gap-1"><button class="btn btn-ghost btn-icon" type="button" :aria-label="`查看任务：${job.title}`" @click="details(job)"><Eye :size="15" /></button><button v-if="job.status === 'FAILED'" class="btn btn-ghost btn-icon" type="button" :aria-label="`重试任务：${job.title}`" :disabled="retryMutation.isPending.value" @click="retryMutation.mutate(job)"><RotateCcw :size="15" /></button><button v-if="['QUEUED','RUNNING'].includes(job.status)" class="btn btn-ghost btn-icon text-[var(--danger)]" type="button" :aria-label="`取消任务：${job.title}`" :disabled="cancelMutation.isPending.value" @click="cancelMutation.mutate(job)"><Ban :size="15" /></button></div></td></tr></tbody></table></div><AppPagination :page="page" :page-size="pageSize" :total="query.data.value.total" @change="page = $event" @page-size-change="changePageSize" /></template>
    </section>

    <section class="ui-card overflow-hidden" :class="{ 'data-refreshing': writebackQuery.isFetching.value && !writebackQuery.isPending.value }" :aria-busy="writebackQuery.isFetching.value">
      <div class="flex flex-col gap-3 border-b border-[var(--border)] px-5 py-4 sm:flex-row sm:items-center sm:justify-between"><div><h2 class="font-bold">Tag 写回任务</h2><p class="mt-1 text-xs text-[var(--muted)]">源文件 Tag 的写入与校验状态</p></div><div class="flex gap-2"><select v-model="writebackStatus" class="ui-select min-w-40" @change="writebackPage = 1"><option value="">全部状态</option><option value="PENDING">等待中</option><option value="PROCESSING">处理中</option><option value="READY">完成</option><option value="FAILED">失败</option><option value="CANCELLED">已取消</option></select><AppButton icon-only :loading="writebackQuery.isFetching.value" @click="writebackQuery.refetch()"><template #icon><RefreshCw :size="16" /></template>刷新</AppButton></div></div>
      <StatePanel v-if="writebackQuery.isPending.value" state="loading" compact /><StatePanel v-else-if="writebackQuery.isError.value" state="error" compact :detail="apiErrorMessage(writebackQuery.error.value, '无法读取 Tag 写回任务。')" @retry="writebackQuery.refetch()" /><StatePanel v-else-if="!writebackQuery.data.value?.items.length" state="empty" compact title="暂无 Tag 写回任务" />
      <template v-else><div class="overflow-x-auto"><table class="data-table min-w-[820px]"><thead><tr><th>任务 / 曲目</th><th>状态</th><th>元数据版本</th><th>尝试</th><th>错误</th><th>创建时间</th><th>操作</th></tr></thead><tbody><tr v-for="job in writebackQuery.data.value.items" :key="job.id"><td><p class="font-mono text-xs font-semibold">{{ job.id.slice(0, 8) }}</p><p class="mt-1 font-mono text-[10px] text-[var(--muted)]">{{ job.trackId }}</p></td><td><StatusBadge :status="job.status" dot /></td><td>{{ job.metadataVersion }}</td><td>{{ job.attempts }} / {{ job.maxAttempts }}</td><td><p v-if="job.lastError" class="max-w-52 truncate text-xs text-[var(--danger)]" :title="job.lastError">{{ job.lastErrorCode }} · {{ job.lastError }}</p><span v-else>—</span></td><td class="text-xs text-[var(--muted)]">{{ formatDate(job.createdAt) }}</td><td><div class="flex gap-1"><button v-if="['FAILED','CANCELLED'].includes(job.status)" class="btn btn-ghost btn-icon" type="button" aria-label="重试写回" @click="askWriteback(job, 'retry')"><RotateCcw :size="15" /></button><button v-if="['PENDING','PROCESSING'].includes(job.status)" class="btn btn-ghost btn-icon text-[var(--danger)]" type="button" aria-label="取消写回" @click="askWriteback(job, 'cancel')"><Ban :size="15" /></button><span v-if="job.status === 'READY'">—</span></div></td></tr></tbody></table></div><AppPagination :page="writebackPage" :page-size="writebackPageSize" :total="writebackQuery.data.value.total" @change="writebackPage = $event" @page-size-change="changeWritebackPageSize" /></template>
    </section>

    <BaseDialog v-model="detailOpen" title="任务详情" :description="selected ? `${humanize(selected.type)} · ${selected.id}` : ''">
      <StatePanel v-if="detailQuery.isPending.value && !selected" state="loading" compact />
      <StatePanel v-else-if="detailQuery.isError.value" state="error" compact :detail="apiErrorMessage(detailQuery.error.value, '无法读取任务详情。')" @retry="detailQuery.refetch()" />
      <template v-else-if="selected"><div class="flex items-center justify-between rounded-xl bg-[var(--surface-muted)] p-4"><div><p class="font-bold">{{ selected.title }}</p><p class="mt-1 text-xs text-[var(--muted)]">尝试 {{ selected.attempts }}<template v-if="detailQuery.data.value"> / {{ detailQuery.data.value.maxAttempts }}</template> 次</p></div><StatusBadge :status="selected.status" dot /></div><div class="mt-5"><div class="mb-2 flex justify-between text-xs font-semibold"><span>处理进度</span><span>{{ selected.processed }} / {{ selected.total }}</span></div><div class="h-2 overflow-hidden rounded-full bg-[var(--surface-muted)]"><div class="progress-fill h-full rounded-full bg-[var(--primary)]" :style="{ width: `${percent(selected)}%` }" /></div></div><dl class="mt-6 grid grid-cols-2 gap-4 text-sm"><div><dt class="text-xs text-[var(--muted)]">创建时间</dt><dd class="mt-1 font-semibold">{{ formatDate(selected.createdAt) }}</dd></div><div><dt class="text-xs text-[var(--muted)]">开始时间</dt><dd class="mt-1 font-semibold">{{ formatDate(selected.startedAt ?? undefined) }}</dd></div><div><dt class="text-xs text-[var(--muted)]">完成时间</dt><dd class="mt-1 font-semibold">{{ formatDate(selected.completedAt ?? undefined) }}</dd></div><div><dt class="text-xs text-[var(--muted)]">任务类型</dt><dd class="mt-1 font-semibold">{{ humanize(selected.type) }}</dd></div><template v-if="detailQuery.data.value"><div><dt class="text-xs text-[var(--muted)]">更新时间</dt><dd class="mt-1 font-semibold">{{ formatDate(detailQuery.data.value.updatedAt) }}</dd></div><div><dt class="text-xs text-[var(--muted)]">任务来源</dt><dd class="mt-1 font-semibold">{{ humanize(detailQuery.data.value.source) }}</dd></div><div><dt class="text-xs text-[var(--muted)]">Worker 心跳</dt><dd class="mt-1 font-semibold">{{ formatDate(detailQuery.data.value.heartbeatAt ?? undefined) }}</dd></div><div><dt class="text-xs text-[var(--muted)]">锁定至</dt><dd class="mt-1 font-semibold">{{ formatDate(detailQuery.data.value.lockedUntil ?? undefined) }}</dd></div></template></dl><div v-if="detailQuery.data.value?.cancelRequested" class="mt-5 rounded-md border border-amber-500/25 bg-amber-500/8 p-3 text-sm text-amber-700 dark:text-amber-300">已提交取消请求，Worker 将在安全点停止任务。</div><div v-if="selected.error" class="mt-5 rounded-xl border border-rose-500/20 bg-rose-500/8 p-4"><p class="flex items-center gap-2 font-semibold text-[var(--danger)]"><AlertTriangle :size="16" />{{ selected.error.code }}</p><p class="mt-2 text-sm leading-6 text-[var(--muted)]">{{ selected.error.message }}</p></div><p class="mt-5 flex items-center gap-1.5 text-xs text-[var(--muted)]"><Clock3 :size="13" />任务状态由服务端工作进程持续更新。</p></template>
      <template #footer><AppButton v-if="selected?.status === 'FAILED'" :loading="retryMutation.isPending.value" @click="retrySelected"><template #icon><RotateCcw :size="15" /></template>重试任务</AppButton><AppButton @click="detailOpen = false">关闭</AppButton></template>
    </BaseDialog>
    <BaseDialog v-model="writebackActionOpen" :title="writebackAction === 'retry' ? '重试 Tag 写回' : '取消 Tag 写回'" :description="selectedWriteback?.id"><div><label class="ui-label">操作原因</label><input v-model="writebackReason" class="ui-input" /></div><template #footer><AppButton @click="writebackActionOpen = false">取消</AppButton><AppButton :variant="writebackAction === 'cancel' ? 'danger' : 'primary'" :loading="writebackMutation.isPending.value" @click="writebackMutation.mutate()">确认</AppButton></template></BaseDialog>
  </div>
</template>
