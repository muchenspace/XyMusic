<script setup lang="ts">
import { watch } from "vue";
import { Heart, ListMusic, LoaderCircle, Maximize2, Monitor, PanelTopOpen, Pause, Play, Repeat1, Repeat2, Shuffle, SkipBack, SkipForward, Volume1, Volume2, VolumeX } from "@lucide/vue";
import type { Track } from "../../domain/music";
import { usePlayerStore } from "../stores/playerStore";
import { useDesktopLyricsStore } from "../stores/desktopLyricsStore";
import { useApplicationServices } from "../services";
import ArtworkImage from "./ui/ArtworkImage.vue";

const player = usePlayerStore();
const desktopLyrics = useDesktopLyricsStore();
const desktopWindow = useApplicationServices().desktopWindow;
defineEmits<{ favorite: [track: Track] }>();
let volumeBeforeMute = player.volume > 0 ? player.volume : 72;
watch(() => player.volume, (value) => { if (value > 0) volumeBeforeMute = value; });
const formatTime = (seconds: number) => {
  const value = Number.isFinite(seconds) && seconds > 0 ? seconds : 0;
  return `${Math.floor(value / 60)}:${String(Math.floor(value % 60)).padStart(2, "0")}`;
};

function updateProgress(event: Event) { player.seek(Number((event.target as HTMLInputElement).value)); }
function updateVolume(event: Event) { player.volume = Number((event.target as HTMLInputElement).value); }
function toggleMute() {
  if (player.volume > 0) {
    volumeBeforeMute = player.volume;
    player.volume = 0;
  } else {
    player.volume = Math.max(1, volumeBeforeMute);
  }
}
function toggleFullscreen() {
  void desktopWindow.toggleFullscreen().catch(() => undefined);
}
function cycleRepeatMode() {
  player.repeatMode = player.repeatMode === "off" ? "all" : player.repeatMode === "all" ? "one" : "off";
}
</script>

<template>
  <footer v-if="player.currentTrack && !player.lyricsOpen" class="player-bar" aria-label="音乐播放器">
    <button
      class="now-playing"
      type="button"
      :title="player.lyricsOpen ? '返回首页' : '打开歌词页'"
      :aria-label="`${player.lyricsOpen ? '返回首页' : '打开歌词页'}：${player.currentTrack.title}`"
      @click="player.toggleLyrics"
    >
      <ArtworkImage :src="player.currentTrack.coverUrl" :alt="`${player.currentTrack.title}封面`" kind="track" loading="eager" />
      <span class="now-playing-copy" aria-live="polite">
        <strong>{{ player.currentTrack.title }}</strong>
        <span>{{ player.currentTrack.artist }}</span>
        <small v-if="player.error" class="player-error" role="alert">{{ player.error }}</small>
      </span>
    </button>

    <div class="transport">
      <div class="transport-buttons">
        <button type="button" class="bare-button" :class="{ enabled: player.shuffled }" title="随机播放" aria-label="随机播放" :aria-pressed="player.shuffled" @click="player.shuffled = !player.shuffled"><Shuffle :size="17" /></button>
        <button type="button" class="bare-button" title="上一首" aria-label="上一首" :disabled="player.loading" @click="player.previous"><SkipBack :size="19" fill="currentColor" /></button>
        <button type="button" class="play-button" :title="player.isPlaying ? '暂停' : '播放'" :aria-label="player.isPlaying ? '暂停' : '播放'" :disabled="player.loading" @click="player.toggle">
          <LoaderCircle v-if="player.loading" class="spin" :size="19" />
          <Pause v-else-if="player.isPlaying" :size="20" fill="currentColor" />
          <Play v-else :size="20" fill="currentColor" />
        </button>
        <button type="button" class="bare-button" title="下一首" aria-label="下一首" :disabled="player.loading" @click="player.next"><SkipForward :size="19" fill="currentColor" /></button>
        <button type="button" class="bare-button" :class="{ enabled: player.repeatMode !== 'off' }" :title="player.repeatMode === 'off' ? '顺序播放' : player.repeatMode === 'all' ? '列表循环' : '单曲循环'" :aria-label="player.repeatMode === 'off' ? '顺序播放，点击切换列表循环' : player.repeatMode === 'all' ? '列表循环，点击切换单曲循环' : '单曲循环，点击切换顺序播放'" :aria-pressed="player.repeatMode !== 'off'" @click="cycleRepeatMode"><Repeat1 v-if="player.repeatMode === 'one'" :size="17" /><Repeat2 v-else :size="17" /></button>
      </div>
      <div class="progress-row">
        <span>{{ formatTime(player.currentTime) }}</span>
        <input
          :value="player.progress"
          type="range"
          min="0"
          max="100"
          step="0.1"
          aria-label="播放进度"
          :aria-valuetext="`${formatTime(player.currentTime)} / ${formatTime(player.duration || player.currentTrack.duration)}`"
          :style="{ '--range-progress': `${player.progress}%` }"
          @input="updateProgress"
        />
        <span>{{ formatTime(player.duration || player.currentTrack.duration) }}</span>
      </div>
    </div>

    <div class="player-extras">
      <button type="button" class="bare-button" :class="{ liked: player.currentTrack.liked }" :title="player.currentTrack.liked ? '取消收藏当前歌曲' : '收藏当前歌曲'" :aria-label="player.currentTrack.liked ? '取消收藏当前歌曲' : '收藏当前歌曲'" :aria-pressed="player.currentTrack.liked" @click="$emit('favorite', player.currentTrack)"><Heart :size="18" :fill="player.currentTrack.liked ? 'currentColor' : 'none'" /></button>
      <button type="button" class="lyrics-button desktop-lyrics-button" :title="desktopLyrics.visible ? '关闭桌面歌词' : '打开桌面歌词'" :aria-label="desktopLyrics.visible ? '关闭桌面歌词' : '打开桌面歌词'" :class="{ enabled: desktopLyrics.visible }" :aria-pressed="desktopLyrics.visible" @click="desktopLyrics.toggleVisible"><Monitor :size="17" /></button>
      <button type="button" class="bare-button" title="播放队列" aria-label="播放队列" :class="{ enabled: player.queueOpen }" :aria-pressed="player.queueOpen" @click="player.toggleQueue"><ListMusic :size="19" /></button>
      <button type="button" class="bare-button volume-button" :title="player.volume > 0 ? '静音' : '取消静音'" :aria-label="player.volume > 0 ? '静音' : '取消静音'" @click="toggleMute">
        <Volume2 v-if="player.volume > 45" :size="18" />
        <Volume1 v-else-if="player.volume > 0" :size="18" />
        <VolumeX v-else :size="18" />
      </button>
      <input :value="player.volume" class="volume-slider" type="range" min="0" max="100" aria-label="音量" :aria-valuetext="`${Math.round(player.volume)}%`" :style="{ '--range-progress': `${player.volume}%` }" @input="updateVolume" />
      <button type="button" class="bare-button mini-mode-button" title="迷你播放器" aria-label="迷你播放器" @click="player.setMiniMode(true)"><PanelTopOpen :size="17" /></button>
      <button type="button" class="bare-button fullscreen-button" title="全屏" aria-label="全屏" @click="toggleFullscreen"><Maximize2 :size="17" /></button>
    </div>
  </footer>
</template>
