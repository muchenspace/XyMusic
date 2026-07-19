<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from "vue";
import { ListMusic, Pause, Play, Trash2, X } from "@lucide/vue";
import type { Track } from "../../domain/music";
import { usePlayerStore } from "../stores/playerStore";
import { useVirtualRows } from "../composables/useVirtualRows";
import ArtworkImage from "./ui/ArtworkImage.vue";
import EmptyState from "./ui/EmptyState.vue";

const QUEUE_ROW_HEIGHT = 62;
const player = usePlayerStore();
const panel = ref<HTMLElement | null>(null);
const queueList = ref<HTMLElement | null>(null);
const queueRows = ref<HTMLElement | null>(null);
const queueCount = computed(() => player.queue.length);
const virtualRows = useVirtualRows(queueCount, queueRows, { rowHeight: QUEUE_ROW_HEIGHT });
const stableTrackKeys = new WeakMap<Track, string>();
let nextTrackKey = 0;
const queueRowKeys = computed(() => {
  const occurrences = new Map<Track, number>();
  return player.queue.map((track) => {
    const occurrence = occurrences.get(track) ?? 0;
    occurrences.set(track, occurrence + 1);
    let key = stableTrackKeys.get(track);
    if (!key) {
      key = `queue-track-${nextTrackKey++}`;
      stableTrackKeys.set(track, key);
    }
    return `${key}-${occurrence}`;
  });
});
const renderedQueue = computed(() => player.queue
  .slice(virtualRows.start.value, virtualRows.end.value)
  .map((track, offset) => {
    const index = virtualRows.start.value + offset;
    return { track, index, key: queueRowKeys.value[index]! };
  }));
let previouslyFocused: HTMLElement | null = null;

watch(() => player.queueOpen, async (open) => {
  if (open) {
    previouslyFocused = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    await nextTick();
    revealCurrent(true);
    virtualRows.refresh();
    panel.value?.focus();
  } else {
    previouslyFocused?.focus();
    previouslyFocused = null;
  }
});

watch(() => player.currentIndex, async () => {
  if (!player.queueOpen) return;
  await nextTick();
  revealCurrent(false);
});

function revealCurrent(center: boolean): void {
  const list = queueList.value;
  const index = player.currentIndex;
  if (!list || index < 0) return;
  const rowTop = index * QUEUE_ROW_HEIGHT;
  const rowBottom = rowTop + QUEUE_ROW_HEIGHT;
  if (!center && rowTop >= list.scrollTop && rowBottom <= list.scrollTop + list.clientHeight) return;
  list.scrollTop = Math.max(0, center
    ? rowTop - Math.max(0, (list.clientHeight - QUEUE_ROW_HEIGHT) / 2)
    : rowTop < list.scrollTop ? rowTop : rowBottom - list.clientHeight);
}

function close() { player.queueOpen = false; }
function onKeydown(event: KeyboardEvent) { if (event.key === "Escape") close(); }
onBeforeUnmount(() => previouslyFocused?.focus());
</script>

<template>
  <Transition name="queue">
    <aside v-if="player.queueOpen" ref="panel" class="queue-panel" aria-label="播放队列" tabindex="-1" @keydown="onKeydown">
      <header>
        <div><p>播放队列</p><span>共 {{ player.queue.length }} 首歌曲</span></div>
        <div>
          <button type="button" class="icon-button" title="仅保留当前歌曲" aria-label="仅保留当前歌曲" :disabled="player.queue.length <= 1" @click="player.clearQueue"><Trash2 :size="17" /></button>
          <button type="button" class="icon-button" title="关闭队列" aria-label="关闭队列" @click="close"><X :size="19" /></button>
        </div>
      </header>
      <div ref="queueList" class="queue-list" role="list" aria-label="播放队列曲目">
        <div ref="queueRows" class="queue-row-group" :data-virtualized="virtualRows.enabled.value || undefined">
          <div v-if="virtualRows.topSpacer.value" class="queue-virtual-spacer" :style="{ height: `${virtualRows.topSpacer.value}px` }" aria-hidden="true"></div>
          <div
            v-for="{ track, index, key } in renderedQueue"
            :key="key"
            class="queue-item"
            :class="{ active: player.currentIndex === index }"
            role="listitem"
            :aria-posinset="index + 1"
            :aria-setsize="player.queue.length"
          >
            <button type="button" class="queue-item-main" :aria-label="`播放《${track.title}》`" @click="player.playAt(index)">
              <ArtworkImage :src="track.coverUrl" :alt="`${track.title}封面`" kind="track" />
              <span><strong>{{ track.title }}</strong><small>{{ track.artist }}</small></span>
            </button>
            <Pause v-if="player.currentIndex === index && player.isPlaying" class="queue-playing" :size="15" fill="currentColor" aria-label="正在播放" />
            <Play v-else-if="player.currentIndex === index" class="queue-playing" :size="15" fill="currentColor" aria-label="当前歌曲" />
            <button v-else type="button" class="queue-remove" :title="`从队列移除《${track.title}》`" :aria-label="`从队列移除《${track.title}》`" @click="player.removeFromQueueAt(index)"><X :size="16" /></button>
          </div>
          <div v-if="virtualRows.bottomSpacer.value" class="queue-virtual-spacer" :style="{ height: `${virtualRows.bottomSpacer.value}px` }" aria-hidden="true"></div>
        </div>
        <EmptyState v-if="!player.queue.length" title="队列为空" description="播放歌曲后会显示在这里。" compact>
          <template #icon><ListMusic :size="24" /></template>
        </EmptyState>
      </div>
    </aside>
  </Transition>
</template>
