<script setup lang="ts">
import { Fingerprint, Search, Sparkles } from "lucide-vue-next";
import { computed, onUnmounted, reactive, ref, watch } from "vue";
import AppButton from "@/components/AppButton.vue";
import BaseDialog from "@/components/BaseDialog.vue";
import type { TrackMetadataRecord, TrackSummary } from "@/features/music/domain/models";
import {
  assertWritebackAllowed,
  sourceWritebackCapability,
  useWritebackSelection,
  writebackBlockedMessage,
} from "@/features/music/presentation/writeback-capability";
import { defaultScrapingFields, type SearchSource, type TagCandidate } from "@/features/scraping/domain/models";
import { useTagScraping } from "@/app/services/scraping";
import { useUiStore } from "@/stores/ui";

const open = defineModel<boolean>({ required: true });
const props = defineProps<{ track?: TrackSummary; expectedVersion?: number; writebackSource?: TrackMetadataRecord["source"] }>();
const emit = defineEmits<{ applied: [] }>();
const scraping = useTagScraping();
const ui = useUiStore();
const source = ref<SearchSource>("smart");
const query = ref("");
const candidates = ref<TagCandidate[]>([]);
const selected = ref<TagCandidate>();
const fields = reactive(defaultScrapingFields());
const archived = computed(() => props.track?.status === "ARCHIVED");
const writebackCapability = computed(() => sourceWritebackCapability(props.writebackSource));
const writeBack = useWritebackSelection(writebackCapability);
const reason = ref("在线 Tag 刮削");
const loading = ref(false);
const error = ref("");
let lookupController: AbortController | undefined;
let lookupGeneration = 0;
let applyGeneration = 0;

function cancelLookup(): void {
  lookupGeneration += 1;
  lookupController?.abort();
  lookupController = undefined;
}

watch(open, (value) => {
  applyGeneration += 1;
  cancelLookup();
  if (!value || !props.track) return;
  query.value = [props.track.title, props.track.artists.join(" ")].filter(Boolean).join(" ");
  candidates.value = []; selected.value = undefined; writeBack.value = false;
  error.value = archived.value ? "已归档曲目需先恢复后才能在线刮削" : "";
});
onUnmounted(cancelLookup);

function scrapableTrack(): TrackSummary | undefined {
  const track = props.track;
  if (!track) return undefined;
  if (!archived.value) return track;
  error.value = "已归档曲目需先恢复后才能在线刮削";
  return undefined;
}

async function search(): Promise<void> {
  const track = scrapableTrack();
  if (!track || !query.value.trim()) return;
  cancelLookup();
  const generation = lookupGeneration;
  const controller = new AbortController();
  lookupController = controller;
  loading.value = true; error.value = "";
  try {
    const result = await scraping.search({
      source: source.value, query: query.value.trim(), title: track.title,
      artist: track.artists.join(","), album: track.album?.title ?? "",
    }, controller.signal);
    if (generation !== lookupGeneration || !open.value) return;
    candidates.value = result;
    selected.value = candidates.value[0];
  } catch (cause) {
    if (controller.signal.aborted || generation !== lookupGeneration || !open.value) return;
    error.value = cause instanceof Error ? cause.message : "搜索失败";
  } finally {
    if (generation === lookupGeneration) loading.value = false;
    if (lookupController === controller) lookupController = undefined;
  }
}

async function fingerprint(): Promise<void> {
  const track = scrapableTrack();
  if (!track) return;
  cancelLookup();
  const generation = lookupGeneration;
  const controller = new AbortController();
  lookupController = controller;
  loading.value = true; error.value = "";
  try {
    const result = await scraping.fingerprint(track.id, controller.signal);
    if (generation !== lookupGeneration || !open.value) return;
    candidates.value = result;
    selected.value = candidates.value[0];
  } catch (cause) {
    if (controller.signal.aborted || generation !== lookupGeneration || !open.value) return;
    error.value = cause instanceof Error ? cause.message : "指纹识别失败";
  } finally {
    if (generation === lookupGeneration) loading.value = false;
    if (lookupController === controller) lookupController = undefined;
  }
}

async function apply(): Promise<void> {
  const track = scrapableTrack();
  if (!track || !selected.value || !props.expectedVersion) return;
  try {
    assertWritebackAllowed(writeBack.value, writebackCapability.value);
  } catch (cause) {
    writeBack.value = false;
    error.value = cause instanceof Error ? cause.message : "当前曲目不能写回源文件 Tag";
    return;
  }
  const generation = applyGeneration;
  const trackId = track.id;
  loading.value = true; error.value = "";
  try {
    const result = await scraping.apply(trackId, { expectedVersion: props.expectedVersion, candidate: selected.value, fields: { ...fields }, writeBack: writeBack.value, reason: reason.value.trim() || "在线 Tag 刮削" });
    if (result.warnings.length) ui.notify("warning", "Tag 已应用，但存在警告", result.warnings.join("；"));
    emit("applied");
    if (generation === applyGeneration && open.value && props.track?.id === trackId) open.value = false;
  } catch (cause) {
    if (generation === applyGeneration && open.value) error.value = cause instanceof Error ? cause.message : "应用失败";
  } finally {
    if (generation === applyGeneration) loading.value = false;
  }
}
</script>

<template>
  <BaseDialog v-model="open" title="在线 Tag 刮削" :description="track?.title" width="2xl">
    <div class="grid gap-4 lg:grid-cols-[180px_minmax(0,1fr)_auto] lg:items-end">
      <div><label class="ui-label">搜索来源</label><select v-model="source" class="ui-select" :disabled="archived"><option value="smart">智能多源</option><option value="netease">网易云</option><option value="migu">咪咕</option><option value="qmusic">QQ 音乐</option><option value="kugou">酷狗</option><option value="kuwo">酷我</option></select></div>
      <div><label class="ui-label">搜索关键词</label><input v-model="query" class="ui-input" :disabled="archived" placeholder="歌曲、艺术家或专辑" @keyup.enter="search" /></div>
      <div class="grid grid-cols-2 gap-2 lg:flex"><AppButton :loading="loading" :disabled="archived" @click="search"><template #icon><Search :size="15" /></template>搜索</AppButton><AppButton :loading="loading" :disabled="archived" @click="fingerprint"><template #icon><Fingerprint :size="15" /></template>音频指纹</AppButton></div>
    </div>
    <div class="mt-6 grid gap-6 xl:grid-cols-[minmax(0,1.65fr)_minmax(300px,0.75fr)]">
      <section class="overflow-hidden rounded-2xl border border-[var(--border)]"><div class="flex items-center justify-between border-b border-[var(--border)] px-4 py-3"><div><h3 class="font-bold">匹配候选</h3><p class="mt-0.5 text-xs text-[var(--muted)]">选择最符合当前曲目的元数据</p></div><span class="text-xs font-semibold text-[var(--muted)]">{{ candidates.length }} 条结果</span></div><div v-if="candidates.length" class="grid min-h-[360px] max-h-[56vh] gap-3 overflow-y-auto p-4 md:grid-cols-2"><button v-for="item in candidates" :key="`${item.source}:${item.id}`" type="button" class="flex min-w-0 gap-4 rounded-2xl border p-4 text-left transition-colors" :class="selected === item ? 'border-violet-500 bg-violet-500/8 ring-1 ring-violet-500/20' : 'border-[var(--border)] hover:bg-[var(--surface-muted)]'" @click="selected = item"><img v-if="item.albumImg" :src="scraping.artworkUrl(item.albumImg)" class="h-20 w-20 shrink-0 rounded-xl object-cover" alt="候选封面" width="80" height="80" loading="lazy" decoding="async" /><span v-else class="grid h-20 w-20 shrink-0 place-items-center rounded-xl bg-[var(--surface-muted)] text-xs text-[var(--muted)]">无封面</span><span class="min-w-0 flex-1"><span class="block truncate text-base font-bold">{{ item.name }}</span><span class="mt-1 block truncate text-sm text-[var(--muted)]">{{ item.artist }}</span><span class="mt-1 block truncate text-xs text-[var(--muted)]">{{ item.album || '未知专辑' }}</span><span class="mt-3 block text-[10px] font-bold uppercase text-[var(--primary)]">{{ item.source }} · 匹配分 {{ item.score ?? '-' }}</span></span></button></div><div v-else class="grid min-h-[360px] place-items-center bg-[var(--surface-muted)]/45 p-8 text-center"><div><Search :size="28" class="mx-auto text-[var(--muted)]" /><p class="mt-3 font-semibold">尚无候选结果</p><p class="mt-1 text-sm text-[var(--muted)]">搜索或使用音频指纹识别候选 Tag。</p></div></div></section>
      <aside class="rounded-2xl border border-[var(--border)] bg-[var(--surface-muted)]/45 p-5"><div><h3 class="font-bold">应用设置</h3><p class="mt-1 text-xs leading-5 text-[var(--muted)]">选择要写入的字段，并决定是否覆盖已有内容。</p></div><div class="mt-5"><p class="ui-label">应用字段</p><div class="grid grid-cols-2 gap-2"><label v-for="item in [{k:'title',l:'标题'},{k:'artist',l:'艺术家'},{k:'album',l:'专辑'},{k:'year',l:'年份'},{k:'genre',l:'流派'},{k:'lyrics',l:'歌词'},{k:'cover',l:'专辑封面'}]" :key="item.k" class="flex items-center gap-2 rounded-xl border border-[var(--border)] bg-[var(--surface-solid)] p-3 text-sm"><input v-model="fields[item.k as keyof typeof fields]" type="checkbox" />{{ item.l }}</label></div></div><label class="mt-5 flex items-start gap-3 rounded-xl border border-[var(--border)] bg-[var(--surface-solid)] p-4 text-sm"><input v-model="fields.overwrite" class="mt-0.5" type="checkbox" /><span><span class="block font-semibold">覆盖已有字段</span><span class="mt-1 block text-xs leading-5 text-[var(--muted)]">关闭时只填补当前为空的字段。</span></span></label><label class="mt-3 flex items-start gap-3 rounded-xl border border-[var(--border)] bg-[var(--surface-solid)] p-4 text-sm" :class="!writebackCapability.canWriteBack && 'opacity-75'"><input v-model="writeBack" data-testid="tag-writeback" class="mt-0.5" type="checkbox" :disabled="!writebackCapability.canWriteBack" /><span><span class="block font-semibold">写回源文件 Tag</span><span class="mt-1 block text-xs leading-5 text-[var(--muted)]">{{ writebackCapability.canWriteBack ? '应用成功后创建安全写回任务。' : writebackBlockedMessage(writebackCapability) }}</span></span></label><div class="mt-5"><label class="ui-label">修改原因</label><input v-model="reason" class="ui-input" /></div></aside>
    </div>
    <p v-if="error" class="mt-4 rounded-xl bg-rose-500/10 p-3 text-sm text-[var(--danger)]">{{ error }}</p>
    <template #footer><AppButton @click="open = false">取消</AppButton><AppButton variant="primary" :loading="loading" :disabled="archived || !selected || !expectedVersion" @click="apply"><template #icon><Sparkles :size="15" /></template>应用候选</AppButton></template>
  </BaseDialog>
</template>
