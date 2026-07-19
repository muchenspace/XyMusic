<script setup lang="ts">
import { Clock3, Heart, ListPlus, Pause, Play } from "@lucide/vue";
import type { Track } from "../../domain/music";
import ArtworkImage from "./ui/ArtworkImage.vue";
import EmptyState from "./ui/EmptyState.vue";

const props = defineProps<{
  tracks: Track[];
  currentId?: string;
  isPlaying: boolean;
}>();

const emit = defineEmits<{
  play: [track: Track];
  toggle: [];
  favorite: [track: Track];
  add: [track: Track];
}>();

function toggleTrack(track: Track): void {
  if (props.currentId === track.id) emit("toggle");
  else emit("play", track);
}

function formatTime(seconds: number): string {
  const safeSeconds = Number.isFinite(seconds) && seconds > 0 ? seconds : 0;
  return `${Math.floor(safeSeconds / 60)}:${String(Math.floor(safeSeconds % 60)).padStart(2, "0")}`;
}
</script>

<template>
  <section class="content-section random-tracks-section" aria-label="随心听">
    <div class="section-heading"><div><h2>随机播放</h2><p>从资料库中随机选取的歌曲</p></div></div>
    <div class="random-track-grid">
      <article v-for="track in tracks" :key="track.id" class="random-track-card" :class="{ current: currentId === track.id }">
        <button type="button" class="random-track-main" :aria-label="currentId === track.id && isPlaying ? `暂停《${track.title}》` : `播放《${track.title}》`" @click="toggleTrack(track)">
          <ArtworkImage :src="track.coverUrl" :alt="`${track.title}封面`" kind="track" />
          <span class="random-track-copy">
            <strong>{{ track.title }}</strong>
            <small>{{ track.artist }}</small>
            <span>{{ track.album || "未知专辑" }}</span>
          </span>
          <span class="random-track-play" aria-hidden="true">
            <Pause v-if="currentId === track.id && isPlaying" :size="18" fill="currentColor" />
            <Play v-else :size="18" fill="currentColor" />
          </span>
        </button>
        <div class="random-track-actions">
          <span class="random-track-duration"><Clock3 :size="13" aria-hidden="true" />{{ formatTime(track.duration) }}</span>
          <button type="button" :class="{ liked: track.liked }" :title="track.liked ? `取消收藏《${track.title}》` : `收藏《${track.title}》`" :aria-label="track.liked ? `取消收藏《${track.title}》` : `收藏《${track.title}》`" :aria-pressed="track.liked" @click="emit('favorite', track)"><Heart :size="17" :fill="track.liked ? 'currentColor' : 'none'" /></button>
          <button type="button" :title="`添加《${track.title}》到歌单`" :aria-label="`添加《${track.title}》到歌单`" @click="emit('add', track)"><ListPlus :size="17" /></button>
        </div>
      </article>
      <EmptyState v-if="!tracks.length" class="random-track-empty" title="暂无可播放歌曲" description="稍后刷新页面再试。" compact />
    </div>
  </section>
</template>
