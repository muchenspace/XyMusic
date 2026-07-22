<script setup lang="ts">
import type { Playlist } from "../../domain/music";
import type { PlaylistSort } from "../../domain/pagination";
import PlaylistLibrary from "../components/PlaylistLibrary.vue";
import PageHeader from "../components/ui/PageHeader.vue";
import PaginationFooter from "../components/ui/PaginationFooter.vue";

defineProps<{ playlists: Playlist[]; sort: PlaylistSort; playLoadingId?: string; error?: string; hasMore: boolean; loadingMore: boolean; pageKey?: string | null }>();
defineEmits<{ open: [playlist: Playlist]; play: [playlist: Playlist]; edit: [playlist: Playlist]; delete: [playlist: Playlist]; create: []; changeSort: [value: string]; loadMore: [] }>();
</script>

<template>
  <PageHeader title="我的歌单" description="整理、播放并管理自己的音乐集合。" eyebrow="个人收藏">
    <template #actions>
      <label class="page-sort-control">
        <span>排序</span>
        <select :value="sort" @change="$emit('changeSort', ($event.target as HTMLSelectElement).value)">
          <option value="UPDATED_DESC">最近更新</option>
          <option value="NAME_ASC">名称升序</option>
          <option value="NAME_DESC">名称降序</option>
        </select>
      </label>
    </template>
  </PageHeader>
  <PlaylistLibrary :playlists="playlists" :play-loading-id="playLoadingId" @open="$emit('open', $event)" @play="$emit('play', $event)" @edit="$emit('edit', $event)" @delete="$emit('delete', $event)" @create="$emit('create')" />
  <PaginationFooter :has-more="hasMore" :loading="loadingMore" :error="hasMore ? error : ''" :page-key="pageKey" @load-more="$emit('loadMore')" />
</template>
