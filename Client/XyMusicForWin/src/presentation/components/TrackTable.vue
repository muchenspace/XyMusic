<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { ArrowDown, ArrowUp, Clock3, GripVertical, Heart, ListPlus, Trash2 } from "@lucide/vue";
import type { PlaylistEntry, Track } from "../../domain/music";
import { useVirtualRows } from "../composables/useVirtualRows";
import ArtworkImage from "./ui/ArtworkImage.vue";
import EmptyState from "./ui/EmptyState.vue";

const props = withDefaults(defineProps<{
  tracks: Track[];
  currentId?: string;
  isPlaying?: boolean;
  entries?: PlaylistEntry[];
  currentEntryId?: string;
  title?: string;
  description?: string;
  emptyTitle?: string;
  emptyDescription?: string;
  busy?: boolean;
  reorderDisabled?: boolean;
}>(), {
  title: "曲目",
  description: "点击曲目即可播放",
  emptyTitle: "暂无歌曲",
  emptyDescription: "这里还没有可显示的歌曲。",
  busy: false,
  reorderDisabled: false,
});

const emit = defineEmits<{
  play: [track: Track, index: number];
  toggle: [];
  favorite: [track: Track];
  add: [track: Track];
  remove: [entryId: string];
  removeSelected: [entryIds: string[]];
  move: [entryId: string, direction: -1 | 1];
  reorder: [orderedEntryIds: string[]];
}>();

interface RenderedTrack {
  track: Track;
  index: number;
  entry?: PlaylistEntry;
  current: boolean;
  selected: boolean;
}

const rowGroup = ref<HTMLElement | null>(null);
const selectedEntryIds = ref<string[]>([]);
const draggedEntryId = ref<string | null>(null);
const dragOverEntryId = ref<string | null>(null);
const trackCount = computed(() => props.tracks.length);
const playlistMode = computed(() => Boolean(props.entries));
const allSelected = computed(() => Boolean(props.entries?.length) && selectedEntryIds.value.length === props.entries?.length);
const selectedEntrySet = computed(() => new Set(selectedEntryIds.value));
const virtualRows = useVirtualRows(trackCount, rowGroup, { rowHeight: 64 });
const renderedTracks = computed<RenderedTrack[]>(() => {
  const start = virtualRows.start.value;
  return props.tracks
    .slice(start, virtualRows.end.value)
    .map((track, offset) => {
      const index = start + offset;
    const entry = props.entries?.[index];
      return {
        track,
        index,
        entry,
        current: entry ? Boolean(props.currentEntryId) && props.currentEntryId === entry.id : props.currentId === track.id,
        selected: entry ? selectedEntrySet.value.has(entry.id) : false,
      };
    });
});

watch(() => props.entries?.map((entry) => entry.id), (ids = []) => {
  const available = new Set(ids);
  selectedEntryIds.value = selectedEntryIds.value.filter((id) => available.has(id));
});

function entryAt(index: number): PlaylistEntry | undefined {
  return props.entries?.[index];
}

function isCurrent(track: Track, index: number): boolean {
  const entry = entryAt(index);
  return entry ? Boolean(props.currentEntryId) && props.currentEntryId === entry.id : props.currentId === track.id;
}

function toggleTrack(track: Track, index: number): void {
  if (isCurrent(track, index)) emit("toggle");
  else emit("play", track, index);
}

function handleRowClick(event: MouseEvent, track: Track, index: number): void {
  const target = event.target;
  if (target instanceof Element && target.closest("button, input, select, textarea, a, [contenteditable='true']")) return;
  toggleTrack(track, index);
}

function toggleEntry(entryId: string, checked: boolean): void {
  selectedEntryIds.value = checked
    ? [...new Set([...selectedEntryIds.value, entryId])]
    : selectedEntryIds.value.filter((id) => id !== entryId);
}

function toggleAll(checked: boolean): void {
  selectedEntryIds.value = checked ? props.entries?.map((entry) => entry.id) ?? [] : [];
}

function removeSelected(): void {
  if (!props.busy && selectedEntryIds.value.length) emit("removeSelected", [...selectedEntryIds.value]);
}

function startDrag(event: DragEvent, entryId: string): void {
  if (props.busy) return;
  draggedEntryId.value = entryId;
  event.dataTransfer?.setData("text/plain", entryId);
  if (event.dataTransfer) event.dataTransfer.effectAllowed = "move";
}

function dropOn(targetEntryId: string): void {
  const sourceEntryId = draggedEntryId.value;
  clearDrag();
  if (!sourceEntryId || sourceEntryId === targetEntryId || !props.entries) return;
  const orderedIds = props.entries.map((entry) => entry.id);
  const sourceIndex = orderedIds.indexOf(sourceEntryId);
  const originalTargetIndex = orderedIds.indexOf(targetEntryId);
  if (sourceIndex < 0 || originalTargetIndex < 0) return;
  orderedIds.splice(sourceIndex, 1);
  const targetIndex = orderedIds.indexOf(targetEntryId);
  if (targetIndex < 0) return;
  const insertionIndex = sourceIndex < originalTargetIndex ? targetIndex + 1 : targetIndex;
  orderedIds.splice(insertionIndex, 0, sourceEntryId);
  emit("reorder", orderedIds);
}

function clearDrag(): void {
  draggedEntryId.value = null;
  dragOverEntryId.value = null;
}

function rowMemo(row: RenderedTrack): unknown[] {
  return [
    row.track,
    row.entry,
    row.track.id,
    row.track.title,
    row.track.artist,
    row.track.album,
    row.track.coverUrl,
    row.track.duration,
    row.track.liked,
    row.track.publishedAt,
    row.entry?.id,
    row.index,
    row.current,
    row.selected,
    row.index === props.tracks.length - 1,
    props.busy,
    props.reorderDisabled,
    draggedEntryId.value === row.entry?.id,
    dragOverEntryId.value === row.entry?.id,
  ];
}

function rowKey(row: RenderedTrack): string {
  return row.entry?.id ?? row.track.id;
}

function formatTime(seconds: number): string {
  const safeSeconds = Number.isFinite(seconds) && seconds > 0 ? seconds : 0;
  return `${Math.floor(safeSeconds / 60)}:${String(Math.floor(safeSeconds % 60)).padStart(2, "0")}`;
}

function formatDate(value: string): string {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? "未知" : date.toLocaleDateString("zh-CN", { year: "numeric", month: "2-digit", day: "2-digit" });
}
</script>

<template>
  <section class="content-section tracks-section" :aria-label="title">
    <div class="section-heading"><div><h2>{{ title }}</h2><p>{{ description }}</p></div></div>
    <div v-if="playlistMode && entries?.length" class="playlist-selection-toolbar">
      <label><input type="checkbox" :checked="allSelected" :disabled="busy" @change="toggleAll(($event.target as HTMLInputElement).checked)" />全选</label>
      <span>已选择 {{ selectedEntryIds.length }} 首</span>
      <button class="danger-button" type="button" :disabled="busy || !selectedEntryIds.length" @click="removeSelected"><Trash2 :size="15" />移除所选</button>
      <small>拖动行末手柄可调整顺序</small>
    </div>
    <div class="track-table" role="table" :aria-label="title" :aria-rowcount="tracks.length + 1" :aria-busy="busy">
      <div class="track-row track-header" role="row">
        <span role="columnheader">#</span>
        <span role="columnheader">歌曲</span>
        <span role="columnheader">专辑</span>
        <span role="columnheader">发行日期</span>
        <span role="columnheader" aria-label="时长"><Clock3 :size="15" aria-hidden="true" /></span>
        <span role="columnheader" aria-label="操作"></span>
      </div>

      <div ref="rowGroup" class="track-row-group" role="rowgroup" :data-virtualized="virtualRows.enabled.value || undefined">
        <div v-if="virtualRows.topSpacer.value" class="track-virtual-spacer" :style="{ height: `${virtualRows.topSpacer.value}px` }" aria-hidden="true"></div>
        <div
          v-for="row in renderedTracks"
          :key="rowKey(row)"
          v-memo="rowMemo(row)"
          class="track-row"
          :class="{ current: row.current, dragging: draggedEntryId === row.entry?.id, 'drag-over': dragOverEntryId === row.entry?.id }"
          role="row"
          :aria-rowindex="row.index + 2"
          :aria-current="row.current ? 'true' : undefined"
          tabindex="0"
          @click="handleRowClick($event, row.track, row.index)"
          @keydown.enter.self="toggleTrack(row.track, row.index)"
          @dragover.prevent="dragOverEntryId = row.entry?.id ?? null"
          @drop.prevent="row.entry && dropOn(row.entry.id)"
        >
          <span class="track-index" role="cell">
            <input v-if="row.entry" type="checkbox" :checked="row.selected" :disabled="busy" :aria-label="`选择《${row.track.title}》`" @click.stop @change="toggleEntry(row.entry.id, ($event.target as HTMLInputElement).checked)" />
            <span v-else aria-hidden="true">{{ String(row.index + 1).padStart(2, "0") }}</span>
          </span>
          <span class="track-title" role="cell">
            <ArtworkImage :src="row.track.coverUrl" :alt="`${row.track.title}封面`" kind="track" />
            <span><strong class="track-main-title">{{ row.track.title }}</strong><small>{{ row.track.artist }}</small></span>
          </span>
          <span class="track-album" role="cell" :title="row.track.album">{{ row.track.album || "未知专辑" }}</span>
          <time role="cell" :datetime="row.track.publishedAt">{{ formatDate(row.track.publishedAt) }}</time>
          <span role="cell">{{ formatTime(row.track.duration) }}</span>
          <span class="track-actions" role="cell">
            <template v-if="row.entry">
              <button type="button" :disabled="busy || reorderDisabled || row.index === 0" :title="reorderDisabled ? '加载完整歌单后可排序' : `上移《${row.track.title}》`" @click.stop="emit('move', row.entry.id, -1)"><ArrowUp :size="16" /></button>
              <button type="button" :disabled="busy || reorderDisabled || row.index === tracks.length - 1" :title="reorderDisabled ? '加载完整歌单后可排序' : `下移《${row.track.title}》`" @click.stop="emit('move', row.entry.id, 1)"><ArrowDown :size="16" /></button>
              <button type="button" class="danger-action" :disabled="busy" :title="`从歌单移除《${row.track.title}》`" @click.stop="emit('remove', row.entry.id)"><Trash2 :size="16" /></button>
              <button type="button" class="drag-handle" :disabled="busy || reorderDisabled" :draggable="!busy && !reorderDisabled" :title="reorderDisabled ? '加载完整歌单后可排序' : `拖动《${row.track.title}》排序`" @click.stop @dragstart.stop="startDrag($event, row.entry.id)" @dragend="clearDrag"><GripVertical :size="17" /></button>
            </template>
            <template v-else>
              <button type="button" :class="{ liked: row.track.liked }" :title="row.track.liked ? `取消收藏《${row.track.title}》` : `收藏《${row.track.title}》`" :aria-pressed="row.track.liked" @click.stop="emit('favorite', row.track)"><Heart :size="17" :fill="row.track.liked ? 'currentColor' : 'none'" /></button>
              <button type="button" :title="`添加《${row.track.title}》到歌单`" @click.stop="emit('add', row.track)"><ListPlus :size="17" /></button>
            </template>
          </span>
        </div>
        <div v-if="virtualRows.bottomSpacer.value" class="track-virtual-spacer" :style="{ height: `${virtualRows.bottomSpacer.value}px` }" aria-hidden="true"></div>
      </div>

      <EmptyState v-if="!tracks.length" :title="emptyTitle" :description="emptyDescription" compact />
    </div>
  </section>
</template>
