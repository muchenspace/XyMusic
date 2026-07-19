<script setup lang="ts">
import { AlertTriangle, Album as AlbumIcon, GitMerge, Pencil, RefreshCw, Search } from "lucide-vue-next";
import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/vue-query";
import { refDebounced } from "@vueuse/core";
import { computed, reactive, ref, watch } from "vue";
import { useRouter } from "vue-router";
import AlbumMergeDialog from "@/components/AlbumMergeDialog.vue";
import AppButton from "@/components/AppButton.vue";
import AppPagination from "@/components/AppPagination.vue";
import ArtworkUploadField from "@/components/ArtworkUploadField.vue";
import BaseDialog from "@/components/BaseDialog.vue";
import PageHeader from "@/components/PageHeader.vue";
import StatePanel from "@/components/StatePanel.vue";
import type { AlbumDuplicateGroup, AlbumSummary, CreditRole } from "@/features/music/domain/models";
import { useMusicAdmin } from "@/app/services/music";
import { useUiStore } from "@/stores/ui";
import { formatDate } from "@/utils/format";

const queryClient = useQueryClient();
const router = useRouter();
const ui = useUiStore();
const musicAdmin = useMusicAdmin();
const search = ref("");
const debounced = refDebounced(search, 300);
const page = ref(1);
const pageSize = ref(24);
const duplicatePage = ref(1);
const duplicatePageSize = ref(10);
const selected = ref<AlbumSummary>();
const editorOpen = ref(false);
const duplicatesOpen = ref(false);
const mergeOpen = ref(false);
const mergeCandidates = ref<AlbumSummary[]>([]);
const mergePreferredId = ref<string>();
const mergeFromEditor = ref(false);
const selectedDuplicateLoading = ref(false);
const groupMergeLoadingKey = ref<string>();
const actionError = ref("");
let allowEditorClose = false;
const form = reactive({ title: "", releaseDate: "", description: "", credits: [] as Array<{ artistId: string; name: string; role: CreditRole; sortOrder: number }> });
const query = useQuery({ queryKey: computed(() => ["admin", "albums", { page: page.value, pageSize: pageSize.value, search: debounced.value }]), queryFn: ({ signal }) => musicAdmin.listAlbums({ page: page.value, pageSize: pageSize.value, search: debounced.value, sort: "updatedAt", order: "desc" }, signal), placeholderData: keepPreviousData });
const duplicatesQuery = useQuery({
  queryKey: computed(() => ["admin", "albums", "duplicates", { page: duplicatePage.value, pageSize: duplicatePageSize.value }]),
  queryFn: ({ signal }) => musicAdmin.getAlbumDuplicates({ page: duplicatePage.value, pageSize: duplicatePageSize.value }, signal),
  placeholderData: keepPreviousData,
});
function changePageSize(value: number): void { pageSize.value = value; page.value = 1; }
function changeDuplicatePageSize(value: number): void { duplicatePageSize.value = value; duplicatePage.value = 1; }
function openAlbum(album: AlbumSummary): void { void router.push({ name: "album-detail", params: { id: album.id } }); }
function edit(album: AlbumSummary): void { selected.value = album; Object.assign(form, { title: album.title, releaseDate: album.releaseDate ?? "", description: album.description ?? "", credits: album.artistCredits.map((credit) => ({ artistId: credit.artist.id, name: credit.artist.name, role: credit.role, sortOrder: credit.sortOrder })) }); actionError.value = ""; editorOpen.value = true; }
async function refresh(): Promise<void> { await Promise.all([queryClient.invalidateQueries({ queryKey: ["admin", "albums"] }), queryClient.invalidateQueries({ queryKey: ["admin", "tracks"] }), queryClient.invalidateQueries({ queryKey: ["admin", "artists"] }), queryClient.invalidateQueries({ queryKey: ["admin", "dashboard"] }), queryClient.invalidateQueries({ queryKey: ["admin", "audit"] })]); }
async function artworkUploaded(): Promise<void> {
  ui.notify("success", "专辑封面已更新");
  if (selected.value) selected.value = { ...selected.value, version: selected.value.version + 1 };
  await Promise.all([
    queryClient.invalidateQueries({ queryKey: ["admin", "albums", "duplicates"] }),
    queryClient.invalidateQueries({ queryKey: ["admin", "tracks"] }),
  ]);
  const result = await query.refetch();
  const updated = result.data?.items.find((album) => album.id === selected.value?.id);
  if (updated) selected.value = updated;
}
const saveMutation = useMutation({ mutationFn: () => { if (!form.title.trim()) throw new Error("专辑标题不能为空"); if (!form.credits.length) throw new Error("专辑至少需要一个艺术家署名"); return musicAdmin.updateAlbum(selected.value!.id, { expectedVersion: selected.value!.version, title: form.title.trim(), releaseDate: form.releaseDate.trim() || null, description: form.description.trim() || null, artistCredits: form.credits.map((credit, index) => ({ artistId: credit.artistId, role: credit.role, sortOrder: index })) }); }, onSuccess: async () => { allowEditorClose = true; editorOpen.value = false; ui.notify("success", "专辑信息已更新"); await refresh(); }, onError: (error) => { actionError.value = error instanceof Error ? error.message : "专辑保存失败"; } });
async function openSelectedMerge(): Promise<void> {
  if (!selected.value) return;
  selectedDuplicateLoading.value = true;
  try {
    const group = await musicAdmin.getCompleteAlbumDuplicateGroup(selected.value.id);
    if (!group) {
      ui.notify("info", "当前没有可合并的同名专辑");
      return;
    }
    mergeCandidates.value = group.albums;
    mergePreferredId.value = selected.value.id;
    mergeFromEditor.value = true;
    mergeOpen.value = true;
  } catch (error) {
    ui.notify("error", error instanceof Error ? error.message : "无法读取同名专辑");
  } finally {
    selectedDuplicateLoading.value = false;
  }
}
async function openGroupMerge(group: AlbumDuplicateGroup): Promise<void> {
  const albumId = group.albums[0]?.id;
  if (!albumId) return;
  groupMergeLoadingKey.value = group.key;
  try {
    const complete = await musicAdmin.getCompleteAlbumDuplicateGroup(albumId);
    if (!complete) {
      ui.notify("info", "该同名专辑组已不存在，请刷新后重试");
      return;
    }
    mergeCandidates.value = complete.albums;
    mergePreferredId.value = albumId;
    mergeFromEditor.value = false;
    mergeOpen.value = true;
  } catch (error) {
    ui.notify("error", error instanceof Error ? error.message : "无法完整加载同名专辑组");
  } finally {
    groupMergeLoadingKey.value = undefined;
  }
}
async function merged(): Promise<void> {
  duplicatesOpen.value = false;
  if (mergeFromEditor.value) { allowEditorClose = true; editorOpen.value = false; }
  await refresh();
}
watch(editorOpen, (value) => { if (!value && saveMutation.isPending.value && !allowEditorClose) editorOpen.value = true; allowEditorClose = false; });
</script>

<template>
  <div class="space-y-6 page-enter">
    <PageHeader title="专辑" description="维护专辑标题、艺术家署名、发行日期与简介。"><template #eyebrow>音乐资料库</template><template #actions><AppButton :loading="query.isFetching.value" @click="query.refetch()"><template #icon><RefreshCw :size="16" /></template>刷新</AppButton></template></PageHeader>
    <nav class="flex gap-1 overflow-x-auto rounded-xl bg-[var(--surface-muted)] p-1 sm:w-max"><RouterLink class="rounded-lg px-4 py-2 text-sm font-semibold text-[var(--muted)]" to="/music/tracks">曲目</RouterLink><RouterLink class="rounded-lg bg-[var(--surface-solid)] px-4 py-2 text-sm font-bold text-[var(--primary)] shadow-sm" to="/music/albums">专辑</RouterLink><RouterLink class="rounded-lg px-4 py-2 text-sm font-semibold text-[var(--muted)]" to="/music/artists">艺术家</RouterLink></nav>
    <section v-if="duplicatesQuery.data.value?.duplicateAlbumCount" class="motion-item flex flex-col gap-3 rounded-2xl border border-amber-500/30 bg-amber-500/10 p-4 sm:flex-row sm:items-center"><AlertTriangle :size="20" class="shrink-0 text-amber-500" /><div class="flex-1"><p class="font-bold">当前有 {{ duplicatesQuery.data.value.groupCount }} 组同名专辑，是否合并？</p><p class="mt-1 text-xs text-[var(--muted)]">共 {{ duplicatesQuery.data.value.duplicateAlbumCount }} 个可合并专辑；只按规范化后的专辑名称分组，不限制艺术家。</p></div><AppButton @click="duplicatesOpen = true"><template #icon><GitMerge :size="16" /></template>选择并合并</AppButton></section>
    <section class="ui-card overflow-hidden"><div class="border-b border-[var(--border)] p-4"><div class="relative"><Search :size="16" class="absolute left-3.5 top-1/2 -translate-y-1/2 text-[var(--muted)]" /><input v-model="search" class="ui-input !pl-10" type="search" placeholder="搜索专辑或艺术家" @input="page = 1" /></div></div><StatePanel v-if="query.isPending.value" state="loading" /><StatePanel v-else-if="query.isError.value" state="error" @retry="query.refetch()" /><StatePanel v-else-if="!query.data.value?.items.length" state="empty" title="没有符合条件的专辑" /><template v-else><div class="grid gap-px bg-[var(--border)] sm:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4"><article v-for="(album, index) in query.data.value.items" :key="album.id" class="group motion-item interactive-tile flex cursor-pointer gap-4 bg-[var(--surface-solid)] p-4 hover:bg-[var(--surface-muted)]" :style="{ '--motion-index': index }" role="link" tabindex="0" @click="openAlbum(album)" @keydown.enter.self="openAlbum(album)" @keydown.space.prevent.self="openAlbum(album)"><span class="grid h-20 w-20 shrink-0 place-items-center overflow-hidden rounded-xl bg-[var(--surface-muted)]"><img v-if="album.artwork" :src="album.artwork.url" :alt="`${album.title} 封面`" class="media-artwork h-full w-full object-cover" width="80" height="80" loading="lazy" decoding="async" /><AlbumIcon v-else :size="24" /></span><div class="min-w-0 flex-1"><div class="flex items-start justify-between"><div class="min-w-0"><h3 class="truncate font-bold">{{ album.title }}</h3><p class="mt-1 truncate text-xs text-[var(--muted)]">{{ album.artistCredits.map((credit) => credit.artist.name).join('、') || '未知艺术家' }}</p></div><button class="btn btn-ghost btn-icon" type="button" aria-label="编辑专辑" @click.stop="edit(album)"><Pencil :size="15" /></button></div><div class="mt-4 flex items-center justify-end"><span class="text-xs text-[var(--muted)]">{{ album.trackCount }} 首</span></div></div></article></div><AppPagination :page="page" :page-size="pageSize" :total="query.data.value.total" @change="page = $event" @page-size-change="changePageSize" /></template></section>
    <BaseDialog v-model="editorOpen" title="编辑专辑" :description="selected ? `更新于 ${formatDate(selected.updatedAt)}` : ''" width="lg"><div v-if="selected" class="grid gap-6 sm:grid-cols-[120px_1fr]"><ArtworkUploadField :target-id="selected.id" purpose="ALBUM_ARTWORK" :image-url="selected.artwork?.url" alt="专辑封面" @completed="artworkUploaded"><AlbumIcon :size="30" /></ArtworkUploadField><div class="space-y-5"><div><label class="ui-label">专辑标题</label><input v-model="form.title" class="ui-input" /></div><div><label class="ui-label">发行日期</label><input v-model="form.releaseDate" class="ui-input" type="date" /></div><div><label class="ui-label">艺术家署名</label><div class="space-y-2"><div v-for="(credit, index) in form.credits" :key="`${credit.artistId}:${index}`" class="flex items-center gap-2 rounded-xl bg-[var(--surface-muted)] p-2"><span class="min-w-0 flex-1 truncate font-semibold">{{ credit.name }}</span><select v-model="credit.role" class="ui-select !w-40"><option value="PRIMARY">主要艺术家</option><option value="FEATURED">合作艺术家</option><option value="COMPOSER">作曲</option><option value="LYRICIST">作词</option><option value="PRODUCER">制作人</option></select></div></div></div><div><label class="ui-label">专辑简介</label><textarea v-model="form.description" class="ui-textarea" /></div></div></div><p v-if="actionError" class="mt-5 rounded-xl bg-rose-500/10 p-3 text-sm text-[var(--danger)]">{{ actionError }}</p><template #footer><AppButton :loading="selectedDuplicateLoading" @click="openSelectedMerge"><template #icon><GitMerge :size="15" /></template>合并同名专辑</AppButton><span class="flex-1" /><AppButton @click="editorOpen = false">取消</AppButton><AppButton variant="primary" :loading="saveMutation.isPending.value" @click="saveMutation.mutate()">保存专辑</AppButton></template></BaseDialog>
    <BaseDialog v-model="duplicatesOpen" title="同名专辑" :description="duplicatesQuery.data.value ? `${duplicatesQuery.data.value.groupCount} 组同名专辑` : ''" width="2xl"><div class="space-y-4"><article v-for="group in duplicatesQuery.data.value?.groups ?? []" :key="group.key" class="rounded-xl border border-[var(--border)] p-4"><div class="flex flex-col gap-3 sm:flex-row sm:items-start"><div class="min-w-0 flex-1"><h3 class="font-bold">{{ group.title }}</h3><p class="mt-1 text-xs text-[var(--muted)]">涉及艺术家：{{ group.primaryArtists.map((artist) => artist.name).join('、') || '无主要艺术家' }}</p><p v-if="group.albumTotal > group.albums.length" class="mt-1 text-xs text-[var(--muted)]">当前展示 {{ group.albums.length }} / {{ group.albumTotal }} 张；打开合并时会按页加载完整候选。</p></div><AppButton :loading="groupMergeLoadingKey === group.key" @click="openGroupMerge(group)"><template #icon><GitMerge :size="15" /></template>选择并合并</AppButton></div><div class="mt-3 grid gap-2 md:grid-cols-2"><div v-for="album in group.albums" :key="album.id" class="rounded-xl bg-[var(--surface-muted)] p-3"><p class="truncate font-semibold">{{ album.title }}</p><p class="mt-1 truncate text-xs text-[var(--muted)]">{{ album.artistCredits.map((credit) => credit.artist.name).join('、') || '无艺术家' }}</p><p class="mt-1 text-xs text-[var(--muted)]">{{ album.trackCount }} 首 · 创建于 {{ formatDate(album.createdAt) }} · {{ album.id.slice(0, 8) }}</p></div></div></article></div><AppPagination v-if="duplicatesQuery.data.value?.total" :page="duplicatePage" :page-size="duplicatePageSize" :total="duplicatesQuery.data.value.total" :total-pages="duplicatesQuery.data.value.totalPages" @change="duplicatePage = $event" @page-size-change="changeDuplicatePageSize" /><template #footer><AppButton @click="duplicatesOpen = false">关闭</AppButton></template></BaseDialog>
    <AlbumMergeDialog v-model="mergeOpen" :albums="mergeCandidates" :preferred-album-id="mergePreferredId" @merged="merged" />
  </div>
</template>
