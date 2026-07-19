<script setup lang="ts">
import { ChevronDown, Heart, LoaderCircle, Minus, Pause, Play, SkipBack, SkipForward, X } from "@lucide/vue";
import type { Track } from "../../domain/music";
import { useApplicationServices } from "../services";
import { usePlayerStore } from "../stores/playerStore";
import ArtworkImage from "./ui/ArtworkImage.vue";

const player = usePlayerStore();
withDefaults(defineProps<{ fullscreen?: boolean }>(), { fullscreen: false });
defineEmits<{ favorite: [track: Track] }>();
const desktopWindow = useApplicationServices().desktopWindow;

function minimize(): void { void desktopWindow?.minimize().catch(() => undefined); }
function close(): void { void desktopWindow?.close().catch(() => undefined); }
function updateProgress(event: Event): void { player.seek(Number((event.target as HTMLInputElement).value)); }
</script>

<template>
  <section v-if="player.currentTrack" class="mini-player" aria-label="迷你播放器">
    <header class="mini-titlebar">
      <span data-tauri-drag-region>XY Music</span>
      <div v-if="!fullscreen" class="mini-window-controls">
        <button type="button" title="退出迷你模式" aria-label="退出迷你模式" @click="player.setMiniMode(false)"><ChevronDown :size="16" /></button>
        <button type="button" title="最小化" aria-label="最小化" @click="minimize"><Minus :size="16" /></button>
        <button type="button" class="close" title="关闭" aria-label="关闭" @click="close"><X :size="17" /></button>
      </div>
    </header>
    <div class="mini-player-content">
      <ArtworkImage :src="player.currentTrack.coverUrl" :alt="`${player.currentTrack.title}封面`" kind="track" loading="eager" />
      <div class="mini-track-copy">
        <strong>{{ player.currentTrack.title }}</strong>
        <span>{{ player.currentTrack.artist }}</span>
        <input :value="player.progress" type="range" min="0" max="100" step="0.1" aria-label="播放进度" :style="{ '--range-progress': `${player.progress}%` }" @input="updateProgress" />
      </div>
      <div class="mini-transport">
        <button type="button" :class="{ liked: player.currentTrack.liked }" :title="player.currentTrack.liked ? '取消收藏当前歌曲' : '收藏当前歌曲'" :aria-label="player.currentTrack.liked ? '取消收藏当前歌曲' : '收藏当前歌曲'" :aria-pressed="player.currentTrack.liked" @click="$emit('favorite', player.currentTrack)"><Heart :size="17" :fill="player.currentTrack.liked ? 'currentColor' : 'none'" /></button>
        <button type="button" title="上一首" aria-label="上一首" :disabled="player.loading" @click="player.previous"><SkipBack :size="18" fill="currentColor" /></button>
        <button type="button" class="mini-play-button" :title="player.isPlaying ? '暂停' : '播放'" :aria-label="player.isPlaying ? '暂停' : '播放'" :disabled="player.loading" @click="player.toggle">
          <LoaderCircle v-if="player.loading" class="spin" :size="18" />
          <Pause v-else-if="player.isPlaying" :size="19" fill="currentColor" />
          <Play v-else :size="19" fill="currentColor" />
        </button>
        <button type="button" title="下一首" aria-label="下一首" :disabled="player.loading" @click="player.next"><SkipForward :size="18" fill="currentColor" /></button>
      </div>
    </div>
  </section>
</template>
