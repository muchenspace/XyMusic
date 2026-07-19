<script setup lang="ts">
import { onUnmounted, ref, watch } from "vue";
import AppButton from "@/components/AppButton.vue";
import BaseDialog from "@/components/BaseDialog.vue";
import StatusBadge from "@/components/StatusBadge.vue";
import type { ArtistSummary } from "@/features/music/domain/models";
import type { ArtistArtworkBatch, TagSource } from "@/features/scraping/domain/models";
import {
  batchItemMessage,
  batchItemStatusPresentation,
  batchJobStatusPresentation,
} from "@/features/scraping/presentation/batch-status";
import { useTagScraping } from "@/app/services/scraping";

const open = defineModel<boolean>({ required: true });
const props = defineProps<{ artists: ArtistSummary[] }>();
const emit = defineEmits<{ completed: [] }>();
const scraping = useTagScraping();

const sources = ref<TagSource[]>(["qmusic", "netease"]);
const reason = ref("批量在线刮削艺术家头像");
const job = ref<ArtistArtworkBatch>();
const loading = ref(false);
const error = ref("");
const notice = ref("");
const submittedCount = ref(0);
const conditionExcluded = ref(0);
const artistNames = ref(new Map<string, string>());
let timer: ReturnType<typeof setTimeout> | undefined;
let pollController: AbortController | undefined;
let pollGeneration = 0;
let pollFailures = 0;
let completedEmitted = false;
let actionGeneration = 0;

const sourceOptions: Array<{ value: TagSource; label: string }> = [
  { value: "qmusic", label: "QQ 音乐" },
  { value: "netease", label: "网易云" },
];

watch(open, (value) => {
  actionGeneration += 1;
  stopPolling();
  if (!value) return;
  if (job.value && (job.value.status === "PENDING" || job.value.status === "RUNNING")) {
    artistNames.value = new Map(props.artists.map((artist) => [artist.id, artist.name]));
    loading.value = false;
    error.value = "";
    beginPolling(job.value.id);
    return;
  }
  sources.value = ["qmusic", "netease"];
  reason.value = "批量在线刮削艺术家头像";
  job.value = undefined;
  loading.value = false;
  error.value = "";
  notice.value = "";
  submittedCount.value = 0;
  conditionExcluded.value = 0;
  artistNames.value = new Map(props.artists.map((artist) => [artist.id, artist.name]));
  completedEmitted = false;
}, { immediate: true });

onUnmounted(() => {
  actionGeneration += 1;
  stopPolling();
});

function stopPolling(): void {
  pollGeneration += 1;
  if (timer) clearTimeout(timer);
  timer = undefined;
  pollController?.abort();
  pollController = undefined;
}

function schedulePolling(id: string, generation: number, delay = 2_000): void {
  if (!open.value || generation !== pollGeneration) return;
  if (timer) clearTimeout(timer);
  timer = setTimeout(() => void refresh(id, generation), delay);
}

function beginPolling(id: string): void {
  stopPolling();
  completedEmitted = false;
  pollFailures = 0;
  schedulePolling(id, pollGeneration);
}

function finishIfTerminal(update: ArtistArtworkBatch): boolean {
  if (update.status === "PENDING" || update.status === "RUNNING") return false;
  if (!completedEmitted) {
    completedEmitted = true;
    emit("completed");
  }
  return true;
}

function artistName(artistId: string): string {
  return artistNames.value.get(artistId) ?? `艺术家 ${artistId.slice(0, 8)}`;
}

function mergeBatchUpdate(current: ArtistArtworkBatch | undefined, update: ArtistArtworkBatch): ArtistArtworkBatch {
  if (!current || !update.partialItems) return update;
  const items = new Map(current.items.map((item) => [item.id, item]));
  for (const item of update.items) items.set(item.id, item);
  return { ...update, items: [...items.values()].sort((left, right) => left.position - right.position) };
}

async function refresh(id = job.value?.id, generation = pollGeneration): Promise<void> {
  if (!id || generation !== pollGeneration || !open.value) return;
  if (timer) clearTimeout(timer);
  timer = undefined;
  const controller = new AbortController();
  pollController = controller;
  try {
    const update = await scraping.artistArtworkBatch(id, job.value?.updatedAt, controller.signal);
    if (generation !== pollGeneration || !open.value) return;
    pollFailures = 0;
    error.value = "";
    const merged = mergeBatchUpdate(job.value, update);
    job.value = merged;
    if (!finishIfTerminal(merged)) schedulePolling(id, generation);
  } catch (cause) {
    if (controller.signal.aborted || generation !== pollGeneration || !open.value) return;
    error.value = cause instanceof Error ? cause.message : "读取头像刮削任务失败";
    pollFailures += 1;
    schedulePolling(id, generation, Math.min(10_000, 2_000 * 2 ** pollFailures));
  } finally {
    if (pollController === controller) pollController = undefined;
  }
}

async function start(): Promise<void> {
  if (!sources.value.length) {
    error.value = "至少选择一个来源";
    return;
  }
  const trimmedReason = reason.value.trim();
  if (trimmedReason.length < 2 || trimmedReason.length > 500) {
    error.value = "任务原因需为 2 至 500 个字符";
    return;
  }

  const selected = [...props.artists];
  const eligible = selected.filter((artist) => !artist.artwork);
  submittedCount.value = selected.length;
  conditionExcluded.value = selected.length - eligible.length;
  artistNames.value = new Map(selected.map((artist) => [artist.id, artist.name]));
  error.value = "";
  notice.value = "";

  if (!eligible.length) {
    notice.value = "所选艺术家均已有头像，无需创建刮削任务。";
    return;
  }
  if (eligible.length > 200) {
    error.value = "单次最多刮削 200 位艺术家，请减少选择后重试。";
    return;
  }

  loading.value = true;
  const generation = pollGeneration;
  const action = ++actionGeneration;
  try {
    const created = await scraping.createArtistArtworkBatch({
      items: eligible.map((artist) => ({ artistId: artist.id, expectedVersion: artist.version })),
      options: { sources: [...sources.value], overwrite: false, reason: trimmedReason },
    });
    if (generation !== pollGeneration || action !== actionGeneration || !open.value) return;
    conditionExcluded.value += created.conditionExcluded;
    if (!created.job) {
      notice.value = "符合条件的艺术家在任务创建前已被排除，无需刮削。";
      return;
    }
    job.value = created.job;
    if (!finishIfTerminal(created.job)) beginPolling(created.job.id);
  } catch (cause) {
    if (action === actionGeneration && open.value) {
      error.value = cause instanceof Error ? cause.message : "创建头像刮削任务失败";
    }
  } finally {
    if (action === actionGeneration) loading.value = false;
  }
}

async function cancel(): Promise<void> {
  if (!job.value) return;
  const id = job.value.id;
  loading.value = true;
  stopPolling();
  const generation = pollGeneration;
  const action = ++actionGeneration;
  try {
    const update = await scraping.cancelArtistArtworkBatch(id);
    if (generation !== pollGeneration || action !== actionGeneration || !open.value) return;
    job.value = update;
    if (!finishIfTerminal(update)) beginPolling(id);
  } catch (cause) {
    if (action === actionGeneration && open.value) {
      error.value = cause instanceof Error ? cause.message : "取消头像刮削任务失败";
    }
  } finally {
    if (action === actionGeneration) loading.value = false;
  }
}

async function retry(): Promise<void> {
  if (!job.value) return;
  const id = job.value.id;
  loading.value = true;
  stopPolling();
  const generation = pollGeneration;
  const action = ++actionGeneration;
  try {
    const update = await scraping.retryArtistArtworkBatch(id);
    if (generation !== pollGeneration || action !== actionGeneration || !open.value) return;
    job.value = update;
    if (!finishIfTerminal(update)) beginPolling(id);
  } catch (cause) {
    if (action === actionGeneration && open.value) {
      error.value = cause instanceof Error ? cause.message : "重试头像刮削任务失败";
    }
  } finally {
    if (action === actionGeneration) loading.value = false;
  }
}
</script>

<template>
  <BaseDialog
    v-model="open"
    :title="`批量刮削 ${submittedCount || artists.length} 位艺术家头像`"
    description="只提交当前缺少头像的艺术家；已有头像会在创建任务前排除。关闭窗口不会中止任务。"
    width="xl"
    :prevent-close="loading"
  >
    <template v-if="!job">
      <p class="ui-label">头像来源</p>
      <div class="flex flex-wrap gap-2">
        <label v-for="item in sourceOptions" :key="item.value" class="flex items-center gap-2 rounded-lg border border-[var(--border)] px-3 py-2 text-sm">
          <input v-model="sources" type="checkbox" :value="item.value" />
          {{ item.label }}
        </label>
      </div>
      <div class="mt-5">
        <label class="ui-label">任务原因</label>
        <input v-model="reason" class="ui-input" maxlength="500" />
      </div>
      <div class="mt-5 rounded-xl border border-[var(--border)] bg-[var(--surface-muted)] p-4 text-sm">
        <p class="font-semibold">默认仅补全缺失头像</p>
        <p class="mt-1 text-xs leading-5 text-[var(--muted)]">不会覆盖任何已有头像；低置信或无可靠候选会标记为“已跳过”，不会显示为失败。</p>
      </div>
    </template>

    <template v-else>
      <div class="rounded-xl bg-[var(--surface-muted)] p-4">
        <div class="flex items-center justify-between gap-3">
          <StatusBadge :status="job.status" :label="batchJobStatusPresentation(job.status).label" :tone="batchJobStatusPresentation(job.status).tone" dot />
          <span>{{ job.processed }} / {{ job.total }}</span>
        </div>
        <div class="mt-3 h-2 overflow-hidden rounded bg-[var(--surface-solid)]">
          <div class="progress-fill h-full bg-violet-500" :style="{ width: `${job.total ? job.processed / job.total * 100 : 0}%` }" />
        </div>
        <p class="mt-2 text-xs text-[var(--muted)]">条件排除 {{ conditionExcluded }} · 成功 {{ job.succeeded }} · 已跳过 {{ job.skipped }} · 失败 {{ job.failed }}</p>
        <p class="mt-1 text-[10px] text-[var(--muted)]">共选择 {{ submittedCount }} 位，{{ job.total }} 位进入头像刮削任务</p>
      </div>
      <div class="mt-4 max-h-80 overflow-y-auto rounded-xl border border-[var(--border)]">
        <div v-for="item in job.items" :key="item.id" class="flex items-center justify-between gap-3 border-b border-[var(--border)] p-3 text-sm last:border-0">
          <span class="min-w-0">
            <span class="block truncate font-semibold">{{ artistName(item.artistId) }}</span>
            <span class="mt-0.5 block text-xs text-[var(--muted)]">{{ item.source ?? (item.status === 'PENDING' || item.status === 'RUNNING' ? '等待来源' : '未使用来源') }}</span>
          </span>
          <span class="flex shrink-0 items-center gap-2 text-right">
            <StatusBadge data-testid="artist-batch-item-status" :status="item.status" :label="batchItemStatusPresentation(item.status).label" :tone="batchItemStatusPresentation(item.status).tone" />
            <span class="max-w-64 text-[var(--muted)]">{{ batchItemMessage(item.status, item.message) }}</span>
          </span>
        </div>
      </div>
    </template>

    <div v-if="notice" class="mt-4 rounded-xl border border-[var(--border)] bg-[var(--surface-muted)] p-4 text-sm">
      <p class="font-semibold">无需刮削</p>
      <p class="mt-1 text-xs leading-5 text-[var(--muted)]">{{ notice }}</p>
      <p class="mt-2 text-xs font-semibold text-[var(--muted)]">条件排除 {{ conditionExcluded }} 位</p>
    </div>
    <p v-if="error" class="mt-4 rounded-xl bg-rose-500/10 p-3 text-sm text-[var(--danger)]">{{ error }}</p>

    <template #footer>
      <AppButton :disabled="loading" @click="open = false">关闭</AppButton>
      <AppButton v-if="!job" variant="primary" :loading="loading" @click="start">开始刮削</AppButton>
      <AppButton v-else-if="job.status === 'PENDING' || job.status === 'RUNNING'" variant="danger" :loading="loading" @click="cancel">停止任务</AppButton>
      <AppButton v-else-if="job.failed && (job.status === 'FAILED' || job.status === 'COMPLETED')" variant="primary" :loading="loading" @click="retry">重试失败项</AppButton>
    </template>
  </BaseDialog>
</template>
