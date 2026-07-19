<script setup lang="ts">
import type { Track } from "../../domain/music";
import type { FavoriteSort } from "../../domain/pagination";
import TrackTable from "../components/TrackTable.vue";
import PaginationFooter from "../components/ui/PaginationFooter.vue";

defineProps<{ title: string; description: string; tracks: Track[]; currentId?: string; isPlaying: boolean; sort?: FavoriteSort; error?: string; hasMore: boolean; loadingMore: boolean; pageKey?: string | null }>();
defineEmits<{ play: [track: Track]; toggle: []; favorite: [track: Track]; add: [track: Track]; changeSort: [value: string]; loadMore: [] }>();
</script>

<template>
  <section class="page-intro"><p class="eyebrow">音乐库</p><h1>{{ title }}</h1><p>{{ description }}</p></section>
  <div v-if="sort" class="view-toolbar"><label>排序<select :value="sort" @change="$emit('changeSort', ($event.target as HTMLSelectElement).value)">
    <option value="FAVORITED_DESC">最近收藏</option><option value="TITLE_ASC">标题升序</option>
  </select></label></div>
  <TrackTable :tracks="tracks" :current-id="currentId" :is-playing="isPlaying" @play="$emit('play', $event)" @toggle="$emit('toggle')" @favorite="$emit('favorite', $event)" @add="$emit('add', $event)" />
  <PaginationFooter :has-more="hasMore" :loading="loadingMore" :error="hasMore ? error : ''" :page-key="pageKey" @load-more="$emit('loadMore')" />
</template>
