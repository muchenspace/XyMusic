<script setup lang="ts">
import { Check, Mic2, Pencil, RefreshCw, Search, Sparkles, X } from "lucide-vue-next";
import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/vue-query";
import { refDebounced } from "@vueuse/core";
import { computed, reactive, ref, watch } from "vue";
import { ApiError } from "@/shared/application/api-error";
import AppButton from "@/components/AppButton.vue";
import AppPagination from "@/components/AppPagination.vue";
import ArtistArtworkScrapeDialog from "@/components/ArtistArtworkScrapeDialog.vue";
import ArtworkUploadField from "@/components/ArtworkUploadField.vue";
import BaseDialog from "@/components/BaseDialog.vue";
import BatchArtistArtworkScrapeDialog from "@/components/BatchArtistArtworkScrapeDialog.vue";
import PageHeader from "@/components/PageHeader.vue";
import StatePanel from "@/components/StatePanel.vue";
import type { ArtistSummary } from "@/features/music/domain/models";
import { useMusicAdmin } from "@/app/services/music";
import { useUiStore } from "@/stores/ui";
import { formatDate } from "@/utils/format";

const queryClient = useQueryClient();
const ui = useUiStore();
const musicAdmin = useMusicAdmin();
const search = ref("");
const debounced = refDebounced(search, 300);
const page = ref(1);
const pageSize = ref(24);
const selected = ref<ArtistSummary>();
const selectedArtists = ref(new Map<string, ArtistSummary>());
const selectedIds = computed(() => new Set(selectedArtists.value.keys()));
const editorOpen = ref(false);
const scrapeOpen = ref(false);
const batchScrapeOpen = ref(false);
const actionError = ref("");
let allowEditorClose = false;
const form = reactive({ name: "", description: "" });
const maximumBatchArtists = 200;

const query = useQuery({
  queryKey: computed(() => ["admin", "artists", { page: page.value, pageSize: pageSize.value, search: debounced.value }]),
  queryFn: ({ signal }) => musicAdmin.listArtists({
    page: page.value,
    pageSize: pageSize.value,
    search: debounced.value,
    sort: "name",
    order: "asc",
  }, signal),
  placeholderData: keepPreviousData,
});

const pageArtists = computed(() => query.data.value?.items ?? []);
const pageSelected = computed(() => pageArtists.value.length > 0 && pageArtists.value.every((artist) => selectedIds.value.has(artist.id)));
const pagePartiallySelected = computed(() => !pageSelected.value && pageArtists.value.some((artist) => selectedIds.value.has(artist.id)));

function changePageSize(value: number): void {
  pageSize.value = value;
  page.value = 1;
}

function edit(artist: ArtistSummary): void {
  selected.value = artist;
  Object.assign(form, { name: artist.name, description: artist.description ?? "" });
  actionError.value = "";
  editorOpen.value = true;
}

function toggleArtist(artist: ArtistSummary): void {
  const next = new Map(selectedArtists.value);
  if (next.has(artist.id)) next.delete(artist.id);
  else {
    if (next.size >= maximumBatchArtists) {
      ui.notify("warning", "单次最多选择 200 位艺术家");
      return;
    }
    next.set(artist.id, artist);
  }
  selectedArtists.value = next;
}

function togglePage(): void {
  const next = new Map(selectedArtists.value);
  if (pageSelected.value) {
    for (const artist of pageArtists.value) next.delete(artist.id);
  } else {
    for (const artist of pageArtists.value) {
      if (next.has(artist.id)) continue;
      if (next.size >= maximumBatchArtists) {
        ui.notify("warning", "单次最多选择 200 位艺术家");
        break;
      }
      next.set(artist.id, artist);
    }
  }
  selectedArtists.value = next;
}

function clearSelection(): void {
  selectedArtists.value = new Map();
}

function openBatchScrape(): void {
  if (!selectedArtists.value.size) return;
  batchScrapeOpen.value = true;
}

function openArtworkScrape(): void {
  if (!selected.value) return;
  scrapeOpen.value = true;
}

async function refresh(): Promise<void> {
  await Promise.all([
    queryClient.invalidateQueries({ queryKey: ["admin", "artists"] }),
    queryClient.invalidateQueries({ queryKey: ["admin", "albums"] }),
    queryClient.invalidateQueries({ queryKey: ["admin", "tracks"] }),
    queryClient.invalidateQueries({ queryKey: ["admin", "dashboard"] }),
    queryClient.invalidateQueries({ queryKey: ["admin", "audit"] }),
  ]);
  const updated = query.data.value?.items.find((artist) => artist.id === selected.value?.id);
  if (updated) selected.value = updated;
}

async function artworkUploaded(): Promise<void> {
  ui.notify("success", "艺术家头像已更新");
  if (selected.value) selected.value = { ...selected.value, version: selected.value.version + 1 };
  await refresh();
}

async function artworkScraped(version: number): Promise<void> {
  if (selected.value) selected.value = { ...selected.value, version };
  await refresh();
}

async function batchScrapingCompleted(): Promise<void> {
  clearSelection();
  await refresh();
}

const saveMutation = useMutation({
  mutationFn: () => {
    if (!form.name.trim()) throw new Error("艺术家名称不能为空");
    return musicAdmin.updateArtist(selected.value!.id, {
      expectedVersion: selected.value!.version,
      name: form.name.trim(),
      description: form.description.trim() || null,
    });
  },
  onSuccess: async () => {
    allowEditorClose = true;
    editorOpen.value = false;
    ui.notify("success", "艺术家资料已更新");
    await refresh();
  },
  onError: (error) => {
    actionError.value = error instanceof ApiError || error instanceof Error ? error.message : "艺术家保存失败";
  },
});

watch(() => query.data.value?.items, (items) => {
  if (!items?.length || !selectedArtists.value.size) return;
  const next = new Map(selectedArtists.value);
  for (const artist of items) {
    if (next.has(artist.id)) next.set(artist.id, artist);
  }
  selectedArtists.value = next;
});

watch(search, () => {
  if (selectedArtists.value.size) clearSelection();
});

watch(editorOpen, (value) => {
  if (!value && saveMutation.isPending.value && !allowEditorClose) editorOpen.value = true;
  allowEditorClose = false;
});
</script>

<template>
  <div class="space-y-6 page-enter">
    <PageHeader title="艺术家" description="维护艺术家名称、简介与头像，查看关联专辑和曲目数量。">
      <template #eyebrow>音乐资料库</template>
      <template #actions>
        <AppButton :loading="query.isFetching.value" @click="query.refetch()">
          <template #icon><RefreshCw :size="16" /></template>
          刷新
        </AppButton>
      </template>
    </PageHeader>

    <nav class="flex gap-1 overflow-x-auto rounded-xl bg-[var(--surface-muted)] p-1 sm:w-max">
      <RouterLink class="rounded-lg px-4 py-2 text-sm font-semibold text-[var(--muted)]" to="/music/tracks">曲目</RouterLink>
      <RouterLink class="rounded-lg px-4 py-2 text-sm font-semibold text-[var(--muted)]" to="/music/albums">专辑</RouterLink>
      <RouterLink class="rounded-lg bg-[var(--surface-solid)] px-4 py-2 text-sm font-bold text-[var(--primary)] shadow-sm" to="/music/artists">艺术家</RouterLink>
    </nav>

    <Transition name="content-swap">
      <div v-if="selectedIds.size" class="sticky top-[84px] z-10 flex flex-wrap items-center gap-3 rounded-2xl border border-violet-500/25 bg-[var(--surface-solid)] p-3 shadow-xl">
        <span class="grid h-9 w-9 place-items-center rounded-xl bg-[var(--primary-soft)] text-[var(--primary)]"><Check :size="17" /></span>
        <p class="mr-auto font-semibold">已选择 {{ selectedIds.size }} 位艺术家</p>
        <AppButton variant="ghost" :disabled="batchScrapeOpen" @click="clearSelection">
          <template #icon><X :size="15" /></template>
          取消
        </AppButton>
        <AppButton variant="primary" :disabled="batchScrapeOpen" @click="openBatchScrape">
          <template #icon><Sparkles :size="15" /></template>
          批量刮削头像
        </AppButton>
      </div>
    </Transition>

    <section class="ui-card overflow-hidden" :class="{ 'data-refreshing': query.isFetching.value && !query.isPending.value }" :aria-busy="query.isFetching.value">
      <div class="flex items-center gap-3 border-b border-[var(--border)] p-4">
        <label class="grid h-10 w-10 shrink-0 place-items-center rounded-xl border border-[var(--border)] bg-[var(--surface-solid)]" title="选择当前页">
          <input
            type="checkbox"
            :checked="pageSelected"
            :indeterminate="pagePartiallySelected"
            :disabled="!pageArtists.length || batchScrapeOpen"
            aria-label="选择当前页全部艺术家"
            @change="togglePage"
          />
        </label>
        <div class="relative max-w-lg flex-1">
          <Search :size="16" class="absolute left-3.5 top-1/2 -translate-y-1/2 text-[var(--muted)]" />
          <input v-model="search" class="ui-input !pl-10" type="search" placeholder="搜索艺术家名称或简介" @input="page = 1" />
        </div>
      </div>
      <StatePanel v-if="query.isPending.value" state="loading" />
      <StatePanel v-else-if="query.isError.value" state="error" @retry="query.refetch()" />
      <StatePanel v-else-if="!query.data.value?.items.length" state="empty" title="没有符合条件的艺术家" />
      <template v-else>
        <div class="grid gap-px bg-[var(--border)] sm:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4">
          <article
            v-for="(artist, index) in query.data.value.items"
            :key="artist.id"
            class="group motion-item media-tile relative flex items-center gap-4 bg-[var(--surface-solid)] p-4 hover:bg-[var(--surface-muted)]"
            :style="{ '--motion-index': index }"
          >
            <label
              class="absolute left-1.5 top-1.5 z-10 grid h-7 w-7 place-items-center rounded-lg border border-[var(--border)] bg-[var(--surface-solid)]/95 shadow-sm"
              :class="selectedIds.has(artist.id) ? 'opacity-100' : 'opacity-70 transition-opacity group-hover:opacity-100 focus-within:opacity-100'"
              @click.stop
              @keydown.stop
            >
              <input
                type="checkbox"
                :checked="selectedIds.has(artist.id)"
                :disabled="batchScrapeOpen"
                :aria-label="`选择艺术家：${artist.name}`"
                @change="toggleArtist(artist)"
              />
            </label>
            <span class="grid h-16 w-16 shrink-0 place-items-center overflow-hidden rounded-full bg-[var(--primary-soft)] text-[var(--primary)]">
              <img v-if="artist.artwork" :src="artist.artwork.url" :alt="artist.name" class="media-artwork h-full w-full object-cover" width="64" height="64" loading="lazy" decoding="async" />
              <Mic2 v-else :size="23" />
            </span>
            <div class="min-w-0 flex-1">
              <h3 class="truncate font-bold">{{ artist.name }}</h3>
              <p class="mt-1 line-clamp-1 text-xs text-[var(--muted)]">{{ artist.description || '暂无简介' }}</p>
              <p class="mt-2 text-xs font-semibold text-[var(--muted)]">{{ artist.albumCount }} 张专辑 · {{ artist.trackCount }} 首曲目</p>
            </div>
            <button class="btn btn-ghost btn-icon" type="button" :aria-label="`编辑艺术家：${artist.name}`" @click="edit(artist)">
              <Pencil :size="15" />
            </button>
          </article>
        </div>
        <AppPagination :page="page" :page-size="pageSize" :total="query.data.value.total" @change="page = $event" @page-size-change="changePageSize" />
      </template>
    </section>

    <BaseDialog v-model="editorOpen" title="编辑艺术家" :description="selected ? `更新于 ${formatDate(selected.updatedAt)}` : ''" width="lg">
      <div v-if="selected" class="grid gap-6 sm:grid-cols-[120px_1fr]">
        <ArtworkUploadField
          :target-id="selected.id"
          purpose="ARTIST_ARTWORK"
          :image-url="selected.artwork?.url"
          :alt="`${selected.name} 头像`"
          shape="circle"
          noun="头像"
          @completed="artworkUploaded"
        >
          <Mic2 :size="34" />
        </ArtworkUploadField>
        <div class="space-y-5">
          <div><label class="ui-label">艺术家名称</label><input v-model="form.name" class="ui-input" /></div>
          <div><label class="ui-label">艺术家简介</label><textarea v-model="form.description" class="ui-textarea min-h-40" /></div>
        </div>
      </div>
      <p v-if="actionError" class="mt-5 rounded-xl bg-rose-500/10 p-3 text-sm text-[var(--danger)]">{{ actionError }}</p>
      <template #footer>
        <AppButton :disabled="saveMutation.isPending.value" @click="openArtworkScrape">
          <template #icon><Sparkles :size="15" /></template>
          在线刮削头像
        </AppButton>
        <span class="flex-1" />
        <AppButton @click="editorOpen = false">取消</AppButton>
        <AppButton variant="primary" :loading="saveMutation.isPending.value" @click="saveMutation.mutate()">保存艺术家</AppButton>
      </template>
    </BaseDialog>

    <ArtistArtworkScrapeDialog v-model="scrapeOpen" :artist="selected" @applied="artworkScraped" />
    <BatchArtistArtworkScrapeDialog v-model="batchScrapeOpen" :artists="[...selectedArtists.values()]" @completed="batchScrapingCompleted" />
  </div>
</template>
