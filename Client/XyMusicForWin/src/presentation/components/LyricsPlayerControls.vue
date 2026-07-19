<script setup lang="ts">
import { Heart, ListMusic, Pause, Play, SkipBack, SkipForward, Volume1, Volume2, VolumeX } from "@lucide/vue";
import type { Track } from "../../domain/music";
import { usePlayerStore } from "../stores/playerStore";
import ArtworkImage from "./ui/ArtworkImage.vue";

const emit = defineEmits<{ favorite: [track: Track] }>();
const player = usePlayerStore();
let volumeBeforeMute = player.volume > 0 ? player.volume : 72;

function updateProgress(event: Event): void {
  player.seek(Number((event.target as HTMLInputElement).value));
}

function updateVolume(event: Event): void {
  player.volume = Number((event.target as HTMLInputElement).value);
}

function toggleMute(): void {
  if (player.volume > 0) {
    volumeBeforeMute = player.volume;
    player.volume = 0;
    return;
  }
  player.volume = Math.max(1, volumeBeforeMute);
}

function toggleFavorite(): void {
  if (player.currentTrack) emit("favorite", player.currentTrack);
}

const formatTime = (seconds: number): string => {
  const value = Number.isFinite(seconds) && seconds > 0 ? seconds : 0;
  return `${Math.floor(value / 60)}:${String(Math.floor(value % 60)).padStart(2, "0")}`;
};
</script>

<template>
  <footer v-if="player.currentTrack" class="lyrics-player-controls" aria-label="歌词页播放器">
    <button class="lyrics-now-playing" type="button" title="返回首页" @click="player.toggleLyrics">
      <ArtworkImage :src="player.currentTrack.coverUrl" :alt="`${player.currentTrack.title}封面`" kind="track" />
      <span><strong>{{ player.currentTrack.title }}</strong><small>{{ player.currentTrack.artist }}</small></span>
    </button>
    <div class="lyrics-transport">
      <div class="lyrics-transport-buttons">
        <button type="button" title="上一首" aria-label="上一首" :disabled="player.loading" @click="player.previous"><SkipBack :size="19" fill="currentColor" /></button>
        <button type="button" class="lyrics-play-button" :title="player.isPlaying ? '暂停' : '播放'" :aria-label="player.isPlaying ? '暂停' : '播放'" :disabled="player.loading" @click="player.toggle"><Pause v-if="player.isPlaying" :size="21" fill="currentColor" /><Play v-else :size="21" fill="currentColor" /></button>
        <button type="button" title="下一首" aria-label="下一首" :disabled="player.loading" @click="player.next"><SkipForward :size="19" fill="currentColor" /></button>
      </div>
      <div class="lyrics-progress-row">
        <span>{{ formatTime(player.currentTime) }}</span>
        <input :value="player.progress" aria-label="播放进度" type="range" min="0" max="100" step="0.1" :aria-valuetext="`${formatTime(player.currentTime)} / ${formatTime(player.duration)}`" :style="{ '--range-progress': `${player.progress}%` }" @input="updateProgress" />
        <span>{{ formatTime(player.duration) }}</span>
      </div>
    </div>
    <div class="lyrics-player-extras">
      <button type="button" :class="{ liked: player.currentTrack.liked }" :title="player.currentTrack.liked ? '取消收藏当前歌曲' : '收藏当前歌曲'" :aria-label="player.currentTrack.liked ? '取消收藏当前歌曲' : '收藏当前歌曲'" :aria-pressed="player.currentTrack.liked" @click="toggleFavorite"><Heart :size="18" :fill="player.currentTrack.liked ? 'currentColor' : 'none'" /></button>
      <button type="button" title="播放队列" aria-label="播放队列" @click="player.toggleQueue"><ListMusic :size="19" /></button>
      <button type="button" :title="player.volume > 0 ? '静音' : '取消静音'" :aria-label="player.volume > 0 ? '静音' : '取消静音'" @click="toggleMute"><Volume2 v-if="player.volume > 45" :size="18" /><Volume1 v-else-if="player.volume > 0" :size="18" /><VolumeX v-else :size="18" /></button>
      <input :value="player.volume" aria-label="音量" type="range" min="0" max="100" :aria-valuetext="`${Math.round(player.volume)}%`" :style="{ '--range-progress': `${player.volume}%` }" @input="updateVolume" />
    </div>
  </footer>
</template>
