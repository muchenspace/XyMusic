<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
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

interface TrackEntryPair {
  track: Track;
  entry?: PlaylistEntry;
}

const rowGroup = ref<HTMLElement | null>(null);
const selectedEntryIds = ref<string[]>([]);
const draggedEntryId = ref<string | null>(null);
const dragOverEntryId = ref<string | null>(null);
const dragPreviewOrder = ref<string[] | null>(null);
const dragSettling = ref(false);
let dragPointerId: number | null = null;
let dragHandle: HTMLElement | null = null;
let dragRowElement: HTMLElement | null = null;
let dragOffsetValue = 0;
let dragLastClientY = 0;
let dragRowHeight = 64;
let dragSettleFrame: number | undefined;
let dragSettleTimer: number | undefined;
const trackCount = computed(() => props.tracks.length);
const playlistMode = computed(() => Boolean(props.entries));
const allSelected = computed(() => Boolean(props.entries?.length) && selectedEntryIds.value.length === props.entries?.length);
const selectedEntrySet = computed(() => new Set(selectedEntryIds.value));
const orderedTrackEntries = computed<TrackEntryPair[]>(() => {
  const pairs = props.tracks.map((track, index) => ({ track, entry: props.entries?.[index] }));
  const previewOrder = dragPreviewOrder.value;
  if (!props.entries || !previewOrder) return pairs;
  const pairsByEntryId = new Map(
    pairs.flatMap((pair) => pair.entry ? [[pair.entry.id, pair] as const] : []),
  );
  const ordered = previewOrder.flatMap((entryId) => {
    const pair = pairsByEntryId.get(entryId);
    return pair ? [pair] : [];
  });
  return ordered.length === pairs.length ? ordered : pairs;
});
const virtualRows = useVirtualRows(trackCount, rowGroup, { rowHeight: 64 });
const renderedTracks = computed<RenderedTrack[]>(() => {
  const start = virtualRows.start.value;
  return orderedTrackEntries.value
    .slice(start, virtualRows.end.value)
    .map(({ track, entry }, offset) => {
      const index = start + offset;
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
  if (dragPreviewOrder.value && sameOrder(ids, dragPreviewOrder.value)) dragPreviewOrder.value = null;
});
watch(() => props.busy, (busy, wasBusy) => {
  if (wasBusy && !busy) dragPreviewOrder.value = null;
});
onMounted(() => window.addEventListener("blur", cancelActivePointerDrag));
onBeforeUnmount(() => {
  window.removeEventListener("blur", cancelActivePointerDrag);
  removeWindowPointerListeners();
  clearDragSettleWork();
  clearDragDom();
  document.documentElement.classList.remove("track-reordering");
});

function entryAt(index: number): PlaylistEntry | undefined {
  return orderedTrackEntries.value[index]?.entry;
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

function beginPointerDrag(event: PointerEvent, entryId: string): void {
  if (props.busy || props.reorderDisabled || !props.entries) return;
  if (event.pointerType === "mouse" && event.button !== 0) return;
  clearDragSettleWork();
  clearDragDom();
  draggedEntryId.value = entryId;
  dragOverEntryId.value = null;
  dragPreviewOrder.value = props.entries.map((entry) => entry.id);
  dragSettling.value = false;
  dragPointerId = event.pointerId;
  dragLastClientY = event.clientY;
  const handle = event.currentTarget;
  if (handle instanceof HTMLElement) {
    dragHandle = handle;
    dragRowElement = handle.closest<HTMLElement>(".track-row");
    dragRowHeight = Math.max(1, dragRowElement?.offsetHeight || 64);
    dragOffsetValue = 0;
    applyDragOffset();
    handle.setPointerCapture?.(event.pointerId);
  }
  addWindowPointerListeners();
  document.documentElement.classList.add("track-reordering");
}

function movePointerDrag(event: PointerEvent): void {
  const sourceEntryId = draggedEntryId.value;
  const previewOrder = dragPreviewOrder.value;
  if (!sourceEntryId || !previewOrder || event.pointerId !== dragPointerId || dragSettling.value) return;
  const deltaY = event.clientY - dragLastClientY;
  if (!Number.isFinite(deltaY) || deltaY === 0) return;
  dragLastClientY = event.clientY;
  let offset = dragOffsetValue + deltaY;
  const threshold = dragRowHeight / 2;
  const nextOrder = [...previewOrder];
  let sourceIndex = nextOrder.indexOf(sourceEntryId);
  let changed = false;
  while (sourceIndex >= 0 && offset >= threshold && sourceIndex < nextOrder.length - 1) {
    const targetIndex = sourceIndex + 1;
    dragOverEntryId.value = nextOrder[targetIndex] ?? null;
    [nextOrder[sourceIndex], nextOrder[targetIndex]] = [nextOrder[targetIndex]!, nextOrder[sourceIndex]!];
    sourceIndex = targetIndex;
    offset -= dragRowHeight;
    changed = true;
  }
  while (sourceIndex > 0 && offset <= -threshold) {
    const targetIndex = sourceIndex - 1;
    dragOverEntryId.value = nextOrder[targetIndex] ?? null;
    [nextOrder[sourceIndex], nextOrder[targetIndex]] = [nextOrder[targetIndex]!, nextOrder[sourceIndex]!];
    sourceIndex = targetIndex;
    offset += dragRowHeight;
    changed = true;
  }
  if (sourceIndex === 0 && offset < -threshold) offset = -threshold;
  if (sourceIndex === nextOrder.length - 1 && offset > threshold) offset = threshold;
  dragOffsetValue = offset;
  applyDragOffset();
  if (changed) dragPreviewOrder.value = nextOrder;
}

function endPointerDrag(event: PointerEvent): void {
  if (event.pointerId !== dragPointerId) return;
  releasePointerCapture(event.pointerId);
  settlePointerDrag(true);
}

function cancelPointerDrag(event: PointerEvent): void {
  if (event.pointerId !== dragPointerId) return;
  releasePointerCapture(event.pointerId);
  settlePointerDrag(false);
}

function handleWindowPointerMove(event: PointerEvent): void {
  if (event.pointerId !== dragPointerId) return;
  event.preventDefault();
  movePointerDrag(event);
}

function handleWindowPointerUp(event: PointerEvent): void {
  if (event.pointerId !== dragPointerId) return;
  event.preventDefault();
  endPointerDrag(event);
}

function handleWindowPointerCancel(event: PointerEvent): void {
  if (event.pointerId !== dragPointerId) return;
  event.preventDefault();
  cancelPointerDrag(event);
}

function handleLostPointerCapture(event: PointerEvent): void {
  if (event.pointerId !== dragPointerId) return;
  dragHandle = null;
}

function cancelActivePointerDrag(): void {
  if (dragPointerId === null) return;
  dragPointerId = null;
  releasePointerCaptureWork();
  document.documentElement.classList.remove("track-reordering");
  settlePointerDrag(false);
}

function releasePointerCapture(pointerId: number): void {
  if (dragHandle?.hasPointerCapture?.(pointerId)) dragHandle.releasePointerCapture(pointerId);
  dragHandle = null;
  dragPointerId = null;
  removeWindowPointerListeners();
  document.documentElement.classList.remove("track-reordering");
}

function releasePointerCaptureWork(): void {
  dragHandle = null;
  removeWindowPointerListeners();
}

function addWindowPointerListeners(): void {
  removeWindowPointerListeners();
  window.addEventListener("pointermove", handleWindowPointerMove, { passive: false });
  window.addEventListener("pointerup", handleWindowPointerUp, { passive: false });
  window.addEventListener("pointercancel", handleWindowPointerCancel, { passive: false });
}

function removeWindowPointerListeners(): void {
  window.removeEventListener("pointermove", handleWindowPointerMove);
  window.removeEventListener("pointerup", handleWindowPointerUp);
  window.removeEventListener("pointercancel", handleWindowPointerCancel);
}

function settlePointerDrag(commit: boolean): void {
  const sourceEntryId = draggedEntryId.value;
  const previewOrder = dragPreviewOrder.value;
  const originalOrder = props.entries?.map((entry) => entry.id) ?? [];
  if (!sourceEntryId || !previewOrder) return;
  const changed = !sameOrder(previewOrder, originalOrder);
  if (!commit) {
    const previewIndex = previewOrder.indexOf(sourceEntryId);
    const originalIndex = originalOrder.indexOf(sourceEntryId);
    if (previewIndex >= 0 && originalIndex >= 0) {
      dragOffsetValue += (previewIndex - originalIndex) * dragRowHeight;
      applyDragOffset();
    }
    dragPreviewOrder.value = null;
  } else if (changed) {
    emit("reorder", [...previewOrder]);
  } else {
    dragPreviewOrder.value = null;
  }
  dragOverEntryId.value = null;
  dragSettling.value = true;
  dragSettleFrame = window.requestAnimationFrame(() => {
    dragSettleFrame = undefined;
    dragOffsetValue = 0;
    applyDragOffset();
  });
  dragSettleTimer = window.setTimeout(() => {
    dragSettleTimer = undefined;
    if (draggedEntryId.value === sourceEntryId) draggedEntryId.value = null;
    dragSettling.value = false;
    clearDragDom();
  }, 170);
}

function applyDragOffset(): void {
  dragRowElement?.style.setProperty("--track-drag-offset-y", `${dragOffsetValue}px`);
}

function clearDragDom(): void {
  dragRowElement?.style.removeProperty("--track-drag-offset-y");
  dragRowElement = null;
  dragOffsetValue = 0;
}

function clearDragSettleWork(): void {
  if (dragSettleFrame !== undefined) window.cancelAnimationFrame(dragSettleFrame);
  if (dragSettleTimer !== undefined) window.clearTimeout(dragSettleTimer);
  dragSettleFrame = undefined;
  dragSettleTimer = undefined;
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
    dragSettling.value && draggedEntryId.value === row.entry?.id,
    dragOverEntryId.value === row.entry?.id,
  ];
}

function rowKey(row: RenderedTrack): string {
  return row.entry?.id ?? row.track.id;
}

function sameOrder(left: string[], right: string[]): boolean {
  return left.length === right.length && left.every((entryId, index) => entryId === right[index]);
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
        <TransitionGroup name="track-reorder">
        <div
          v-for="row in renderedTracks"
          :key="rowKey(row)"
          v-memo="rowMemo(row)"
          class="track-row"
          :class="{ current: row.current, dragging: draggedEntryId === row.entry?.id, settling: dragSettling && draggedEntryId === row.entry?.id, 'drag-over': dragOverEntryId === row.entry?.id }"
          role="row"
          :aria-rowindex="row.index + 2"
          :aria-current="row.current ? 'true' : undefined"
          :aria-grabbed="draggedEntryId === row.entry?.id || undefined"
          :data-entry-id="row.entry?.id"
          tabindex="0"
          @click="handleRowClick($event, row.track, row.index)"
          @keydown.enter.self="toggleTrack(row.track, row.index)"
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
              <button type="button" class="drag-handle" :disabled="busy || reorderDisabled" :title="reorderDisabled ? '加载完整歌单后可排序' : `拖动《${row.track.title}》排序`" @click.stop.prevent @pointerdown.stop.prevent="beginPointerDrag($event, row.entry.id)" @pointermove.stop.prevent="movePointerDrag" @pointerup.stop.prevent="endPointerDrag" @pointercancel.stop.prevent="cancelPointerDrag" @lostpointercapture.stop="handleLostPointerCapture"><GripVertical :size="17" /></button>
            </template>
            <template v-else>
              <button type="button" :class="{ liked: row.track.liked }" :title="row.track.liked ? `取消收藏《${row.track.title}》` : `收藏《${row.track.title}》`" :aria-pressed="row.track.liked" @click.stop="emit('favorite', row.track)"><Heart :size="17" :fill="row.track.liked ? 'currentColor' : 'none'" /></button>
              <button type="button" :title="`添加《${row.track.title}》到歌单`" @click.stop="emit('add', row.track)"><ListPlus :size="17" /></button>
            </template>
          </span>
        </div>
        </TransitionGroup>
        <div v-if="virtualRows.bottomSpacer.value" class="track-virtual-spacer" :style="{ height: `${virtualRows.bottomSpacer.value}px` }" aria-hidden="true"></div>
      </div>

      <EmptyState v-if="!tracks.length" :title="emptyTitle" :description="emptyDescription" compact />
    </div>
  </section>
</template>
