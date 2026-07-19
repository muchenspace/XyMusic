<script setup lang="ts">
import type { Album, Artist, SearchResults, SearchScope, Track } from "../../domain/music";
import AlbumRow from "../components/AlbumRow.vue";
import ArtistGrid from "../components/ArtistGrid.vue";
import TrackTable from "../components/TrackTable.vue";

defineProps<{
  query: string;
  resultsQuery: string;
  results: SearchResults | null;
  searching: boolean;
  loadingMore: SearchScope | null;
  albumPlayLoadingId?: string;
  currentId?: string;
  isPlaying: boolean;
}>();

defineEmits<{
  playTrack: [track: Track];
  toggle: [];
  favorite: [track: Track];
  add: [track: Track];
  playAlbum: [album: Album];
  openAlbum: [album: Album];
  openArtist: [artist: Artist];
  loadMore: [scope: SearchScope];
}>();
</script>

<template>
  <section class="page-intro search-intro">
    <p class="eyebrow">搜索结果</p>
    <h1>“{{ query }}”</h1>
    <p>在歌曲、专辑和歌手中查找匹配内容。</p>
  </section>
  <div v-if="!results" class="empty-state" role="status" aria-live="polite">{{ searching ? "正在搜索…" : "没有可显示的搜索结果" }}</div>
  <template v-else>
    <p v-if="resultsQuery !== query.trim()" class="search-results-status" role="status" aria-live="polite">
      {{ searching ? `正在搜索“${query.trim()}”，当前仍显示“${resultsQuery}”的结果。` : `当前显示“${resultsQuery}”的结果。` }}
    </p>
    <TrackTable v-if="results.tracks.length" :tracks="results.tracks" title="歌曲" description="匹配的歌曲" empty-title="没有找到歌曲" empty-description="尝试更换关键词。" :current-id="currentId" :is-playing="isPlaying" @play="$emit('playTrack', $event)" @toggle="$emit('toggle')" @favorite="$emit('favorite', $event)" @add="$emit('add', $event)" />
    <div v-if="results.nextCursors?.tracks" class="pagination-footer"><button type="button" class="secondary-button" :disabled="searching || resultsQuery !== query.trim() || loadingMore !== null" @click="$emit('loadMore', 'tracks')">{{ loadingMore === 'tracks' ? '正在加载…' : '加载更多歌曲' }}</button></div>
    <ArtistGrid v-if="results.artists.length" :artists="results.artists" @open="$emit('openArtist', $event)" />
    <div v-if="results.nextCursors?.artists" class="pagination-footer"><button type="button" class="secondary-button" :disabled="searching || resultsQuery !== query.trim() || loadingMore !== null" @click="$emit('loadMore', 'artists')">{{ loadingMore === 'artists' ? '正在加载…' : '加载更多歌手' }}</button></div>
    <AlbumRow v-if="results.albums.length" :albums="results.albums" title="专辑" description="匹配的专辑" :play-loading-id="albumPlayLoadingId" @play="$emit('playAlbum', $event)" @open="$emit('openAlbum', $event)" />
    <div v-if="results.nextCursors?.albums" class="pagination-footer"><button type="button" class="secondary-button" :disabled="searching || resultsQuery !== query.trim() || loadingMore !== null" @click="$emit('loadMore', 'albums')">{{ loadingMore === 'albums' ? '正在加载…' : '加载更多专辑' }}</button></div>
    <div v-if="!results.tracks.length && !results.artists.length && !results.albums.length" class="empty-state" role="status">没有找到相关内容</div>
  </template>
</template>
