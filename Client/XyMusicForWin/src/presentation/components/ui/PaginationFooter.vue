<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from "vue";

const props = defineProps<{ hasMore: boolean; loading: boolean; error?: string; pageKey?: string | null }>();
const emit = defineEmits<{ loadMore: [] }>();
const sentinel = ref<HTMLElement | null>(null);
let observer: IntersectionObserver | null = null;

function observe(): void {
  observer?.disconnect();
  observer = null;
  if (!sentinel.value || !props.hasMore) return;
  observer = new IntersectionObserver(([entry]) => {
    if (entry?.isIntersecting && props.hasMore && !props.loading) emit("loadMore");
  }, { rootMargin: "240px" });
  observer.observe(sentinel.value);
}

onMounted(observe);
watch(() => [props.hasMore, props.pageKey] as const, observe);
onBeforeUnmount(() => observer?.disconnect());
</script>

<template>
  <div ref="sentinel" class="pagination-footer">
    <button v-if="hasMore" type="button" class="secondary-button" :disabled="loading" @click="emit('loadMore')">{{ loading ? "正在加载…" : error ? "重试加载" : "加载更多" }}</button>
    <span v-else>已经到底了</span>
  </div>
</template>
