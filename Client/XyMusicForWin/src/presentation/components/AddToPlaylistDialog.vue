<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { ListMusic } from "@lucide/vue";
import type { Playlist, Track } from "../../domain/music";
import AppDialog from "./ui/AppDialog.vue";
import EmptyState from "./ui/EmptyState.vue";

const props = defineProps<{ track: Track | null; playlists: Playlist[]; busy: boolean; loading: boolean; retryAvailable: boolean; hasMore: boolean; error: string }>();
defineEmits<{ close: []; select: [playlist: Playlist]; loadMore: []; retry: [] }>();
const query = ref("");
const filteredPlaylists = computed(() => {
  const normalized = query.value.trim().toLocaleLowerCase();
  return normalized ? props.playlists.filter((playlist) => playlist.title.toLocaleLowerCase().includes(normalized)) : props.playlists;
});
watch(() => props.track?.id, () => { query.value = ""; });
</script>

<template>
  <AppDialog
    :open="Boolean(track)"
    title="添加到歌单"
    :description="track ? `${track.title} · ${track.artist}` : ''"
    :dismissible="!busy"
    @close="$emit('close')"
  >
    <div v-if="track">
      <input v-model="query" class="dialog-filter" type="search" maxlength="100" placeholder="筛选歌单" aria-label="筛选歌单" />
      <div class="dialog-list">
        <button v-for="playlist in filteredPlaylists" :key="playlist.id" type="button" :disabled="busy" @click="$emit('select', playlist)">
          <span><ListMusic :size="17" aria-hidden="true" /><strong>{{ playlist.title }}</strong></span>
          <small>{{ playlist.trackCount }} 首</small>
        </button>
        <EmptyState v-if="!loading && !filteredPlaylists.length" :title="query.trim() ? '没有匹配的歌单' : '还没有歌单'" :description="query.trim() ? '尝试更换筛选关键词。' : '请先创建歌单，再添加歌曲。'" compact />
      </div>
      <div v-if="loading || hasMore" class="dialog-pagination">
        <button v-if="hasMore" type="button" class="secondary-button" :disabled="busy || loading" @click="$emit('loadMore')">{{ loading ? "正在加载…" : "加载更多歌单" }}</button>
        <span v-else role="status">正在加载歌单…</span>
      </div>
    </div>
    <div v-if="error" class="dialog-error dialog-error--action"><p role="alert">{{ error }}</p><button v-if="retryAvailable" type="button" class="bare-button" :disabled="busy || loading" @click="$emit('retry')">重试</button></div>
    <template #actions><button type="button" class="secondary-button" :disabled="busy" @click="$emit('close')">关闭</button></template>
  </AppDialog>
</template>
