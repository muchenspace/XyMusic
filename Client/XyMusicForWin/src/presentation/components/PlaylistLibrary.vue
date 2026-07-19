<script setup lang="ts">
import { LoaderCircle, Pencil, Play, Plus, Trash2 } from "@lucide/vue";
import type { Playlist } from "../../domain/music";
import ArtworkImage from "./ui/ArtworkImage.vue";
import EmptyState from "./ui/EmptyState.vue";

defineProps<{ playlists: Playlist[]; playLoadingId?: string }>();
defineEmits<{ open: [playlist: Playlist]; play: [playlist: Playlist]; edit: [playlist: Playlist]; delete: [playlist: Playlist]; create: [] }>();

const visibilityLabels = { PRIVATE: "私密", UNLISTED: "不公开列出", PUBLIC: "公开" } as const;
</script>

<template>
  <section class="content-section" aria-labelledby="playlist-library-heading">
    <div class="section-heading">
      <div><h2 id="playlist-library-heading">我的歌单</h2><p>管理歌单与曲目顺序</p></div>
      <button type="button" class="primary-button" @click="$emit('create')"><Plus :size="16" aria-hidden="true" />新建歌单</button>
    </div>
    <div v-if="playlists.length" class="playlist-library-grid">
      <article v-for="playlist in playlists" :key="playlist.id" class="playlist-library-card">
        <button type="button" class="playlist-art" :aria-label="`打开歌单《${playlist.title}》`" @click="$emit('open', playlist)">
          <ArtworkImage :src="playlist.coverUrl" :alt="`${playlist.title}歌单封面`" />
        </button>
        <div class="playlist-library-copy">
          <button type="button" class="playlist-title" @click="$emit('open', playlist)">{{ playlist.title }}</button>
          <p>{{ playlist.trackCount }} 首 · {{ visibilityLabels[playlist.visibility] }}</p>
        </div>
        <div class="playlist-card-actions">
          <button type="button" :title="`播放《${playlist.title}》`" :aria-label="`播放《${playlist.title}》`" :aria-busy="playLoadingId === playlist.id" :disabled="!playlist.trackCount || playLoadingId === playlist.id" @click="$emit('play', playlist)"><LoaderCircle v-if="playLoadingId === playlist.id" class="spin" :size="16" /><Play v-else :size="16" /></button>
          <button type="button" :title="`编辑《${playlist.title}》`" :aria-label="`编辑《${playlist.title}》`" @click="$emit('edit', playlist)"><Pencil :size="16" /></button>
          <button type="button" class="danger-action" :title="`删除《${playlist.title}》`" :aria-label="`删除《${playlist.title}》`" @click="$emit('delete', playlist)"><Trash2 :size="16" /></button>
        </div>
      </article>
    </div>
    <EmptyState v-else title="还没有歌单" description="创建歌单，把喜欢的音乐整理在一起。">
      <template #actions><button type="button" class="primary-button" @click="$emit('create')"><Plus :size="16" />新建歌单</button></template>
    </EmptyState>
  </section>
</template>
