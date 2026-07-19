<script setup lang="ts">
import { ArrowLeft, Disc3, FileAudio, GitMerge, RefreshCw } from "lucide-vue-next";
import { useQuery } from "@tanstack/vue-query";
import { computed, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import AlbumMergeDialog from "@/components/AlbumMergeDialog.vue";
import AppButton from "@/components/AppButton.vue";
import AppPagination from "@/components/AppPagination.vue";
import AudioStatusBadge from "@/components/AudioStatusBadge.vue";
import PageHeader from "@/components/PageHeader.vue";
import StatePanel from "@/components/StatePanel.vue";
import { useMusicAdmin } from "@/app/services/music";
import type { AlbumMergeResult, AlbumSummary } from "@/features/music/domain/models";
import { useUiStore } from "@/stores/ui";
import { formatDuration } from "@/utils/format";

const route = useRoute();
const router = useRouter();
const musicAdmin = useMusicAdmin();
const ui = useUiStore();
const albumId = computed(() => String(route.params.id));
const mergeOpen = ref(false);
const mergeLoading = ref(false);
const mergeCandidates = ref<AlbumSummary[]>([]);
const trackPage = ref(1);
const trackPageSize = ref(25);
const query = useQuery({
  queryKey: computed(() => ["admin", "album", albumId.value, { page: trackPage.value, pageSize: trackPageSize.value }]),
  queryFn: ({ signal }) => musicAdmin.getAlbum(albumId.value, trackPage.value, trackPageSize.value, signal),
});
const duplicatesQuery = useQuery({
  queryKey: computed(() => ["admin", "albums", "duplicates", { albumId: albumId.value }]),
  queryFn: ({ signal }) => musicAdmin.getAlbumDuplicates({ page: 1, pageSize: 1, albumId: albumId.value }, signal),
});
const duplicateGroup = computed(() => duplicatesQuery.data.value?.groups[0]);

watch(albumId, () => { trackPage.value = 1; mergeCandidates.value = []; mergeOpen.value = false; });

async function openMerge(): Promise<void> {
  if (!duplicateGroup.value) {
    ui.notify("info", "当前没有可合并的同名专辑");
    return;
  }
  mergeLoading.value = true;
  try {
    const group = await musicAdmin.getCompleteAlbumDuplicateGroup(albumId.value);
    if (!group) {
      ui.notify("info", "当前没有可合并的同名专辑");
      return;
    }
    mergeCandidates.value = group.albums;
    mergeOpen.value = true;
  } catch (error) {
    ui.notify("error", error instanceof Error ? error.message : "无法完整加载同名专辑组");
  } finally {
    mergeLoading.value = false;
  }
}

async function merged(result: AlbumMergeResult): Promise<void> {
  if (result.targetAlbumId !== albumId.value) {
    await router.replace({ name: "album-detail", params: { id: result.targetAlbumId } });
  } else {
    await query.refetch();
  }
}

function trackPosition(discNumber: number, trackNumber: number | null): string {
  if (trackNumber === null) return "—";
  return discNumber > 1 ? `${discNumber}-${trackNumber}` : String(trackNumber);
}

function changeTrackPageSize(value: number): void {
  trackPageSize.value = value;
  trackPage.value = 1;
}
</script>

<template>
  <div class="space-y-6 page-enter">
    <RouterLink class="inline-flex items-center gap-2 text-sm font-semibold text-[var(--muted)] hover:text-[var(--primary)]" to="/music/albums">
      <ArrowLeft :size="16" />返回专辑
    </RouterLink>
    <PageHeader :title="query.data.value?.title ?? '专辑详情'" description="查看专辑资料及全部曲目。">
      <template #eyebrow>音乐资料库</template>
      <template #actions>
        <AppButton :loading="duplicatesQuery.isFetching.value || mergeLoading" @click="openMerge">
          <template #icon><GitMerge :size="16" /></template>合并同名专辑
        </AppButton>
        <AppButton :loading="query.isFetching.value" @click="query.refetch()">
          <template #icon><RefreshCw :size="16" /></template>刷新
        </AppButton>
      </template>
    </PageHeader>

    <StatePanel v-if="query.isPending.value" state="loading" />
    <StatePanel v-else-if="query.isError.value" state="error" @retry="query.refetch()" />
    <template v-else-if="query.data.value">
      <section class="ui-card grid gap-6 p-6 sm:grid-cols-[160px_1fr]">
        <span class="grid aspect-square w-full place-items-center overflow-hidden rounded-2xl bg-[var(--surface-muted)]">
          <img v-if="query.data.value.artwork" :src="query.data.value.artwork.url" :alt="`${query.data.value.title} 封面`" class="h-full w-full object-cover" decoding="async" />
          <Disc3 v-else :size="42" />
        </span>
        <div class="min-w-0 self-center">
          <h2 class="text-2xl font-black">{{ query.data.value.title }}</h2>
          <p class="mt-2 text-sm text-[var(--muted)]">{{ query.data.value.artistCredits.map((credit) => credit.artist.name).join('、') || '未知艺术家' }}</p>
          <div class="mt-4 flex flex-wrap gap-x-5 gap-y-2 text-sm text-[var(--muted)]">
            <span>{{ query.data.value.releaseDate ?? '发行日期未知' }}</span>
            <span>{{ query.data.value.trackTotal }} 首曲目</span>
          </div>
          <p v-if="query.data.value.description" class="mt-5 max-w-3xl whitespace-pre-wrap text-sm leading-7 text-[var(--muted)]">{{ query.data.value.description }}</p>
        </div>
      </section>

      <section class="ui-card overflow-hidden">
        <div class="border-b border-[var(--border)] p-4">
          <h2 class="font-bold">专辑曲目</h2>
        </div>
        <StatePanel v-if="!query.data.value.tracks.length" state="empty" title="该专辑暂无曲目" />
        <div v-else class="overflow-x-auto">
          <table class="data-table min-w-[760px]">
            <thead><tr><th class="w-20">音轨</th><th>曲目</th><th>时长 / 格式</th><th>音频状态</th></tr></thead>
            <tbody>
              <tr v-for="track in query.data.value.tracks" :key="track.id">
                <td class="font-mono text-xs">{{ trackPosition(track.discNumber, track.trackNumber) }}</td>
                <td><div class="flex items-center gap-3"><span class="grid h-10 w-10 place-items-center overflow-hidden rounded-lg bg-[var(--surface-muted)]"><img v-if="track.artwork" :src="track.artwork.url" class="h-full w-full object-cover" alt="封面" width="40" height="40" loading="lazy" decoding="async" /><FileAudio v-else :size="17" /></span><div class="min-w-0"><p class="max-w-md truncate font-semibold">{{ track.title }}</p><p class="mt-0.5 max-w-md truncate text-xs text-[var(--muted)]">{{ track.artists.join('、') || '未知艺术家' }}</p></div></div></td>
                <td><p class="font-mono text-xs">{{ formatDuration(track.durationMs) }}</p><p class="text-[10px] text-[var(--muted)]">{{ track.source?.format ?? '—' }}</p></td>
                <td><AudioStatusBadge :status="track.audioStatus" :source-status="track.source?.status" /></td>
              </tr>
            </tbody>
          </table>
        </div>
        <AppPagination
          v-if="query.data.value.trackTotal"
          :page="trackPage"
          :page-size="trackPageSize"
          :total="query.data.value.trackTotal"
          :total-pages="query.data.value.trackTotalPages"
          @change="trackPage = $event"
          @page-size-change="changeTrackPageSize"
        />
      </section>
      <AlbumMergeDialog
        v-model="mergeOpen"
        :albums="mergeCandidates"
        :preferred-album-id="albumId"
        @merged="merged"
      />
    </template>
  </div>
</template>
