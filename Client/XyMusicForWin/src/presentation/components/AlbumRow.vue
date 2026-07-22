<script setup lang="ts">
import { LoaderCircle, Play } from "@lucide/vue";
import type { Album } from "../../domain/music";
import ArtworkImage from "./ui/ArtworkImage.vue";

withDefaults(defineProps<{ albums: Album[]; title?: string; description?: string; playLoadingId?: string }>(), {
  title: "最近专辑",
  description: "音乐库中最近收录的专辑",
});
defineEmits<{ play: [album: Album]; open: [album: Album] }>();
</script>

<template>
  <section v-if="albums.length" class="content-section" :aria-label="title">
    <div class="section-heading">
      <div><h2>{{ title }}</h2><p v-if="description">{{ description }}</p></div>
    </div>
    <div class="album-grid">
      <article v-for="album in albums" :key="album.id" class="album-card">
        <button type="button" class="album-card-main" :aria-label="`打开专辑《${album.title}》`" @click="$emit('open', album)">
          <span class="album-cover-wrap">
            <ArtworkImage :src="album.coverUrl" :alt="`${album.title}专辑封面`" />
          </span>
          <span class="album-card-copy"><strong>{{ album.title }}</strong><small>{{ album.artist }}</small></span>
        </button>
        <button type="button" class="cover-play" :title="`播放专辑《${album.title}》`" :aria-label="`播放专辑《${album.title}》`" :aria-busy="playLoadingId === album.id" :disabled="playLoadingId === album.id" @click="$emit('play', album)">
          <LoaderCircle v-if="playLoadingId === album.id" class="spin" :size="18" aria-hidden="true" />
          <Play v-else :size="18" fill="currentColor" aria-hidden="true" />
        </button>
      </article>
    </div>
  </section>
</template>
