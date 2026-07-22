<script setup lang="ts">
import { computed } from "vue";
import { ArrowLeft, LoaderCircle } from "@lucide/vue";
import type { Album, Artist, Playlist, PlaylistEntry, Track } from "../../domain/music";
import TrackTable from "../components/TrackTable.vue";
import PaginationFooter from "../components/ui/PaginationFooter.vue";
import ArtworkImage from "../components/ui/ArtworkImage.vue";

const props = defineProps<{
  heading: string;
  tracks: Track[];
  entries?: PlaylistEntry[];
  playlist?: Playlist | null;
  album?: Album | null;
  artist?: Artist | null;
  currentId?: string;
  currentEntryId?: string;
  isPlaying: boolean;
  playlistBusy?: boolean;
  playAllLoading?: boolean;
  error?: string;
  reorderDisabled?: boolean;
  hasMore: boolean;
  loadingMore: boolean;
  pageKey?: string | null;
}>();

const artwork = computed(() => props.album?.coverUrl || props.playlist?.coverUrl || props.artist?.artwork?.url || "");
const artworkKind = computed(() => props.artist ? "artist" as const : "album" as const);
const description = computed(() => props.album?.description || props.artist?.description || props.playlist?.description || "");
const metadata = computed(() => {
  if (props.album) return [props.album.artist, props.album.year, `${props.album.trackCount} 首歌曲`].filter(Boolean).join(" · ");
  if (props.playlist) return `${visibilityLabels[props.playlist.visibility]} · ${props.playlist.trackCount} 首歌曲`;
  return `已加载 ${props.tracks.length} 首歌曲`;
});

const visibilityLabels = { PRIVATE: "私密歌单", UNLISTED: "不公开列出", PUBLIC: "公开歌单" } as const;

defineEmits<{
  back: [];
  playAll: [];
  editPlaylist: [playlist: Playlist];
  play: [track: Track, index: number];
  toggle: [];
  favorite: [track: Track];
  add: [track: Track];
  remove: [entryId: string];
  removeSelected: [entryIds: string[]];
  move: [entryId: string, direction: -1 | 1];
  reorder: [orderedEntryIds: string[]];
  loadMore: [];
}>();
</script>

<template>
  <div class="collection-header">
    <button class="icon-button" type="button" title="返回" aria-label="返回" @click="$emit('back')"><ArrowLeft :size="18" /></button>
    <ArtworkImage v-if="artwork" :src="artwork" :alt="`${heading}封面`" :kind="artworkKind" loading="eager" />
    <div class="collection-copy"><p class="eyebrow">{{ album ? "专辑" : artist ? "歌手" : playlist ? "歌单" : "音乐集合" }}</p><h1>{{ heading }}</h1><p class="collection-meta">{{ metadata }}</p><p v-if="description" class="collection-description">{{ description }}</p></div>
    <div v-if="tracks.length || playlist" class="collection-actions">
      <button v-if="tracks.length" class="primary-button" type="button" :aria-busy="playAllLoading" :disabled="playAllLoading" @click="$emit('playAll')"><LoaderCircle v-if="playAllLoading" class="spin" :size="16" aria-hidden="true" />{{ playAllLoading ? "正在准备…" : "播放全部" }}</button>
      <button v-if="playlist" class="secondary-button" type="button" @click="$emit('editPlaylist', playlist)">编辑歌单</button>
    </div>
  </div>
  <TrackTable :tracks="tracks" :entries="entries" :current-id="currentId" :current-entry-id="currentEntryId" :is-playing="isPlaying" :busy="playlistBusy" :reorder-disabled="reorderDisabled" empty-title="这个集合还没有歌曲" empty-description="稍后添加歌曲或返回音乐库浏览其他内容。" @play="(track, index) => $emit('play', track, index)" @toggle="$emit('toggle')" @favorite="$emit('favorite', $event)" @add="$emit('add', $event)" @remove="$emit('remove', $event)" @remove-selected="$emit('removeSelected', $event)" @move="(id, direction) => $emit('move', id, direction)" @reorder="$emit('reorder', $event)" />
  <PaginationFooter :has-more="hasMore" :loading="loadingMore" :error="hasMore ? error : ''" :page-key="pageKey" @load-more="$emit('loadMore')" />
</template>
