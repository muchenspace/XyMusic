<script setup lang="ts">
import { AlertTriangle, ChevronRight, Clock3, Folder, FolderOpen, Pencil, Play, Plus, RefreshCw, Square, Trash2 } from "lucide-vue-next";
import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/vue-query";
import { computed, defineComponent, h, onBeforeUnmount, onMounted, reactive, ref, watch } from "vue";
import { z } from "zod";
import { serviceReadiness } from "@/api/client";
import { ApiError } from "@/shared/application/api-error";
import AppButton from "@/components/AppButton.vue";
import AppPagination from "@/components/AppPagination.vue";
import BaseDialog from "@/components/BaseDialog.vue";
import PageHeader from "@/components/PageHeader.vue";
import StatePanel from "@/components/StatePanel.vue";
import StatusBadge from "@/components/StatusBadge.vue";
import type { JobEventSubscription } from "@/features/jobs/application/job-admin-gateway";
import { useJobAdmin } from "@/app/services/jobs";
import { scanQueuePresentation, sourceScanProgress, sourceScanRefetchInterval, submittedScanUpdate } from "@/features/sources/application/scan-queue-health";
import type { SourceScanSubscription } from "@/features/sources/application/source-admin-gateway";
import type { LibrarySource, LibrarySourceInput, SourceScan, SourceScanPage } from "@/features/sources/domain/models";
import { useSourceAdmin } from "@/app/services/sources";
import { useUiStore } from "@/stores/ui";
import { formatDate, formatRelative } from "@/utils/format";

const queryClient = useQueryClient();
const ui = useUiStore();
const sourceAdmin = useSourceAdmin();
const jobAdmin = useJobAdmin();
const readinessQuery = useQuery({
  queryKey: ["service", "readiness"],
  queryFn: ({ signal }) => serviceReadiness(signal),
  refetchInterval: 5_000,
  retry: 1,
});
const workerAvailable = computed<boolean | null>(() =>
  readinessQuery.isError.value ? null : readinessQuery.data.value?.worker?.available ?? null,
);
const workerCanScan = computed(() =>
  !readinessQuery.isError.value &&
  readinessQuery.data.value?.status === "ready" &&
  workerAvailable.value === true,
);
const editorOpen = ref(false);
const deleteOpen = ref(false);
const browseOpen = ref(false);
const selected = ref<LibrarySource>();
const historySourceId = ref("");
const sourcePage = ref(1);
const sourcePageSize = ref(12);
const page = ref(1);
const pageSize = ref(12);
const browsePath = ref("");
const browseRequestPath = ref("");
const browsePage = ref(1);
const directoryPageSize = ref(100);
const archiveCatalog = ref(true);
const fieldErrors = ref<Record<string, string>>({});
const actionError = ref("");
const patternText = reactive({ include: "", exclude: "" });
const form = reactive<Omit<LibrarySourceInput, "scanIntervalMinutes"> & { scanIntervalMinutes: number | null | ""; id: string; expectedVersion: number }>({ id: "", expectedVersion: 0, name: "", path: "", mode: "READ_ONLY", enabled: true, scanOnStartup: true, scanIntervalMinutes: null, includePatterns: [], excludePatterns: [] });
let scanEvents: SourceScanSubscription | undefined;
let scanEventSourceId: string | undefined;
let jobEvents: JobEventSubscription | undefined;
let scanClock: number | undefined;
let processingRefreshTimer: number | undefined;
const scanNow = ref(Date.now());
const jobStreamConnected = ref(false);
type ScanSubmission = {
  sourceId: string;
  sourceName: string;
  requestedAt: string;
  phase: "SUBMITTING" | "QUEUED" | "FAILED";
  scan?: SourceScan;
  error?: string;
};
const scanSubmission = ref<ScanSubmission>();
const completedScanIds = new Set<string>();
const initializedScanSources = new Set<string>();
let allowEditorClose = false;
let allowDeleteClose = false;

const sourcesQuery = useQuery({
  queryKey: computed(() => ["admin", "sources", "list", sourcePage.value, sourcePageSize.value]),
  queryFn: ({ signal }) => sourceAdmin.list(sourcePage.value, sourcePageSize.value, signal),
  refetchInterval: 60_000,
});
watch([sourcePage, sourcePageSize], () => {
  historySourceId.value = "";
  page.value = 1;
});
watch(() => sourcesQuery.data.value?.items, (items) => {
  if (!items?.length) {
    historySourceId.value = "";
    page.value = 1;
  } else if (!items.some((source) => source.id === historySourceId.value)) {
    historySourceId.value = items[0]?.id ?? "";
    page.value = 1;
  }
}, { immediate: true });
const scansQuery = useQuery({
  queryKey: computed(() => ["admin", "sources", historySourceId.value, "scans", page.value, pageSize.value]),
  queryFn: ({ signal }) => sourceAdmin.listScans(historySourceId.value, page.value, pageSize.value, signal),
  enabled: computed(() => Boolean(historySourceId.value)),
  placeholderData: (previousData, previousQuery) => previousQuery?.queryKey[2] === historySourceId.value
    ? keepPreviousData(previousData)
    : undefined,
  refetchInterval: (query) => sourceScanRefetchInterval(
    scanEventSourceId,
    historySourceId.value,
    query.state.data?.items,
  ),
});
const processingQuery = useQuery({
  queryKey: computed(() => ["admin", "sources", historySourceId.value, "processing"]),
  queryFn: ({ signal }) => sourceAdmin.processing(historySourceId.value, signal),
  enabled: computed(() => Boolean(historySourceId.value)),
  refetchInterval: (query) => jobStreamConnected.value ? false : (query.state.data?.active ?? 0) > 0 ? 5_000 : 60_000,
});
const historySourceName = computed(() => sourcesQuery.data.value?.items.find((source) => source.id === historySourceId.value)?.name ?? "当前音源");
const activeScan = computed(() => scansQuery.data.value?.items.find((scan) =>
  scan.status === "PENDING" || scan.status === "RUNNING",
) ?? null);
const visibleScan = computed<SourceScan | null>(() => {
  if (activeScan.value) return activeScan.value;
  if (scanSubmission.value?.sourceId !== historySourceId.value) return null;
  return scanSubmission.value.scan ?? null;
});
const currentSubmission = computed(() =>
  scanSubmission.value?.sourceId === historySourceId.value ? scanSubmission.value : undefined,
);
const showingSubmission = computed(() => currentSubmission.value?.phase === "SUBMITTING");
const livePipelineVisible = computed(() => Boolean(currentSubmission.value || visibleScan.value));

function patterns(value: string): string[] { return value.split(/\r?\n/).map((item) => item.trim()).filter(Boolean); }
function openCreate(): void {
  Object.assign(form, { id: "", expectedVersion: 0, name: "", path: "", mode: "READ_ONLY", enabled: true, scanOnStartup: true, scanIntervalMinutes: null, includePatterns: [], excludePatterns: [] });
  Object.assign(patternText, { include: "", exclude: "" }); fieldErrors.value = {}; actionError.value = ""; editorOpen.value = true;
}
function openEdit(source: LibrarySource): void {
  selected.value = source;
  Object.assign(form, { id: source.id, expectedVersion: source.version, name: source.name, path: source.path, mode: source.mode, enabled: source.enabled, scanOnStartup: source.scanOnStartup, scanIntervalMinutes: source.scanIntervalMinutes, includePatterns: source.includePatterns, excludePatterns: source.excludePatterns });
  Object.assign(patternText, { include: source.includePatterns.join("\n"), exclude: source.excludePatterns.join("\n") }); fieldErrors.value = {}; actionError.value = ""; editorOpen.value = true;
}
function openBrowser(): void { browsePath.value = form.path; browseRequestPath.value = form.path; browsePage.value = 1; browseOpen.value = true; }
function askDelete(source: LibrarySource): void { selected.value = source; archiveCatalog.value = true; actionError.value = ""; deleteOpen.value = true; }

const schema = z.object({ name: z.string().trim().min(1, "请输入音源名称").max(120), path: z.string().trim().min(1, "请输入服务器目录"), mode: z.enum(["READ_ONLY", "READ_WRITE"]), scanIntervalMinutes: z.number().int().min(5).max(10_080).nullable() });
function input(): LibrarySourceInput { return { name: form.name.trim(), path: form.path.trim(), mode: form.mode, enabled: form.enabled, scanOnStartup: form.scanOnStartup, scanIntervalMinutes: form.scanIntervalMinutes === "" ? null : form.scanIntervalMinutes, includePatterns: patterns(patternText.include), excludePatterns: patterns(patternText.exclude) }; }
function validate(): boolean {
  const result = schema.safeParse(input()); fieldErrors.value = {};
  if (!result.success) for (const issue of result.error.issues) fieldErrors.value[issue.path.join(".")] = issue.message;
  return result.success;
}
async function refresh(): Promise<void> { await Promise.all([queryClient.invalidateQueries({ queryKey: ["admin", "sources"] }), queryClient.invalidateQueries({ queryKey: ["admin", "dashboard"] })]); }
async function refreshCatalog(): Promise<void> {
  await Promise.all([
    refresh(),
    queryClient.invalidateQueries({ queryKey: ["admin", "tracks"] }),
    queryClient.invalidateQueries({ queryKey: ["admin", "track"] }),
    queryClient.invalidateQueries({ queryKey: ["admin", "albums"] }),
    queryClient.invalidateQueries({ queryKey: ["admin", "artists"] }),
    queryClient.invalidateQueries({ queryKey: ["admin", "jobs"] }),
    queryClient.invalidateQueries({ queryKey: ["admin", "audit"] }),
  ]);
}
function upsertScan(sourceId: string, scan: SourceScan): void {
  queryClient.setQueriesData<SourceScanPage>({ queryKey: ["admin", "sources", sourceId, "scans"] }, (current) => {
    if (!current) return current;
    const found = current.items.some((item) => item.id === scan.id);
    return {
      ...current,
      items: found
        ? current.items.map((item) => item.id === scan.id ? scan : item)
        : current.page === 1
          ? [scan, ...current.items].slice(0, current.pageSize)
          : current.items,
      total: found || current.page !== 1 ? current.total : current.total + 1,
    };
  });
}
const saveMutation = useMutation({ mutationFn: () => sourceAdmin.save(form.id || null, input(), form.expectedVersion), onSuccess: async () => { allowEditorClose = true; editorOpen.value = false; ui.notify("success", form.id ? "音源配置已更新" : "音源已添加"); await refreshCatalog(); }, onError: (error) => { actionError.value = error instanceof ApiError ? error.message : "保存音源失败"; } });
function save(): void { if (validate()) { actionError.value = ""; saveMutation.mutate(); } }
const browseQuery = useQuery({
  queryKey: computed(() => ["admin", "sources", "browse", browseRequestPath.value, browsePage.value, directoryPageSize.value]),
  queryFn: ({ signal }) => sourceAdmin.browse(browseRequestPath.value, browsePage.value, directoryPageSize.value, signal),
  enabled: computed(() => browseOpen.value),
});
function browse(): void { browsePage.value = 1; browseRequestPath.value = browsePath.value.trim(); }
function openDirectory(path: string): void { browsePage.value = 1; browsePath.value = path; browseRequestPath.value = path; }
function changeSourcePageSize(value: number): void { sourcePageSize.value = value; sourcePage.value = 1; }
function changeScanPageSize(value: number): void { pageSize.value = value; page.value = 1; }
function changeDirectoryPageSize(value: number): void { directoryPageSize.value = value; browsePage.value = 1; }
function chooseDirectory(path: string): void { form.path = path; browseOpen.value = false; }
const deleteMutation = useMutation({ mutationFn: () => sourceAdmin.delete(selected.value!.id, selected.value!.version, archiveCatalog.value), onSuccess: async () => { allowDeleteClose = true; deleteOpen.value = false; ui.notify("success", "音源已移除"); await refreshCatalog(); }, onError: (error) => { actionError.value = error instanceof ApiError ? error.message : "移除音源失败"; } });
watch(editorOpen, (value) => { if (!value && saveMutation.isPending.value && !allowEditorClose) editorOpen.value = true; allowEditorClose = false; });
watch(deleteOpen, (value) => { if (!value && deleteMutation.isPending.value && !allowDeleteClose) deleteOpen.value = true; allowDeleteClose = false; });
function connectScan(sourceId: string, scanId: string): void {
  scanEvents?.close();
  scanEventSourceId = sourceId;
  scanEvents = sourceAdmin.watchScan(sourceId, scanId, (scan) => {
    upsertScan(sourceId, scan);
    if (scanSubmission.value?.sourceId === sourceId) {
      scanSubmission.value = { ...scanSubmission.value, phase: "QUEUED", scan };
    }
    if (["COMPLETED", "FAILED", "CANCELLED"].includes(scan.status)) {
      completedScanIds.add(scan.id);
      if (scanSubmission.value?.sourceId === sourceId) scanSubmission.value = undefined;
      scanEvents?.close(); scanEvents = undefined; scanEventSourceId = undefined;
      void refreshCatalog();
      if (scan.status === "COMPLETED") ui.notify("success", "扫描完成", "新曲目会立即显示；媒体处理中的曲目会在后台任务完成后变为可播放。");
    }
  }, () => {
    scanEvents?.close(); scanEvents = undefined; scanEventSourceId = undefined;
    void queryClient.invalidateQueries({ queryKey: ["admin", "sources", sourceId, "scans"] });
  });
}
function queueProcessingRefresh(): void {
  if (processingRefreshTimer !== undefined) return;
  processingRefreshTimer = window.setTimeout(() => {
    processingRefreshTimer = undefined;
    void queryClient.invalidateQueries({ queryKey: ["admin", "sources", historySourceId.value, "processing"] });
    void queryClient.invalidateQueries({ queryKey: ["admin", "jobs"] });
    void queryClient.invalidateQueries({ queryKey: ["admin", "dashboard"] });
  }, 350);
}
watch(() => scansQuery.data.value?.items, (items) => {
  const sourceId = historySourceId.value;
  const submission = scanSubmission.value;
  if (submission?.sourceId === sourceId) {
    const update = submittedScanUpdate(submission.scan?.id, items);
    if (update.found) {
      scanSubmission.value = update.scan
        ? { ...submission, phase: "QUEUED", scan: update.scan }
        : undefined;
    }
  }
  if (sourceId && !initializedScanSources.has(sourceId)) {
    initializedScanSources.add(sourceId);
    for (const scan of items ?? []) {
      if (["COMPLETED", "FAILED", "CANCELLED"].includes(scan.status)) completedScanIds.add(scan.id);
    }
    return;
  }
  const newlyCompleted = items?.filter((scan) =>
    ["COMPLETED", "FAILED", "CANCELLED"].includes(scan.status) && !completedScanIds.has(scan.id),
  ) ?? [];
  if (!newlyCompleted.length) return;
  newlyCompleted.forEach((scan) => completedScanIds.add(scan.id));
  void refreshCatalog();
});
onMounted(() => {
  scanClock = window.setInterval(() => { scanNow.value = Date.now(); }, 5_000);
  jobEvents = jobAdmin.watch(
    () => { jobStreamConnected.value = true; void processingQuery.refetch(); },
    queueProcessingRefresh,
    () => { jobStreamConnected.value = false; },
  );
});
onBeforeUnmount(() => {
  scanEvents?.close();
  scanEventSourceId = undefined;
  jobEvents?.close();
  if (scanClock !== undefined) window.clearInterval(scanClock);
  if (processingRefreshTimer !== undefined) window.clearTimeout(processingRefreshTimer);
});
const scanMutation = useMutation({
  mutationFn: (source: LibrarySource) => sourceAdmin.startScan(source.id),
  onMutate: (source) => {
    historySourceId.value = source.id;
    page.value = 1;
    scanSubmission.value = {
      sourceId: source.id,
      sourceName: source.name,
      requestedAt: new Date().toISOString(),
      phase: "SUBMITTING",
    };
  },
  onSuccess: async (scan, source) => {
    scanSubmission.value = {
      sourceId: source.id,
      sourceName: source.name,
      requestedAt: scan.createdAt,
      phase: "QUEUED",
      scan,
    };
    upsertScan(source.id, scan);
    connectScan(source.id, scan.id);
    void processingQuery.refetch();
    ui.notify("success", "扫描任务已提交", "状态面板将持续显示扫描和后续媒体处理进度。");
    await refresh();
  },
  onError: (error, source) => {
    const message = error instanceof ApiError ? error.message : "扫描请求失败";
    scanSubmission.value = {
      sourceId: source.id,
      sourceName: source.name,
      requestedAt: new Date().toISOString(),
      phase: "FAILED",
      error: message,
    };
    ui.notify("error", error instanceof ApiError && error.status === 503 ? "后台 Worker 不可用" : "无法启动扫描", message);
  },
});
const cancelMutation = useMutation({ mutationFn: (scan: SourceScan) => sourceAdmin.cancelScan(scan.rootId, scan.id), onSuccess: async () => { ui.notify("success", "扫描取消请求已提交"); await scansQuery.refetch(); }, onError: (error) => ui.notify("error", "取消扫描失败", error instanceof ApiError ? error.message : undefined) });
function progress(scan: SourceScan): number { return sourceScanProgress(scan); }
function scanHealth(scan: SourceScan) { return scanQueuePresentation(scan, workerAvailable.value, scanNow.value); }
function scanStatusLabel(scan: SourceScan): string {
  if (scan.status === "PENDING") return "扫描已排队";
  if (scan.status === "RUNNING") return "正在发现和分析文件";
  if (scan.status === "COMPLETED") return "扫描完成";
  if (scan.status === "FAILED") return "扫描失败";
  return "扫描已取消";
}
function elapsed(from: string, to?: string | null): string {
  const start = Date.parse(from);
  const end = to ? Date.parse(to) : scanNow.value;
  if (!Number.isFinite(start) || !Number.isFinite(end)) return "—";
  const seconds = Math.max(0, Math.floor((end - start) / 1_000));
  if (seconds < 60) return `${seconds} 秒`;
  const minutes = Math.floor(seconds / 60);
  const remainder = seconds % 60;
  return `${minutes} 分 ${remainder} 秒`;
}
function jobStage(status: string): string {
  if (status === "PENDING") return "等待媒体 Worker";
  if (status === "PROCESSING") return "媒体分析与转码中";
  if (status === "READY") return "处理完成";
  if (status === "FAILED") return "处理失败";
  return "已取消";
}
function jobProgress(status: string): number {
  if (status === "PENDING") return 12;
  if (status === "PROCESSING") return 62;
  return 100;
}
function sourceSubmitting(sourceId: string): boolean {
  return scanMutation.isPending.value && scanMutation.variables.value?.id === sourceId;
}

const ToggleSource = defineComponent({ inheritAttrs: false, props: { modelValue: Boolean, label: { type: String, required: true }, detail: { type: String, required: true } }, emits: ["update:modelValue"], setup: (props, { emit, attrs }) => () => h("label", { ...attrs, class: [attrs.class, "flex items-center justify-between gap-4 rounded-xl border border-[var(--border)] p-4"] }, [h("span", [h("span", { class: "block font-semibold" }, props.label), h("span", { class: "mt-1 block text-xs text-[var(--muted)]" }, props.detail)]), h("button", { type: "button", class: "switch", role: "switch", "aria-label": props.label, "aria-checked": String(props.modelValue), onClick: () => emit("update:modelValue", !props.modelValue) })]) });
</script>

<template>
  <div class="space-y-6 page-enter">
    <PageHeader title="音源与扫描" description="管理服务端音乐目录、访问模式、过滤规则和扫描任务。"><template #eyebrow>资料库输入</template><template #actions><AppButton variant="primary" @click="openCreate"><template #icon><Plus :size="16" /></template>添加音源</AppButton></template></PageHeader>
    <div v-if="readinessQuery.isError.value" class="flex items-center justify-between gap-4 rounded-2xl border border-amber-500/25 bg-amber-500/10 p-4 text-sm"><div class="flex gap-3"><AlertTriangle :size="18" class="shrink-0 text-amber-500" /><div><p class="font-bold">无法确认后台 Worker 状态</p><p class="mt-1 text-xs text-[var(--muted)]">为避免任务永久排队，扫描功能暂时停用。</p></div></div><AppButton @click="readinessQuery.refetch()">重试</AppButton></div>
    <div v-else-if="readinessQuery.data.value && !workerCanScan" class="flex gap-3 rounded-2xl border border-rose-500/25 bg-rose-500/10 p-4 text-sm"><AlertTriangle :size="18" class="shrink-0 text-[var(--danger)]" /><div><p class="font-bold text-[var(--danger)]">后台 Worker 不可用</p><p class="mt-1 text-xs text-[var(--muted)]">扫描不会再被静默加入无人消费的队列。请启动 Worker；服务恢复后此页面会自动解除限制。</p></div></div>
    <section>
      <div class="mb-4 flex items-center justify-between"><h2 class="font-bold">已配置音源</h2><span class="text-xs text-[var(--muted)]">{{ sourcesQuery.data.value?.total ?? 0 }} 个目录</span></div>
      <StatePanel v-if="sourcesQuery.isPending.value" state="loading" />
      <StatePanel v-else-if="sourcesQuery.isError.value" state="error" @retry="sourcesQuery.refetch()" />
      <div v-else-if="sourcesQuery.data.value?.items.length" class="grid gap-4 md:grid-cols-2 2xl:grid-cols-3">
        <article v-for="source in sourcesQuery.data.value.items" :key="source.id" class="ui-card p-5">
          <div class="flex items-start gap-3">
            <span class="grid h-11 w-11 place-items-center rounded-2xl bg-[var(--primary-soft)] text-[var(--primary)]"><FolderOpen :size="21" /></span>
            <div class="min-w-0 flex-1"><div class="flex items-center gap-2"><h3 class="truncate font-bold">{{ source.name }}</h3><StatusBadge :status="sourceSubmitting(source.id) ? 'PENDING' : source.status" :label="sourceSubmitting(source.id) ? '正在提交' : undefined" dot /></div><p class="mt-1 truncate font-mono text-xs text-[var(--muted)]" :title="source.path">{{ source.path }}</p></div>
            <button class="btn btn-ghost btn-icon" type="button" :aria-label="`编辑音源：${source.name}`" @click="openEdit(source)"><Pencil :size="15" /></button>
          </div>
          <div class="mt-5 grid grid-cols-4 divide-x divide-[var(--border)] rounded-xl bg-[var(--surface-muted)] py-3 text-center"><div><p class="font-bold">{{ source.fileCount }}</p><p class="text-[10px] text-[var(--muted)]">物理文件</p></div><div><p class="font-bold">{{ source.trackCount }}</p><p class="text-[10px] text-[var(--muted)]">曲目</p></div><div><p class="font-bold">{{ source.cueFileCount }}</p><p class="text-[10px] text-[var(--muted)]">CUE</p></div><div><p class="font-bold" :class="source.failedFileCount > 0 ? 'text-[var(--danger)]' : undefined">{{ source.failedFileCount }}</p><p class="text-[10px] text-[var(--muted)]">失败</p></div></div>
          <p v-if="source.lastError" class="mt-4 flex gap-2 rounded-xl bg-rose-500/8 p-3 text-xs text-[var(--danger)]"><AlertTriangle :size="15" />{{ source.lastError }}</p>
          <div class="mt-5 flex items-center justify-between"><span class="flex items-center gap-1.5 text-xs text-[var(--muted)]"><Clock3 :size="13" />{{ source.lastScanAt ? formatRelative(source.lastScanAt) : '尚未扫描' }}</span><div class="flex gap-1"><button class="btn btn-ghost btn-icon" type="button" :aria-label="`删除音源：${source.name}`" @click="askDelete(source)"><Trash2 :size="15" /></button><AppButton variant="primary" :disabled="!source.enabled || source.status === 'SCANNING' || scanMutation.isPending.value || !workerCanScan" :title="!workerCanScan ? '后台 Worker 不可用，暂时不能扫描' : scanMutation.isPending.value && !sourceSubmitting(source.id) ? '正在提交另一个音源的扫描任务' : undefined" :loading="sourceSubmitting(source.id)" @click="scanMutation.mutate(source)"><template #icon><Play :size="14" /></template>扫描</AppButton></div></div>
        </article>
      </div>
      <div v-else class="ui-card"><StatePanel state="empty" title="还没有音源" detail="添加服务器可访问的音乐目录以开始扫描。" /></div>
      <AppPagination v-if="sourcesQuery.data.value?.total" :page="sourcePage" :page-size="sourcePageSize" :total="sourcesQuery.data.value.total" :total-pages="sourcesQuery.data.value.totalPages" @change="sourcePage = $event" @page-size-change="changeSourcePageSize" />
    </section>

    <section class="ui-card overflow-hidden">
      <div class="flex flex-col gap-3 border-b border-[var(--border)] px-5 py-4 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <div class="flex flex-wrap items-center gap-2"><h2 class="font-bold">实时处理流水线</h2><StatusBadge :status="jobStreamConnected ? 'ONLINE' : 'OFFLINE'" :label="jobStreamConnected ? '实时更新' : '轮询更新'" dot /></div>
          <p class="mt-1 text-xs text-[var(--muted)]">从扫描提交、文件发现到媒体分析与转码，按实际阶段连续展示。</p>
        </div>
        <div class="flex gap-2"><select v-model="historySourceId" class="ui-select min-w-48" @change="page = 1"><option v-for="source in sourcesQuery.data.value?.items" :key="source.id" :value="source.id">{{ source.name }}</option></select><AppButton icon-only :loading="processingQuery.isFetching.value" @click="processingQuery.refetch()"><template #icon><RefreshCw :size="16" /></template>刷新</AppButton></div>
      </div>
      <StatePanel v-if="!historySourceId" state="empty" compact title="请先添加音源" />
      <template v-else>
        <div v-if="livePipelineVisible" class="border-b border-[var(--border)] bg-[var(--surface-muted)] p-5" aria-live="polite">
          <div v-if="showingSubmission" class="flex flex-col gap-4 sm:flex-row sm:items-center">
            <span class="grid h-11 w-11 shrink-0 place-items-center rounded-full bg-[var(--primary-soft)] text-[var(--primary)]"><RefreshCw :size="19" class="animate-spin" /></span>
            <div class="min-w-0 flex-1"><div class="flex flex-wrap items-center gap-2"><p class="font-bold">正在提交扫描任务</p><StatusBadge status="PENDING" label="连接服务端" dot /></div><p class="mt-1 truncate text-xs text-[var(--muted)]">{{ currentSubmission?.sourceName }} · 请求已发出，页面无需等待返回即可看到此状态</p></div>
            <p class="text-xs font-semibold text-[var(--muted)]">{{ elapsed(currentSubmission!.requestedAt) }}</p>
          </div>
          <div v-else-if="currentSubmission?.phase === 'FAILED'" class="flex gap-3 rounded-xl bg-rose-500/10 p-4 text-sm text-[var(--danger)]"><AlertTriangle :size="18" class="shrink-0" /><div><p class="font-bold">扫描提交失败</p><p class="mt-1 text-xs">{{ currentSubmission.error }}</p></div></div>
          <div v-else-if="visibleScan" class="space-y-4">
            <div class="flex flex-col gap-3 sm:flex-row sm:items-center"><div class="min-w-0 flex-1"><div class="flex flex-wrap items-center gap-2"><p class="font-bold">{{ scanStatusLabel(visibleScan) }}</p><StatusBadge :status="scanHealth(visibleScan).status" :label="scanHealth(visibleScan).label ?? undefined" dot /></div><p class="mt-1 text-xs text-[var(--muted)]">已处理 {{ visibleScan.processedFiles }} / {{ visibleScan.discoveredFiles }} 个文件 · 失败 {{ visibleScan.failedFiles }} · 已用时 {{ elapsed(visibleScan.startedAt ?? visibleScan.createdAt, visibleScan.completedAt) }}</p></div><span class="text-2xl font-extrabold text-[var(--primary)]">{{ progress(visibleScan) }}%</span></div>
            <div class="h-2 overflow-hidden rounded-full bg-[var(--surface-solid)]"><div class="progress-fill h-full rounded-full bg-[var(--primary)]" :class="visibleScan.status === 'RUNNING' ? 'animate-pulse' : ''" :style="{ width: `${Math.max(visibleScan.status === 'PENDING' ? 4 : 0, progress(visibleScan))}%` }" /></div>
            <p v-if="scanHealth(visibleScan).warning" class="text-xs text-[var(--danger)]">{{ scanHealth(visibleScan).warning }}</p>
          </div>
        </div>
        <StatePanel v-if="processingQuery.isPending.value" state="loading" compact />
        <StatePanel v-else-if="processingQuery.isError.value" state="error" compact @retry="processingQuery.refetch()" />
        <template v-else-if="processingQuery.data.value">
          <div class="grid grid-cols-2 gap-px bg-[var(--border)] lg:grid-cols-4"><div class="bg-[var(--surface-solid)] p-5"><p class="text-xs font-semibold text-[var(--muted)]">媒体处理中</p><p class="mt-2 text-2xl font-bold">{{ processingQuery.data.value.active }}</p><p class="mt-1 text-[10px] text-[var(--muted)]">排队 {{ processingQuery.data.value.queued }} · 执行 {{ processingQuery.data.value.processing }}</p></div><div class="bg-[var(--surface-solid)] p-5"><p class="text-xs font-semibold text-[var(--muted)]">已完成</p><p class="mt-2 text-2xl font-bold text-emerald-500">{{ processingQuery.data.value.completed }}</p><p class="mt-1 text-[10px] text-[var(--muted)]">可播放变体已就绪</p></div><div class="bg-[var(--surface-solid)] p-5"><p class="text-xs font-semibold text-[var(--muted)]">失败</p><p class="mt-2 text-2xl font-bold" :class="processingQuery.data.value.failed ? 'text-[var(--danger)]' : undefined">{{ processingQuery.data.value.failed }}</p><p class="mt-1 text-[10px] text-[var(--muted)]">取消 {{ processingQuery.data.value.cancelled }}</p></div><div class="bg-[var(--surface-solid)] p-5"><p class="text-xs font-semibold text-[var(--muted)]">总任务</p><p class="mt-2 text-2xl font-bold">{{ processingQuery.data.value.total }}</p><p class="mt-1 truncate text-[10px] text-[var(--muted)]">{{ historySourceName }} · {{ processingQuery.data.value.updatedAt ? formatRelative(processingQuery.data.value.updatedAt) : '等待任务' }}</p></div></div>
          <StatePanel v-if="!processingQuery.data.value.jobs.length" state="empty" compact title="尚未产生媒体任务" detail="扫描阶段已经单独显示；发现新音频后，媒体分析与转码任务会立即出现在这里。" />
          <div v-else class="divide-y divide-[var(--border)]"><article v-for="job in processingQuery.data.value.jobs" :key="job.id" class="px-5 py-4"><div class="flex flex-col gap-3 sm:flex-row sm:items-start"><div class="min-w-0 flex-1"><div class="flex flex-wrap items-center gap-2"><p class="max-w-2xl truncate font-semibold">{{ job.title }}</p><StatusBadge :status="job.status" :label="jobStage(job.status)" dot /></div><p class="mt-1 text-xs text-[var(--muted)]">已用时 {{ elapsed(job.createdAt, ['READY','FAILED','CANCELLED'].includes(job.status) ? job.updatedAt : null) }} · 尝试 {{ job.attempts }} / {{ job.maxAttempts }} · 更新于 {{ formatRelative(job.updatedAt) }}</p><div class="mt-3 h-1.5 overflow-hidden rounded-full bg-[var(--surface-muted)]"><div class="progress-fill h-full rounded-full" :class="job.status === 'FAILED' ? 'bg-[var(--danger)]' : job.status === 'READY' ? 'bg-emerald-500' : 'animate-pulse bg-[var(--primary)]'" :style="{ width: `${jobProgress(job.status)}%` }" /></div><p v-if="job.lastError" class="mt-2 line-clamp-2 rounded-lg bg-rose-500/8 p-2 text-xs text-[var(--danger)]">{{ job.lastError }}</p></div></div></article></div>
        </template>
      </template>
    </section>

    <section class="ui-card overflow-hidden" :class="{ 'data-refreshing': scansQuery.isFetching.value && !scansQuery.isPending.value }" :aria-busy="scansQuery.isFetching.value"><div class="flex flex-col gap-3 border-b border-[var(--border)] px-5 py-4 sm:flex-row sm:items-center sm:justify-between"><div><h2 class="font-bold">扫描记录</h2><p class="mt-1 text-xs text-[var(--muted)]">每个音源独立保留扫描历史</p></div><AppButton icon-only :loading="scansQuery.isFetching.value" @click="scansQuery.refetch()"><template #icon><RefreshCw :size="16" /></template>刷新</AppButton></div><StatePanel v-if="!historySourceId" state="empty" compact title="请先添加音源" /><StatePanel v-else-if="scansQuery.isPending.value" state="loading" compact /><StatePanel v-else-if="scansQuery.isError.value" state="error" compact @retry="scansQuery.refetch()" /><StatePanel v-else-if="!scansQuery.data.value?.items.length" state="empty" compact title="尚无扫描记录" /><template v-else><div class="overflow-x-auto"><table class="data-table min-w-[760px]"><thead><tr><th>状态</th><th>进度</th><th>发现文件</th><th>失败文件</th><th>开始时间</th><th>操作</th></tr></thead><tbody><tr v-for="scan in scansQuery.data.value.items" :key="scan.id"><td class="max-w-64"><StatusBadge :status="scanHealth(scan).status" :label="scanHealth(scan).label ?? undefined" dot /><p v-if="scanHealth(scan).warning" class="mt-1 text-[10px] leading-4 text-[var(--danger)]">{{ scanHealth(scan).warning }}</p></td><td class="w-56"><div class="flex items-center gap-3"><div class="h-1.5 flex-1 overflow-hidden rounded-full bg-[var(--surface-muted)]"><div class="progress-fill h-full bg-[var(--primary)]" :style="{ width: `${progress(scan)}%` }" /></div><span class="text-xs font-semibold">{{ progress(scan) }}%</span></div></td><td>{{ scan.processedFiles }} / {{ scan.discoveredFiles }}</td><td :class="scan.failedFiles > 0 ? 'text-[var(--danger)] font-semibold' : undefined">{{ scan.failedFiles }}</td><td class="text-xs text-[var(--muted)]">{{ formatDate(scan.startedAt ?? scan.createdAt) }}</td><td><button v-if="['PENDING','RUNNING'].includes(scan.status)" class="btn btn-danger" type="button" :disabled="cancelMutation.isPending.value" @click="cancelMutation.mutate(scan)"><Square :size="13" />取消</button><span v-else class="text-xs text-[var(--muted)]">—</span></td></tr></tbody></table></div><AppPagination :page="page" :page-size="pageSize" :total="scansQuery.data.value.total" @change="page = $event" @page-size-change="changeScanPageSize" /></template></section>

    <BaseDialog v-model="editorOpen" :title="form.id ? '编辑音源' : '添加音乐音源'" description="保存时由服务端验证目录权限与过滤规则。" width="lg"><div class="grid gap-5 sm:grid-cols-2"><div><label class="ui-label">音源名称</label><input v-model="form.name" class="ui-input" /><p v-if="fieldErrors.name" class="ui-error">{{ fieldErrors.name }}</p></div><div><label class="ui-label">访问模式</label><select v-model="form.mode" class="ui-select"><option value="READ_ONLY">只读</option><option value="READ_WRITE">读写（Tag 修改或刮削时可选择写回）</option></select></div><div class="sm:col-span-2"><p class="rounded-xl bg-[var(--surface-muted)] p-3 text-xs leading-5 text-[var(--muted)]">只读模式不会修改音源；读写模式允许在 Tag 修改或刮削操作中按次选择写回。</p></div><div class="sm:col-span-2"><label class="ui-label">服务端目录</label><div class="flex gap-2"><input v-model="form.path" class="ui-input font-mono" placeholder="music、D:\Music 或 /srv/music" /><AppButton @click="openBrowser"><template #icon><Folder :size="15" /></template>浏览</AppButton></div><p class="mt-1 text-xs leading-5 text-[var(--muted)]">支持相对或绝对路径；相对路径以服务端二进制文件所在目录为基准。</p><p v-if="fieldErrors.path" class="ui-error">{{ fieldErrors.path }}</p></div><ToggleSource v-model="form.enabled" label="启用音源" detail="停用后不能启动扫描" /><ToggleSource v-model="form.scanOnStartup" label="启动时扫描" detail="服务启动后自动创建扫描任务" /><div><label class="ui-label">定时扫描间隔（分钟）</label><input v-model.number="form.scanIntervalMinutes" class="ui-input" type="number" min="5" max="10080" placeholder="留空则关闭" /><p v-if="fieldErrors.scanIntervalMinutes" class="ui-error">{{ fieldErrors.scanIntervalMinutes }}</p></div><div /><div><label class="ui-label">包含规则</label><textarea v-model="patternText.include" class="ui-textarea font-mono" placeholder="每行一个 Glob；留空包含全部支持格式" /></div><div><label class="ui-label">排除规则</label><textarea v-model="patternText.exclude" class="ui-textarea font-mono" placeholder="例如：**/Temp/**" /></div></div><p v-if="actionError" class="mt-5 rounded-xl bg-rose-500/10 p-3 text-sm text-[var(--danger)]">{{ actionError }}</p><template #footer><AppButton @click="editorOpen = false">取消</AppButton><AppButton variant="primary" :loading="saveMutation.isPending.value" @click="save">保存音源</AppButton></template></BaseDialog>

    <BaseDialog v-model="browseOpen" title="浏览服务器目录" description="目录列表来自 XyMusic 服务端；相对路径以服务端二进制文件所在目录为基准，也支持绝对路径。" width="lg"><div class="flex gap-2"><input v-model="browsePath" class="ui-input font-mono" placeholder="music 或 D:\Music" @keydown.enter="browse" /><AppButton :loading="browseQuery.isFetching.value" @click="browse">打开</AppButton></div><StatePanel v-if="browseQuery.isPending.value" state="loading" compact /><StatePanel v-else-if="browseQuery.isError.value" state="error" compact @retry="browseQuery.refetch()" /><div v-else-if="browseQuery.data.value" class="mt-4"><button class="mb-3 flex w-full items-center justify-between rounded-xl bg-[var(--primary-soft)] p-3 text-left font-mono text-xs text-[var(--primary)]" type="button" @click="chooseDirectory(browseQuery.data.value.path)"><span class="truncate">选择 {{ browseQuery.data.value.path }}</span><ChevronRight :size="15" /></button><div class="max-h-80 divide-y divide-[var(--border)] overflow-y-auto rounded-xl border border-[var(--border)]"><button v-for="directory in browseQuery.data.value.directories" :key="directory.path" class="flex w-full items-center gap-3 p-3 text-left hover:bg-[var(--surface-muted)]" type="button" @click="openDirectory(directory.path)"><Folder :size="16" class="text-[var(--primary)]" /><span class="truncate">{{ directory.name }}</span></button></div><AppPagination v-if="browseQuery.data.value.total" :page="browsePage" :page-size="directoryPageSize" :total="browseQuery.data.value.total" :total-pages="browseQuery.data.value.totalPages" @change="browsePage = $event" @page-size-change="changeDirectoryPageSize" /></div></BaseDialog>

    <BaseDialog v-model="deleteOpen" title="移除音源" description="磁盘上的媒体文件不会被删除。"><div class="rounded-xl bg-[var(--surface-muted)] p-4"><p class="font-semibold">{{ selected?.name }}</p><p class="mt-1 break-all font-mono text-xs text-[var(--muted)]">{{ selected?.path }}</p></div><ToggleSource v-model="archiveCatalog" class="mt-4" label="归档关联曲目" detail="将该音源已导入的曲目标记为已归档" /><p v-if="actionError" class="mt-4 rounded-xl bg-rose-500/10 p-3 text-sm text-[var(--danger)]">{{ actionError }}</p><template #footer><AppButton @click="deleteOpen = false">取消</AppButton><AppButton variant="danger" :loading="deleteMutation.isPending.value" @click="deleteMutation.mutate()">移除音源</AppButton></template></BaseDialog>
  </div>
</template>
