<script setup lang="ts">
import { LoaderCircle, Play } from "@lucide/vue";
import type { Playlist } from "../../domain/music";
import ArtworkImage from "./ui/ArtworkImage.vue";

defineProps<{ playlists: Playlist[]; playLoadingId?: string }>();
defineEmits<{ play: [playlist: Playlist]; open: [playlist: Playlist] }>();
</script>

<template>
  <section v-if="playlists.length" class="content-section playlist-band" aria-labelledby="playlists-row-heading">
    <div class="section-heading"><div><h2 id="playlists-row-heading">我的歌单</h2><p>最近更新的歌单</p></div></div>
    <div class="mood-grid">
      <article v-for="playlist in playlists" :key="playlist.id" class="mood-card" :style="{ '--mood-accent': playlist.accent || 'var(--accent)' }">
        <button type="button" class="mood-open" :aria-label="`打开歌单《${playlist.title}》`" @click="$emit('open', playlist)">
          <ArtworkImage :src="playlist.coverUrl" :alt="`${playlist.title}歌单封面`" />
          <span class="mood-overlay" aria-hidden="true"></span>
          <span class="mood-copy"><small>{{ playlist.trackCount }} 首歌曲</small><strong>{{ playlist.title }}</strong><span>{{ playlist.description || "暂无描述" }}</span></span>
        </button>
        <button
          type="button"
          class="cover-play"
          :title="`播放歌单《${playlist.title}》`"
          :aria-label="`播放歌单《${playlist.title}》`"
          :aria-busy="playLoadingId === playlist.id"
          :disabled="!playlist.trackCount || playLoadingId === playlist.id"
          @click="$emit('play', playlist)"
        >
          <LoaderCircle v-if="playLoadingId === playlist.id" class="spin" :size="18" aria-hidden="true" />
          <Play v-else :size="18" fill="currentColor" aria-hidden="true" />
        </button>
      </article>
    </div>
  </section>
</template>
