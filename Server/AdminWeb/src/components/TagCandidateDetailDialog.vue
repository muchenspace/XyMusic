<script setup lang="ts">
import { Check, Disc3 } from "lucide-vue-next";
import { computed, onUnmounted, ref, watch } from "vue";
import AppButton from "@/components/AppButton.vue";
import BaseDialog from "@/components/BaseDialog.vue";
import StatePanel from "@/components/StatePanel.vue";
import { useTagScraping } from "@/app/services/scraping";
import type { TagCandidate, TagCandidateDetail } from "@/features/scraping/domain/models";

const open = defineModel<boolean>({ required: true });
const props = withDefaults(defineProps<{
  candidate?: TagCandidate;
  selected?: boolean;
}>(), { selected: false });
const emit = defineEmits<{ select: [candidate: TagCandidate] }>();
const scraping = useTagScraping();

const detail = ref<TagCandidateDetail>();
const loading = ref(false);
const error = ref("");
let detailController: AbortController | undefined;
let detailGeneration = 0;

const candidateKey = computed(() => props.candidate ? `${props.candidate.source}:${props.candidate.id}` : "");
const displayedCandidate = computed(() => detail.value?.candidate ?? props.candidate);
const artworkUrl = computed(() => displayedCandidate.value?.albumImg
  ? scraping.artworkUrl(displayedCandidate.value.albumImg)
  : "");
const lyrics = computed(() => detail.value?.lyrics);
const hasLyrics = computed(() => Boolean(lyrics.value?.content.trim()));
const description = computed(() => {
  const candidate = displayedCandidate.value;
  if (!candidate) return undefined;
  return [candidate.artist, candidate.album].filter(Boolean).join(" · ") || sourceLabel(candidate.source);
});

const summaryRows = computed(() => {
  const candidate = displayedCandidate.value;
  if (!candidate) return [];
  return [
    { label: "艺术家", value: displayValue(candidate.artist) },
    { label: "专辑", value: displayValue(candidate.album) },
    { label: "发行年份", value: displayValue(candidate.year) },
    { label: "来源", value: sourceLabel(candidate.source) },
  ];
});

const technicalRows = computed(() => {
  const candidate = displayedCandidate.value;
  if (!candidate) return [];
  return [
    { label: "音轨", value: displayValue(candidate.track) },
    { label: "碟号", value: displayValue(candidate.disc) },
    { label: "流派", value: displayValue(candidate.genre) },
    { label: "歌曲 ID", value: displayValue(candidate.id), mono: true },
    { label: "艺术家 ID", value: displayValue(candidate.artistId), mono: true },
    { label: "专辑 ID", value: displayValue(candidate.albumId), mono: true },
  ];
});

const scoreRows = computed(() => {
  const candidate = displayedCandidate.value;
  if (!candidate) return [];
  return [
    { label: "综合匹配", value: scoreLabel(candidate.score) },
    { label: "标题匹配", value: scoreLabel(candidate.titleScore) },
    { label: "艺术家匹配", value: scoreLabel(candidate.artistScore) },
    { label: "专辑匹配", value: scoreLabel(candidate.albumScore) },
  ];
});

function displayValue(value: string): string {
  return value.trim() || "—";
}

function scoreLabel(value: number | undefined): string {
  return value !== undefined && Number.isFinite(value) ? value.toFixed(2) : "—";
}

function sourceLabel(source: TagCandidate["source"]): string {
  return ({
    netease: "网易云",
    migu: "咪咕",
    qmusic: "QQ 音乐",
    kugou: "酷狗",
    kuwo: "酷我",
    acoustid: "AcoustID",
  } satisfies Record<TagCandidate["source"], string>)[source];
}

function cancelDetailRequest(): void {
  detailGeneration += 1;
  detailController?.abort();
  detailController = undefined;
}

function loadDetail(): void {
  const candidate = props.candidate;
  const key = candidateKey.value;
  cancelDetailRequest();
  detail.value = undefined;
  error.value = "";
  if (!open.value || !candidate || !key) {
    loading.value = false;
    return;
  }

  const generation = detailGeneration;
  const controller = new AbortController();
  detailController = controller;
  loading.value = true;
  void scraping.candidateDetail({ candidate }, controller.signal).then((result) => {
    if (generation !== detailGeneration || !open.value || candidateKey.value !== key) return;
    detail.value = result;
  }).catch((cause: unknown) => {
    if (controller.signal.aborted || generation !== detailGeneration || !open.value || candidateKey.value !== key) return;
    error.value = cause instanceof Error ? cause.message : "候选详情加载失败";
  }).finally(() => {
    if (generation === detailGeneration) loading.value = false;
    if (detailController === controller) detailController = undefined;
  });
}

function selectCandidate(): void {
  if (!props.candidate || props.selected) return;
  emit("select", props.candidate);
  open.value = false;
}

watch([open, candidateKey], loadDetail, { immediate: true });
onUnmounted(cancelDetailRequest);
</script>

<template>
  <BaseDialog v-model="open" :title="displayedCandidate?.name || '候选详情'" :description="description" width="xl">
    <div v-if="displayedCandidate" data-testid="candidate-detail" class="grid gap-6 lg:grid-cols-[minmax(0,0.95fr)_minmax(320px,1.05fr)] lg:items-start">
      <div class="min-w-0 space-y-5">
        <div class="grid gap-5 sm:grid-cols-[144px_minmax(0,1fr)]">
          <div class="mx-auto w-36 sm:mx-0">
            <img
              v-if="artworkUrl"
              :src="artworkUrl"
              :alt="`${displayedCandidate.name} 专辑封面`"
              class="aspect-square w-full rounded-xl object-cover"
              width="144"
              height="144"
              decoding="async"
            />
            <div v-else class="grid aspect-square w-full place-items-center rounded-xl border border-[var(--border)] bg-[var(--surface-muted)] text-[var(--muted)]">
              <Disc3 :size="32" aria-hidden="true" />
              <span class="sr-only">无专辑封面</span>
            </div>
          </div>

          <dl class="min-w-0">
            <div v-for="item in summaryRows" :key="item.label" class="min-w-0 border-b border-[var(--border)] py-2 first:pt-0">
              <dt class="text-xs font-semibold text-[var(--muted)]">{{ item.label }}</dt>
              <dd class="mt-1 break-words text-sm font-medium">{{ item.value }}</dd>
            </div>
          </dl>
        </div>

        <dl class="grid min-w-0 gap-x-5 sm:grid-cols-2">
          <div v-for="item in technicalRows" :key="item.label" class="min-w-0 border-b border-[var(--border)] py-2.5">
            <dt class="text-xs font-semibold text-[var(--muted)]">{{ item.label }}</dt>
            <dd class="mt-1 break-words text-sm font-medium" :class="item.mono && 'break-all font-mono text-xs'">{{ item.value }}</dd>
          </div>
        </dl>

        <section aria-labelledby="candidate-score-heading">
          <h3 id="candidate-score-heading" class="text-sm font-bold">匹配评分</h3>
          <dl class="mt-3 grid grid-cols-2 overflow-hidden rounded-xl border border-[var(--border)]">
            <div v-for="item in scoreRows" :key="item.label" class="border-b border-r border-[var(--border)] p-3 even:border-r-0 [&:nth-last-child(-n+2)]:border-b-0">
              <dt class="text-xs text-[var(--muted)]">{{ item.label }}</dt>
              <dd class="mt-1 text-base font-bold tabular-nums">{{ item.value }}</dd>
            </div>
          </dl>
        </section>
      </div>

      <section class="min-w-0 lg:sticky lg:top-0" aria-labelledby="candidate-lyrics-heading">
        <div class="flex flex-wrap items-baseline justify-between gap-2">
          <h3 id="candidate-lyrics-heading" class="text-sm font-bold">歌词</h3>
          <span v-if="lyrics" class="text-xs text-[var(--muted)]">{{ lyrics.format }} · {{ lyrics.language || 'und' }}</span>
        </div>
        <div class="mt-3 min-h-[360px] overflow-hidden rounded-xl border border-[var(--border)] bg-[var(--surface-muted)]/45">
          <StatePanel v-if="loading" state="loading" compact title="正在获取歌词" />
          <StatePanel v-else-if="error" state="error" compact title="歌词加载失败" :detail="error" @retry="loadDetail" />
          <StatePanel v-else-if="!hasLyrics" state="empty" compact title="未找到歌词" detail="该来源未返回可用于预览的歌词。" />
          <pre v-else data-testid="candidate-lyrics" class="h-[min(52vh,520px)] min-h-[360px] overflow-auto whitespace-pre-wrap break-words p-4 font-mono text-sm leading-7 text-[var(--text)]">{{ lyrics?.content }}</pre>
        </div>
      </section>
    </div>
    <StatePanel v-else state="empty" compact title="未选择候选" />

    <template #footer>
      <AppButton @click="open = false">关闭</AppButton>
      <AppButton variant="primary" :disabled="!candidate || selected" @click="selectCandidate">
        <template #icon><Check :size="15" /></template>
        {{ selected ? '当前已选用' : '选用此候选' }}
      </AppButton>
    </template>
  </BaseDialog>
</template>
