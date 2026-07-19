<script setup lang="ts">
import { ArrowRight, Disc3, LoaderCircle, Play } from "@lucide/vue";
import type { Album } from "../../domain/music";

withDefaults(defineProps<{ album: Album; playLoading?: boolean }>(), { playLoading: false });
defineEmits<{ play: [album: Album]; open: [album: Album] }>();
</script>

<template>
  <section
    class="hero"
    :class="{ 'hero--without-artwork': !album.coverUrl }"
    :style="{ '--hero-image': album.coverUrl ? `url(${album.coverUrl})` : 'none' }"
    aria-labelledby="featured-album-title"
  >
    <Disc3 v-if="!album.coverUrl" class="hero-fallback-icon" aria-hidden="true" />
    <div class="hero-copy">
      <p class="eyebrow"><span aria-hidden="true"></span>最新专辑</p>
      <h1 id="featured-album-title">{{ album.title }}</h1>
      <p class="hero-meta">
        {{ album.artist }}<span aria-hidden="true">·</span>{{ album.year ?? "年份未知" }}<span aria-hidden="true">·</span>{{ album.trackCount }} 首歌曲
      </p>
      <p v-if="album.description" class="hero-description">{{ album.description }}</p>
      <div class="hero-actions">
        <button type="button" class="primary-button" :aria-busy="playLoading" :disabled="playLoading" @click="$emit('play', album)">
          <LoaderCircle v-if="playLoading" class="spin" :size="17" aria-hidden="true" />
          <Play v-else :size="17" fill="currentColor" aria-hidden="true" />{{ playLoading ? "正在准备…" : "播放专辑" }}
        </button>
        <button type="button" class="secondary-button" @click="$emit('open', album)">查看详情<ArrowRight :size="16" aria-hidden="true" /></button>
      </div>
    </div>
  </section>
</template>
