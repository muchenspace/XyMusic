<script setup lang="ts">
import { computed, onUnmounted, reactive, ref, watch } from "vue";
import AppButton from "@/components/AppButton.vue";
import BaseDialog from "@/components/BaseDialog.vue";
import StatusBadge from "@/components/StatusBadge.vue";
import type { TrackSummary } from "@/features/music/domain/models";
import {
  defaultScrapingFields,
  type MatchMode,
  type TagScrapingBatch,
  type TagScrapingMissingField,
  type TagSource,
} from "@/features/scraping/domain/models";
import {
  assertWritebackAllowed,
  batchWritebackCapability,
  batchWritebackHint,
  useWritebackSelection,
} from "@/features/music/presentation/writeback-capability";
import {
  batchItemMessage,
  batchItemStatusPresentation,
  batchJobStatusPresentation,
  isNoScrapingNeededError,
  noScrapingNeededDetail,
} from "@/features/scraping/presentation/batch-status";
import { useTagScraping } from "@/app/services/scraping";

const open = defineModel<boolean>({ required: true });
const props = defineProps<{ tracks: TrackSummary[] }>();
const emit = defineEmits<{ completed: [] }>();
const scraping = useTagScraping();
const sources = ref<TagSource[]>(["qmusic", "netease", "migu", "kugou"]);
const matchMode = ref<MatchMode>("strict");
const missingFields = ref<TagScrapingMissingField[]>([]);
const fields = reactive(defaultScrapingFields());
const archivedTrack = computed(() => props.tracks.find((track) => track.status === "ARCHIVED"));
const writebackCapability = computed(() => batchWritebackCapability(props.tracks));
const hasMissingFieldFilter = computed(() => missingFields.value.length > 0);
const writebackControlCapability = computed(() => hasMissingFieldFilter.value
  ? { canWriteBack: props.tracks.length > 0, blockReason: props.tracks.length ? null : "未选择曲目" }
  : writebackCapability.value);
const writebackHint = computed(() => hasMissingFieldFilter.value
  ? "后端将只校验实际进入任务的曲目；若其中存在不可写音源，任务会返回明确错误。"
  : batchWritebackHint(writebackCapability.value));
const writeBack = useWritebackSelection(writebackControlCapability);
const reason = ref("批量在线 Tag 刮削");
const job = ref<TagScrapingBatch>();
const loading = ref(false);
const error = ref("");
const notice = ref("");
const submittedCount = ref(0);
const conditionExcluded = ref(0);
let timer: ReturnType<typeof setTimeout> | undefined;
let pollController: AbortController | undefined;
let pollGeneration = 0;
let pollFailures = 0;
let completedEmitted = false;
let actionGeneration = 0;
const sourceOptions: Array<{ value: TagSource; label: string }> = [{ value: "qmusic", label: "QQ 音乐" }, { value: "netease", label: "网易云" }, { value: "migu", label: "咪咕" }, { value: "kugou", label: "酷狗" }, { value: "kuwo", label: "酷我" }];

watch(open, (value) => {
  actionGeneration += 1;
  stopPolling();
  if (value) { job.value = undefined; writeBack.value = false; error.value = ""; notice.value = ""; submittedCount.value = 0; conditionExcluded.value = 0; completedEmitted = false; }
});
onUnmounted(() => {
  actionGeneration += 1;
  stopPolling();
});
function stopPolling() {
  pollGeneration += 1;
  if (timer) clearTimeout(timer);
  timer = undefined;
  pollController?.abort();
  pollController = undefined;
}
function schedulePolling(id: string, generation: number, delay = 2000) {
  if (!open.value || generation !== pollGeneration) return;
  if (timer) clearTimeout(timer);
  timer = setTimeout(() => void refresh(id, generation), delay);
}
function beginPolling(id: string) {
  stopPolling();
  completedEmitted = false;
  pollFailures = 0;
  schedulePolling(id, pollGeneration);
}
function finishIfTerminal(update: TagScrapingBatch): boolean {
  if (["PENDING", "RUNNING"].includes(update.status)) return false;
  if (!completedEmitted) { completedEmitted = true; emit("completed"); }
  return true;
}
async function refresh(id = job.value?.id, generation = pollGeneration) {
  if (!id || generation !== pollGeneration || !open.value) return;
  if (timer) clearTimeout(timer);
  timer = undefined;
  const controller = new AbortController();
  pollController = controller;
  try {
    const current = job.value;
    const update = await scraping.batch(id, current?.updatedAt, controller.signal);
    if (generation !== pollGeneration || !open.value) return;
    pollFailures = 0;
    error.value = "";
    if (current && update.partialItems) {
      const changed = new Map(update.items.map((item) => [item.id, item]));
      job.value = { ...update, items: current.items.map((item) => changed.get(item.id) ?? item) };
    } else job.value = update;
    if (!finishIfTerminal(job.value)) schedulePolling(id, generation);
  } catch (cause) {
    if (controller.signal.aborted || generation !== pollGeneration || !open.value) return;
    error.value = cause instanceof Error ? cause.message : "读取任务失败";
    pollFailures += 1;
    schedulePolling(id, generation, Math.min(10_000, 2_000 * 2 ** pollFailures));
  } finally {
    if (pollController === controller) pollController = undefined;
  }
}
async function start() {
  if (archivedTrack.value) { error.value = `已归档曲目“${archivedTrack.value.title}”需先恢复后才能批量刮削`; return; }
  if (!sources.value.length) { error.value = "至少选择一个来源"; return; }
  const invalid = props.tracks.find((track) => track.metadataVersion === null);
  if (invalid) { error.value = `曲目“${invalid.title}”缺少 Tag 版本，请刷新后重试`; return; }
  try {
    assertWritebackAllowed(writeBack.value, writebackControlCapability.value);
  } catch (cause) {
    writeBack.value = false;
    error.value = cause instanceof Error ? cause.message : "当前选择不能写回源文件 Tag";
    return;
  }
  loading.value = true; error.value = ""; notice.value = ""; conditionExcluded.value = 0;
  const generation = pollGeneration;
  const action = ++actionGeneration;
  const selectedCount = props.tracks.length;
  submittedCount.value = selectedCount;
  try {
    const created = await scraping.createBatch({
      items: props.tracks.map((track) => ({ trackId: track.id, expectedVersion: track.metadataVersion! })),
      options: {
        sources: sources.value,
        matchMode: matchMode.value,
        missingFields: missingFields.value,
        fields: { ...fields },
        writeBack: writeBack.value,
        reason: reason.value.trim() || "批量在线 Tag 刮削",
      },
    });
    if (generation !== pollGeneration || action !== actionGeneration || !open.value) return;
    conditionExcluded.value = Math.max(0, selectedCount - created.total);
    job.value = created;
    if (!finishIfTerminal(created)) beginPolling(created.id);
  } catch (cause) {
    if (action === actionGeneration && open.value) {
      if (isNoScrapingNeededError(cause)) {
        conditionExcluded.value = selectedCount;
        notice.value = noScrapingNeededDetail(cause);
      } else error.value = cause instanceof Error ? cause.message : "创建任务失败";
    }
  } finally {
    if (action === actionGeneration) loading.value = false;
  }
}
async function cancel() {
  if (!job.value) return;
  const id = job.value.id;
  loading.value = true;
  stopPolling();
  const generation = pollGeneration;
  const action = ++actionGeneration;
  try {
    const update = await scraping.cancelBatch(id);
    if (generation !== pollGeneration || action !== actionGeneration || !open.value) return;
    job.value = update;
    if (!finishIfTerminal(update)) beginPolling(id);
  } catch (cause) {
    if (action === actionGeneration && open.value) error.value = cause instanceof Error ? cause.message : "取消任务失败";
  } finally {
    if (action === actionGeneration) loading.value = false;
  }
}
async function retry() {
  if (!job.value) return;
  const id = job.value.id;
  loading.value = true;
  stopPolling();
  const generation = pollGeneration;
  const action = ++actionGeneration;
  try {
    const update = await scraping.retryBatch(id);
    if (generation !== pollGeneration || action !== actionGeneration || !open.value) return;
    job.value = update;
    if (!finishIfTerminal(update)) beginPolling(id);
  } catch (cause) {
    if (action === actionGeneration && open.value) error.value = cause instanceof Error ? cause.message : "重试失败";
  } finally {
    if (action === actionGeneration) loading.value = false;
  }
}
</script>

<template>
  <BaseDialog v-model="open" :title="`批量刮削 ${submittedCount || tracks.length} 首曲目`" description="匹配、歌词、封面和字段应用全部由后端执行，关闭窗口不会中止任务。" width="xl">
    <template v-if="!job">
      <p class="ui-label">来源优先级（按显示顺序）</p><div class="flex flex-wrap gap-2"><label v-for="source in sourceOptions" :key="source.value" class="flex items-center gap-2 rounded-lg border border-[var(--border)] px-3 py-2 text-sm"><input v-model="sources" type="checkbox" :value="source.value" />{{ source.label }}</label></div>
      <div class="mt-5 grid gap-4 sm:grid-cols-2"><div><label class="ui-label">匹配模式</label><select v-model="matchMode" class="ui-select"><option value="strict">严格匹配</option><option value="simple">宽松匹配</option></select></div><div><label class="ui-label">任务原因</label><input v-model="reason" class="ui-input" /></div></div>
      <p class="ui-label mt-5">仅刮削缺失以下字段的曲目</p><div class="grid grid-cols-2 gap-2 sm:grid-cols-3"><label v-for="item in [{k:'artist',l:'主要艺术家'},{k:'album',l:'专辑'},{k:'year',l:'发行年份'},{k:'genre',l:'流派'},{k:'lyrics',l:'歌词'},{k:'cover',l:'封面'}]" :key="item.k" class="flex items-center gap-2 rounded-lg border border-[var(--border)] p-2 text-sm"><input v-model="missingFields" type="checkbox" :value="item.k" />无{{ item.l }}</label></div><p class="mt-2 text-xs text-[var(--muted)]">不选择时刮削全部选中曲目；选择多项时满足任一条件即可。</p>
      <p class="ui-label mt-5">应用字段</p><div class="grid grid-cols-2 gap-2 sm:grid-cols-4"><label v-for="item in [{k:'title',l:'标题'},{k:'artist',l:'艺术家'},{k:'album',l:'专辑'},{k:'year',l:'年份'},{k:'genre',l:'流派'},{k:'lyrics',l:'歌词'},{k:'cover',l:'专辑封面'}]" :key="item.k" class="flex items-center gap-2 rounded-lg border border-[var(--border)] p-2 text-sm"><input v-model="fields[item.k as keyof typeof fields]" type="checkbox" />{{ item.l }}</label></div>
      <label class="mt-4 flex items-center gap-2 text-sm"><input v-model="fields.overwrite" type="checkbox" />覆盖已有字段</label>
      <label class="mt-4 flex items-start gap-3 rounded-xl border border-[var(--border)] p-4 text-sm" :class="!writebackControlCapability.canWriteBack && 'opacity-75'"><input v-model="writeBack" data-testid="batch-writeback" class="mt-0.5" type="checkbox" :disabled="!writebackControlCapability.canWriteBack" /><span><span class="block font-semibold">写回源文件 Tag</span><span class="mt-1 block text-xs leading-5 text-[var(--muted)]">{{ writebackHint }}</span></span></label>
    </template>
    <template v-else>
      <div class="rounded-xl bg-[var(--surface-muted)] p-4"><div class="flex items-center justify-between gap-3"><StatusBadge :status="job.status" :label="batchJobStatusPresentation(job.status).label" :tone="batchJobStatusPresentation(job.status).tone" dot /><span>{{ job.processed }} / {{ job.total }}</span></div><div class="mt-3 h-2 overflow-hidden rounded bg-[var(--surface-solid)]"><div class="progress-fill h-full bg-violet-500" :style="{ width: `${job.total ? job.processed / job.total * 100 : 0}%` }" /></div><p class="mt-2 text-xs text-[var(--muted)]">条件排除 {{ conditionExcluded }} · 成功 {{ job.succeeded }} · 已跳过 {{ job.skipped }} · 失败 {{ job.failed }}</p><p class="mt-1 text-[10px] text-[var(--muted)]">共选择 {{ submittedCount }} 首，{{ job.total }} 首进入刮削任务</p></div>
      <div class="mt-4 max-h-80 overflow-y-auto rounded-xl border border-[var(--border)]"><div v-for="item in job.items" :key="item.id" v-memo="[item.status, item.source, item.message]" class="flex items-center justify-between gap-3 border-b border-[var(--border)] p-3 text-sm last:border-0"><span>#{{ item.position + 1 }} · {{ item.source ?? (item.status === 'PENDING' || item.status === 'RUNNING' ? '等待来源' : '未使用来源') }}</span><span class="flex items-center gap-2 text-right"><StatusBadge data-testid="batch-item-status" :status="item.status" :label="batchItemStatusPresentation(item.status).label" :tone="batchItemStatusPresentation(item.status).tone" /><span class="text-[var(--muted)]">{{ batchItemMessage(item.status, item.message) }}</span></span></div></div>
    </template>
    <div v-if="notice" class="mt-4 rounded-xl border border-[var(--border)] bg-[var(--surface-muted)] p-4 text-sm"><p class="font-semibold">无需刮削</p><p class="mt-1 text-xs leading-5 text-[var(--muted)]">{{ notice }}</p><p class="mt-2 text-xs font-semibold text-[var(--muted)]">条件排除 {{ conditionExcluded }} 首</p></div>
    <p v-if="archivedTrack" class="mt-4 rounded-xl bg-amber-500/10 p-3 text-sm text-amber-700 dark:text-amber-300">已归档曲目“{{ archivedTrack.title }}”需先恢复后才能批量刮削。</p>
    <p v-if="error" class="mt-4 rounded-xl bg-rose-500/10 p-3 text-sm text-[var(--danger)]">{{ error }}</p>
    <template #footer><AppButton @click="open = false">关闭</AppButton><AppButton v-if="!job" variant="primary" :loading="loading" :disabled="Boolean(archivedTrack)" @click="start">开始刮削</AppButton><AppButton v-else-if="job.status === 'PENDING' || job.status === 'RUNNING'" variant="danger" :loading="loading" @click="cancel">停止任务</AppButton><AppButton v-else-if="job.failed" variant="primary" :loading="loading" @click="retry">重试失败项</AppButton></template>
  </BaseDialog>
</template>
