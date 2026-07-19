<script setup lang="ts">
import { GitMerge } from "lucide-vue-next";
import { useMutation, useQueryClient } from "@tanstack/vue-query";
import { computed, reactive, watch } from "vue";
import { ApiError } from "@/shared/application/api-error";
import { buildAlbumMergeCommand, createAlbumMergeDraft } from "@/features/music/application/album-merge";
import type { AlbumMergeResult, AlbumSummary } from "@/features/music/domain/models";
import { useMusicAdmin } from "@/app/services/music";
import { useUiStore } from "@/stores/ui";
import { formatDate } from "@/utils/format";
import AppButton from "./AppButton.vue";
import BaseDialog from "./BaseDialog.vue";

const open = defineModel<boolean>({ required: true });
const props = defineProps<{
  albums: AlbumSummary[];
  preferredAlbumId?: string;
}>();
const emit = defineEmits<{ merged: [result: AlbumMergeResult] }>();
const musicAdmin = useMusicAdmin();
const queryClient = useQueryClient();
const ui = useUiStore();
const draft = reactive(createAlbumMergeDraft(props.albums, props.preferredAlbumId));
const selectedAlbums = computed(() => props.albums.filter((album) => draft.selectedIds.includes(album.id)));

watch([open, () => props.albums, () => props.preferredAlbumId], ([isOpen]) => {
  if (isOpen) Object.assign(draft, createAlbumMergeDraft(props.albums, props.preferredAlbumId));
}, { deep: true });

watch(() => [...draft.selectedIds], (ids) => {
  const selected = new Set(ids);
  const fallback = ids[0] ?? "";
  if (!selected.has(draft.survivorId)) draft.survivorId = fallback;
  if (!selected.has(draft.fieldSources.title)) draft.fieldSources.title = fallback;
  if (!selected.has(draft.fieldSources.artistCredits)) draft.fieldSources.artistCredits = fallback;
  for (const field of ["cover", "releaseDate", "description"] as const) {
    const source = draft.fieldSources[field];
    if (source !== null && !selected.has(source)) draft.fieldSources[field] = fallback || null;
  }
});

const mutation = useMutation({
  mutationFn: () => musicAdmin.mergeAlbums(buildAlbumMergeCommand(props.albums, draft)),
  onSuccess: async (result) => {
    open.value = false;
    ui.notify("success", `已合并 ${result.mergedAlbums} 个专辑，迁移 ${result.movedTracks} 首曲目`);
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: ["admin", "albums"] }),
      queryClient.invalidateQueries({ queryKey: ["admin", "tracks"] }),
    ]);
    emit("merged", result);
  },
});
const errorMessage = computed(() => mutation.error.value instanceof ApiError || mutation.error.value instanceof Error
  ? mutation.error.value.message
  : "");
watch(open, (isOpen) => { if (isOpen) mutation.reset(); });

function albumIdentity(album: AlbumSummary): string {
  const artists = album.artistCredits.filter((credit) => credit.role === "PRIMARY")
    .map((credit) => credit.artist.name).join("、") || "无主要艺术家";
  return `${artists} · ${album.trackCount} 首 · ${album.id.slice(0, 8)}`;
}

function creditsLabel(album: AlbumSummary): string {
  return album.artistCredits.map((credit) => `${credit.artist.name}（${credit.role}）`).join("、") || "（空）";
}

function descriptionLabel(album: AlbumSummary): string {
  const value = album.description?.trim();
  return value ? `${value.slice(0, 60)}${value.length > 60 ? "…" : ""}` : "（空）";
}
</script>

<template>
  <BaseDialog
    v-model="open"
    title="合并同名专辑"
    description="选择参与专辑、最终保留的专辑记录，以及每项信息的来源。曲目会迁移到保留专辑，其余专辑会删除。"
    width="2xl"
    :prevent-close="mutation.isPending.value"
  >
    <div class="space-y-6">
      <section>
        <h3 class="text-sm font-bold">1. 选择参与专辑（至少 2 个）</h3>
        <div class="mt-3 grid gap-2 md:grid-cols-2">
          <label v-for="album in albums" :key="album.id" class="flex cursor-pointer items-start gap-3 rounded-xl border border-[var(--border)] p-3 hover:bg-[var(--surface-muted)]">
            <input v-model="draft.selectedIds" class="mt-1" type="checkbox" :value="album.id" />
            <span class="min-w-0">
              <span class="block truncate font-semibold">{{ album.title }}</span>
              <span class="mt-1 block text-xs text-[var(--muted)]">{{ albumIdentity(album) }} · {{ formatDate(album.createdAt) }}</span>
            </span>
          </label>
        </div>
      </section>

      <section>
        <h3 class="text-sm font-bold">2. 选择最终保留的专辑</h3>
        <div class="mt-3 grid gap-2 md:grid-cols-2">
          <label v-for="album in albums" :key="album.id" class="flex items-start gap-3 rounded-xl bg-[var(--surface-muted)] p-3" :class="!draft.selectedIds.includes(album.id) && 'opacity-45'">
            <input v-model="draft.survivorId" class="mt-1" type="radio" :value="album.id" :disabled="!draft.selectedIds.includes(album.id)" />
            <span><span class="block font-semibold">{{ album.title }}</span><span class="text-xs text-[var(--muted)]">{{ albumIdentity(album) }}</span></span>
          </label>
        </div>
      </section>

      <section>
        <h3 class="text-sm font-bold">3. 选择合并后的信息来源</h3>
        <p class="mt-1 text-xs text-[var(--muted)]">“清空（空）”会明确移除该字段；选择本身为空的专辑也会显示“（空）”。</p>
        <div class="mt-3 grid gap-4 md:grid-cols-2">
          <label class="block"><span class="ui-label">标题</span><select v-model="draft.fieldSources.title" class="ui-select"><option v-for="album in selectedAlbums" :key="album.id" :value="album.id">{{ album.title }} · {{ albumIdentity(album) }}</option></select></label>
          <label class="block"><span class="ui-label">艺术家署名</span><select v-model="draft.fieldSources.artistCredits" class="ui-select"><option v-for="album in selectedAlbums" :key="album.id" :value="album.id">{{ creditsLabel(album) }} · {{ album.id.slice(0, 8) }}</option></select></label>
          <label class="block"><span class="ui-label">封面</span><select v-model="draft.fieldSources.cover" class="ui-select"><option :value="null">清空（空）</option><option v-for="album in selectedAlbums" :key="album.id" :value="album.id">{{ album.artwork ? '使用该封面' : '（空）' }} · {{ albumIdentity(album) }}</option></select></label>
          <label class="block"><span class="ui-label">发行日期</span><select v-model="draft.fieldSources.releaseDate" class="ui-select"><option :value="null">清空（空）</option><option v-for="album in selectedAlbums" :key="album.id" :value="album.id">{{ album.releaseDate ?? '（空）' }} · {{ albumIdentity(album) }}</option></select></label>
          <label class="block md:col-span-2"><span class="ui-label">简介</span><select v-model="draft.fieldSources.description" class="ui-select"><option :value="null">清空（空）</option><option v-for="album in selectedAlbums" :key="album.id" :value="album.id">{{ descriptionLabel(album) }} · {{ albumIdentity(album) }}</option></select></label>
        </div>
      </section>

      <p class="rounded-xl bg-amber-500/10 p-3 text-sm text-amber-700 dark:text-amber-300">合并不可撤销。提交时会检查所有参与专辑的版本，期间被修改的专辑不会被合并。</p>
      <p v-if="errorMessage" class="rounded-xl bg-rose-500/10 p-3 text-sm text-[var(--danger)]">{{ errorMessage }}</p>
    </div>
    <template #footer>
      <AppButton :disabled="mutation.isPending.value" @click="open = false">取消</AppButton>
      <AppButton variant="primary" :loading="mutation.isPending.value" :disabled="draft.selectedIds.length < 2" @click="mutation.mutate()">
        <template #icon><GitMerge :size="16" /></template>确认合并
      </AppButton>
    </template>
  </BaseDialog>
</template>
