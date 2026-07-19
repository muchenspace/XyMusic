<script setup lang="ts">
import type { Artist } from "../../domain/music";
import ArtworkImage from "./ui/ArtworkImage.vue";

defineProps<{ artists: Artist[] }>();
defineEmits<{ open: [artist: Artist] }>();
</script>

<template>
  <section class="content-section" aria-labelledby="artists-heading">
    <div class="section-heading"><div><h2 id="artists-heading">歌手</h2><p>匹配的歌手</p></div></div>
    <div class="artist-grid">
      <button v-for="artist in artists" :key="artist.id" type="button" class="artist-card" :aria-label="`查看歌手${artist.name}`" @click="$emit('open', artist)">
        <ArtworkImage :src="artist.artwork?.url" :alt="`${artist.name}的照片`" kind="artist" />
        <strong>{{ artist.name }}</strong>
        <small>{{ artist.description || "查看歌曲" }}</small>
      </button>
    </div>
  </section>
</template>
