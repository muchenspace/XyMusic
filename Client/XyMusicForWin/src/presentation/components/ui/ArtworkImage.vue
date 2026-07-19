<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { Disc3, ImageOff, Mic2, Music2 } from "@lucide/vue";

const props = withDefaults(defineProps<{
  src?: string | null;
  alt?: string;
  kind?: "album" | "artist" | "track" | "brand";
  loading?: "eager" | "lazy";
}>(), {
  src: "",
  alt: "",
  kind: "album",
  loading: "lazy",
});

const emit = defineEmits<{ "load-failed": [src: string] }>();

const failed = ref(false);
const loaded = ref(false);
// 通过 key 变化强制 <img> 重新挂载，绕过浏览器缓存重试加载。
const reloadKey = ref(0);
const retryCount = ref(0);
let retryTimer: number | null = null;

const icon = computed(() => {
  if (props.kind === "artist") return Mic2;
  if (props.kind === "brand") return Music2;
  if (props.kind === "track") return Disc3;
  return ImageOff;
});

watch(() => props.src, () => {
  failed.value = false;
  loaded.value = false;
  retryCount.value = 0;
  if (retryTimer !== null) {
    window.clearTimeout(retryTimer);
    retryTimer = null;
  }
});

onBeforeUnmount(() => {
  if (retryTimer !== null) window.clearTimeout(retryTimer);
});

function fail(): void {
  // 签名 URL 过期或瞬时网络失败时进行有限重试，绕过浏览器缓存重新请求。
  if (retryCount.value < MAX_RETRY) {
    retryCount.value += 1;
    retryTimer = window.setTimeout(() => {
      retryTimer = null;
      loaded.value = false;
      failed.value = false;
      reloadKey.value += 1;
    }, RETRY_DELAY_MS);
    return;
  }
  loaded.value = false;
  failed.value = true;
  if (props.src) emit("load-failed", props.src);
}

const MAX_RETRY = 2;
const RETRY_DELAY_MS = 800;
</script>

<template>
  <span class="artwork-image" :class="`artwork-image--${kind}`">
    <img
      v-if="src && !failed"
      :key="reloadKey"
      :src="src"
      :alt="alt"
      :loading="loading"
      decoding="async"
      :class="{ 'is-loaded': loaded }"
      @load="loaded = true"
      @error="fail"
    />
    <span v-else class="artwork-image__fallback" :aria-label="alt || '暂无封面'" role="img">
      <component :is="icon" aria-hidden="true" />
    </span>
  </span>
</template>
