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
  isPlaying: boolean;
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

const rowGroup = ref<HTMLElement | null>(null);
const selectedEntryIds = ref<string[]>([]);
const draggedEntryId = ref<string | null>(null);
const dragOverEntryId = ref<string | null>(null);
const trackCount = computed(() => props.tracks.length);
const playlistMode = computed(() => Boolean(props.entries));
const allSelected = computed(() => Boolean(props.entries?.length) && selectedEntryIds.value.length === props.entries?.length);
const virtualRows = useVirtualRows(trackCount, rowGroup, { rowHeight: 64 });
const renderedTracks = computed(() => props.tracks
  .slice(virtualRows.start.value, virtualRows.end.value)
  .map((track, offset) => ({ track, index: virtualRows.start.value + offset })));

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
          v-for="{ track, index } in renderedTracks"
          :key="entryAt(index)?.id ?? track.id"
          class="track-row"
          :class="{ current: isCurrent(track, index), dragging: draggedEntryId === entryAt(index)?.id, 'drag-over': dragOverEntryId === entryAt(index)?.id }"
          role="row"
          :aria-rowindex="index + 2"
          :aria-current="isCurrent(track, index) ? 'true' : undefined"
          tabindex="0"
          @click="handleRowClick($event, track, index)"
          @keydown.enter.self="toggleTrack(track, index)"
          @dragover.prevent="dragOverEntryId = entryAt(index)?.id ?? null"
          @drop.prevent="entryAt(index) && dropOn(entryAt(index)!.id)"
        >
          <span class="track-index" role="cell">
            <input v-if="entryAt(index)" type="checkbox" :checked="selectedEntryIds.includes(entryAt(index)!.id)" :disabled="busy" :aria-label="`选择《${track.title}》`" @click.stop @change="toggleEntry(entryAt(index)!.id, ($event.target as HTMLInputElement).checked)" />
            <span v-else aria-hidden="true">{{ String(index + 1).padStart(2, "0") }}</span>
          </span>
          <span class="track-title" role="cell">
            <ArtworkImage :src="track.coverUrl" :alt="`${track.title}封面`" kind="track" />
            <span><strong class="track-main-title">{{ track.title }}</strong><small>{{ track.artist }}</small></span>
          </span>
          <span class="track-album" role="cell" :title="track.album">{{ track.album || "未知专辑" }}</span>
          <time role="cell" :datetime="track.publishedAt">{{ formatDate(track.publishedAt) }}</time>
          <span role="cell">{{ formatTime(track.duration) }}</span>
          <span class="track-actions" role="cell">
            <template v-if="entryAt(index)">
              <button type="button" :disabled="busy || reorderDisabled || index === 0" :title="reorderDisabled ? '加载完整歌单后可排序' : `上移《${track.title}》`" @click.stop="emit('move', entryAt(index)!.id, -1)"><ArrowUp :size="16" /></button>
              <button type="button" :disabled="busy || reorderDisabled || index === tracks.length - 1" :title="reorderDisabled ? '加载完整歌单后可排序' : `下移《${track.title}》`" @click.stop="emit('move', entryAt(index)!.id, 1)"><ArrowDown :size="16" /></button>
              <button type="button" class="danger-action" :disabled="busy" :title="`从歌单移除《${track.title}》`" @click.stop="emit('remove', entryAt(index)!.id)"><Trash2 :size="16" /></button>
              <button type="button" class="drag-handle" :disabled="busy || reorderDisabled" :draggable="!busy && !reorderDisabled" :title="reorderDisabled ? '加载完整歌单后可排序' : `拖动《${track.title}》排序`" @click.stop @dragstart.stop="startDrag($event, entryAt(index)!.id)" @dragend="clearDrag"><GripVertical :size="17" /></button>
            </template>
            <template v-else>
              <button type="button" :class="{ liked: track.liked }" :title="track.liked ? `取消收藏《${track.title}》` : `收藏《${track.title}》`" :aria-pressed="track.liked" @click.stop="emit('favorite', track)"><Heart :size="17" :fill="track.liked ? 'currentColor' : 'none'" /></button>
              <button type="button" :title="`添加《${track.title}》到歌单`" @click.stop="emit('add', track)"><ListPlus :size="17" /></button>
            </template>
          </span>
        </div>
        <div v-if="virtualRows.bottomSpacer.value" class="track-virtual-spacer" :style="{ height: `${virtualRows.bottomSpacer.value}px` }" aria-hidden="true"></div>
      </div>

      <EmptyState v-if="!tracks.length" :title="emptyTitle" :description="emptyDescription" compact />
    </div>
  </section>
</template>
