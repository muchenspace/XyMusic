<script setup lang="ts">
import { Mic2, Search, Sparkles } from "lucide-vue-next";
import { computed, onUnmounted, ref, watch } from "vue";
import AppButton from "@/components/AppButton.vue";
import BaseDialog from "@/components/BaseDialog.vue";
import type { ArtistSummary } from "@/features/music/domain/models";
import type { ArtistCandidate, SearchSource } from "@/features/scraping/domain/models";
import { useTagScraping } from "@/app/services/scraping";
import { useUiStore } from "@/stores/ui";

const open = defineModel<boolean>({ required: true });
const props = defineProps<{ artist?: ArtistSummary }>();
const emit = defineEmits<{ applied: [version: number] }>();
const scraping = useTagScraping();
const ui = useUiStore();

const source = ref<SearchSource>("smart");
const query = ref("");
const reason = ref("在线刮削艺术家头像");
const candidates = ref<ArtistCandidate[]>([]);
const selected = ref<ArtistCandidate>();
const searched = ref(false);
const searching = ref(false);
const applying = ref(false);
const error = ref("");
const notice = ref("");
let searchController: AbortController | undefined;
let searchGeneration = 0;
let applyGeneration = 0;

const sourceOptions: Array<{ value: SearchSource; label: string }> = [
  { value: "smart", label: "智能多源" },
  { value: "qmusic", label: "QQ 音乐" },
  { value: "netease", label: "网易云" },
];
const supportedArtistSources = ["qmusic", "netease"] as const;

const selectedImageUrl = computed(() => selected.value?.imageUrl
  ? scraping.artworkUrl(selected.value.imageUrl)
  : "");

function cancelSearch(): void {
  searchGeneration += 1;
  searchController?.abort();
  searchController = undefined;
}

watch(open, (value) => {
  applyGeneration += 1;
  cancelSearch();
  if (!value) return;
  source.value = "smart";
  query.value = props.artist?.name ?? "";
  reason.value = "在线刮削艺术家头像";
  candidates.value = [];
  selected.value = undefined;
  searched.value = false;
  searching.value = false;
  applying.value = false;
  error.value = "";
  notice.value = "";
}, { immediate: true });

watch([source, query], () => {
  if (!open.value) return;
  cancelSearch();
  candidates.value = [];
  selected.value = undefined;
  searched.value = false;
  notice.value = "";
  error.value = "";
});

onUnmounted(() => {
  applyGeneration += 1;
  cancelSearch();
});

function candidateImageUrl(candidate: ArtistCandidate): string {
  return candidate.imageUrl ? scraping.artworkUrl(candidate.imageUrl) : "";
}

function scoreLabel(score: number): string {
  return Number.isFinite(score) ? score.toFixed(2).replace(/\.00$/u, "") : "—";
}

async function search(): Promise<void> {
  const artist = props.artist;
  const keyword = query.value.trim();
  if (!artist || !keyword) {
    error.value = "请输入艺术家名称";
    return;
  }
  cancelSearch();
  candidates.value = [];
  selected.value = undefined;
  const generation = searchGeneration;
  const controller = new AbortController();
  searchController = controller;
  searching.value = true;
  searched.value = false;
  error.value = "";
  notice.value = "";
  try {
    const result = await scraping.searchArtists({
      source: source.value,
      query: keyword,
      sources: source.value === "smart" ? [...supportedArtistSources] : undefined,
    }, controller.signal);
    if (generation !== searchGeneration || !open.value || props.artist?.id !== artist.id) return;
    candidates.value = result;
    selected.value = result[0];
    searched.value = true;
    if (!result.length) notice.value = "未找到可靠头像，本次已跳过，当前头像未作任何修改。";
  } catch (cause) {
    if (controller.signal.aborted || generation !== searchGeneration || !open.value) return;
    error.value = cause instanceof Error ? cause.message : "搜索艺术家头像失败";
  } finally {
    if (generation === searchGeneration) searching.value = false;
    if (searchController === controller) searchController = undefined;
  }
}

async function apply(): Promise<void> {
  const artist = props.artist;
  const candidate = selected.value;
  if (!artist || !candidate || searching.value) return;
  const trimmedReason = reason.value.trim();
  if (trimmedReason.length < 2 || trimmedReason.length > 500) {
    error.value = "修改原因需为 2 至 500 个字符";
    return;
  }
  const overwrite = Boolean(artist.artwork);
  if (overwrite && !window.confirm(`“${artist.name}”已有头像，确定使用所选候选覆盖吗？`)) return;

  const generation = applyGeneration;
  const artistID = artist.id;
  applying.value = true;
  error.value = "";
  notice.value = "";
  try {
    const result = await scraping.applyArtistArtwork(artistID, {
      expectedVersion: artist.version,
      candidate,
      overwrite,
      reason: trimmedReason,
    });
    if (generation !== applyGeneration || !open.value || props.artist?.id !== artistID) return;
    if (!result.applied) {
      notice.value = "候选未达到可靠应用条件，本次已跳过，当前头像未作任何修改。";
      return;
    }
    ui.notify("success", "艺术家头像已更新");
    emit("applied", result.version);
    open.value = false;
  } catch (cause) {
    if (generation === applyGeneration && open.value) {
      error.value = cause instanceof Error ? cause.message : "应用艺术家头像失败";
    }
  } finally {
    if (generation === applyGeneration) applying.value = false;
  }
}
</script>

<template>
  <BaseDialog
    v-model="open"
    title="在线刮削艺术家头像"
    :description="artist?.name"
    width="2xl"
    :prevent-close="applying"
  >
    <div class="grid gap-4 lg:grid-cols-[180px_minmax(0,1fr)_auto] lg:items-end">
      <div>
        <label class="ui-label">搜索来源</label>
        <select v-model="source" class="ui-select" :disabled="searching || applying">
          <option v-for="item in sourceOptions" :key="item.value" :value="item.value">{{ item.label }}</option>
        </select>
      </div>
      <div>
        <label class="ui-label">艺术家名称</label>
        <input v-model="query" class="ui-input" :disabled="searching || applying" @keyup.enter="search" />
      </div>
      <AppButton :loading="searching" :disabled="applying" @click="search">
        <template #icon><Search :size="15" /></template>
        搜索头像
      </AppButton>
    </div>

    <div class="mt-6 grid gap-6 xl:grid-cols-[minmax(0,1.65fr)_minmax(280px,0.75fr)]">
      <section class="overflow-hidden rounded-2xl border border-[var(--border)]">
        <div class="flex items-center justify-between border-b border-[var(--border)] px-4 py-3">
          <div>
            <h3 class="font-bold">头像候选</h3>
            <p class="mt-0.5 text-xs text-[var(--muted)]">确认歌手身份后再应用，避免同名艺术家误匹配。</p>
          </div>
          <span class="text-xs font-semibold text-[var(--muted)]">{{ candidates.length }} 条结果</span>
        </div>
        <div v-if="candidates.length" class="grid min-h-[360px] max-h-[56vh] gap-3 overflow-y-auto p-4 md:grid-cols-2">
          <button
            v-for="item in candidates"
            :key="`${item.source}:${item.id}`"
            type="button"
            class="flex min-w-0 items-center gap-4 rounded-2xl border p-4 text-left transition-colors"
            :class="selected === item ? 'border-violet-500 bg-violet-500/8 ring-1 ring-violet-500/20' : 'border-[var(--border)] hover:bg-[var(--surface-muted)]'"
            @click="selected = item"
          >
            <img
              v-if="item.imageUrl"
              :src="candidateImageUrl(item)"
              :alt="`${item.name} 候选头像`"
              class="h-20 w-20 shrink-0 rounded-full object-cover"
              width="80"
              height="80"
              loading="lazy"
              decoding="async"
            />
            <span v-else class="grid h-20 w-20 shrink-0 place-items-center rounded-full bg-[var(--surface-muted)] text-[var(--muted)]"><Mic2 :size="24" /></span>
            <span class="min-w-0 flex-1">
              <span class="block truncate text-base font-bold">{{ item.name }}</span>
              <span class="mt-1 block truncate text-xs text-[var(--muted)]">{{ item.aliases?.join('、') || '暂无别名' }}</span>
              <span class="mt-3 block text-[10px] font-bold uppercase text-[var(--primary)]">{{ item.source }} · 匹配分 {{ scoreLabel(item.score) }}</span>
            </span>
          </button>
        </div>
        <div v-else class="grid min-h-[360px] place-items-center bg-[var(--surface-muted)]/45 p-8 text-center">
          <div>
            <Search :size="28" class="mx-auto text-[var(--muted)]" />
            <p class="mt-3 font-semibold">{{ searched ? '未找到可靠候选，已跳过' : '尚无头像候选' }}</p>
            <p class="mt-1 text-sm text-[var(--muted)]">{{ searched ? '当前头像未作任何修改。' : '输入艺术家名称并搜索在线头像。' }}</p>
          </div>
        </div>
      </section>

      <aside class="rounded-2xl border border-[var(--border)] bg-[var(--surface-muted)]/45 p-5">
        <h3 class="font-bold">应用预览</h3>
        <p class="mt-1 text-xs leading-5 text-[var(--muted)]">仅更新艺术家头像，不修改名称、简介或曲目 Tag。</p>
        <div class="mt-6 grid place-items-center">
          <img v-if="selectedImageUrl" :src="selectedImageUrl" :alt="selected?.name" class="h-36 w-36 rounded-full object-cover shadow-lg" width="144" height="144" />
          <span v-else class="grid h-36 w-36 place-items-center rounded-full bg-[var(--surface-solid)] text-[var(--muted)]"><Mic2 :size="36" /></span>
        </div>
        <p class="mt-4 truncate text-center font-bold">{{ selected?.name || '未选择候选' }}</p>
        <p v-if="artist?.artwork" class="mt-5 rounded-xl bg-amber-500/10 p-3 text-xs leading-5 text-amber-700 dark:text-amber-300">当前艺术家已有头像，应用时会再次确认是否覆盖。</p>
        <div class="mt-5">
          <label class="ui-label">修改原因</label>
          <input v-model="reason" class="ui-input" maxlength="500" :disabled="applying" />
        </div>
      </aside>
    </div>

    <p v-if="notice" class="mt-4 rounded-xl border border-[var(--border)] bg-[var(--surface-muted)] p-3 text-sm text-[var(--muted)]">{{ notice }}</p>
    <p v-if="error" class="mt-4 rounded-xl bg-rose-500/10 p-3 text-sm text-[var(--danger)]">{{ error }}</p>

    <template #footer>
      <AppButton :disabled="applying" @click="open = false">关闭</AppButton>
      <AppButton variant="primary" :loading="applying" :disabled="searching || !selected" @click="apply">
        <template #icon><Sparkles :size="15" /></template>
        应用所选头像
      </AppButton>
    </template>
  </BaseDialog>
</template>
