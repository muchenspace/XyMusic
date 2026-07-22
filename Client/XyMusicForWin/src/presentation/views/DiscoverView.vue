<script setup lang="ts">
import type { Album, HomeFeed, Playlist, Track } from "../../domain/music";
import AlbumRow from "../components/AlbumRow.vue";
import FeaturedHero from "../components/FeaturedHero.vue";
import PlaylistRow from "../components/PlaylistRow.vue";
import RandomTrackGrid from "../components/RandomTrackGrid.vue";
import EmptyState from "../components/ui/EmptyState.vue";
import LoadingSkeleton from "../components/ui/LoadingSkeleton.vue";

defineProps<{
  feed: HomeFeed;
  randomAlbums: Album[];
  randomTracks: Track[];
  randomAlbumsLoading: boolean;
  randomTracksLoading: boolean;
  randomAlbumsError: string;
  randomTracksError: string;
  albumPlayLoadingId?: string;
  playlistPlayLoadingId?: string;
  currentId?: string;
  isPlaying: boolean;
}>();

defineEmits<{
  playAlbum: [album: Album];
  openAlbum: [album: Album];
  playTrack: [track: Track];
  toggle: [];
  favorite: [track: Track];
  add: [track: Track];
  playPlaylist: [playlist: Playlist];
  openPlaylist: [playlist: Playlist];
  retryRandomAlbums: [];
  retryRandomTracks: [];
}>();
</script>

<template>
  <FeaturedHero v-if="feed.featured" :album="feed.featured" :play-loading="albumPlayLoadingId === feed.featured.id" @play="$emit('playAlbum', $event)" @open="$emit('openAlbum', $event)" />

  <section v-if="randomAlbumsError" class="content-section" aria-labelledby="random-albums-heading">
    <div class="section-heading"><div><h2 id="random-albums-heading">今日精选</h2></div></div>
    <EmptyState title="今日精选加载失败" :description="randomAlbumsError" compact>
      <template #actions><button type="button" class="secondary-button" @click="$emit('retryRandomAlbums')">重试</button></template>
    </EmptyState>
  </section>
  <AlbumRow v-else-if="randomAlbums.length" :albums="randomAlbums" title="今日精选" description="" :play-loading-id="albumPlayLoadingId" @play="$emit('playAlbum', $event)" @open="$emit('openAlbum', $event)" />
  <section v-else class="content-section" aria-labelledby="random-albums-heading">
    <div class="section-heading"><div><h2 id="random-albums-heading">今日精选</h2></div></div>
    <LoadingSkeleton v-if="randomAlbumsLoading" :count="3" label="正在加载今日精选" compact />
    <EmptyState v-else title="暂无精选专辑" description="资料库中暂时没有可显示的专辑。" compact />
  </section>

  <section v-if="randomTracksError" class="content-section random-tracks-section" aria-labelledby="random-tracks-heading">
    <div class="section-heading"><div><h2 id="random-tracks-heading">猜你喜欢</h2></div></div>
    <EmptyState title="猜你喜欢加载失败" :description="randomTracksError" compact>
      <template #actions><button type="button" class="secondary-button" @click="$emit('retryRandomTracks')">重试</button></template>
    </EmptyState>
  </section>
  <RandomTrackGrid v-else-if="randomTracks.length" :tracks="randomTracks" :current-id="currentId" :is-playing="isPlaying" @play="$emit('playTrack', $event)" @toggle="$emit('toggle')" @favorite="$emit('favorite', $event)" @add="$emit('add', $event)" />
  <section v-else class="content-section random-tracks-section" aria-labelledby="random-tracks-heading">
    <div class="section-heading"><div><h2 id="random-tracks-heading">猜你喜欢</h2></div></div>
    <LoadingSkeleton v-if="randomTracksLoading" :count="2" label="正在加载猜你喜欢" compact />
    <EmptyState v-else title="暂无可播放歌曲" description="资料库中暂时没有可播放歌曲。" compact />
  </section>

  <PlaylistRow :playlists="feed.playlists" :play-loading-id="playlistPlayLoadingId" @play="$emit('playPlaylist', $event)" @open="$emit('openPlaylist', $event)" />
</template>
