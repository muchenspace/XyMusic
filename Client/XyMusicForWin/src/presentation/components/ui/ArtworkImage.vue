<script setup lang="ts">
import { computed, ref, watch } from "vue";
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

const failed = ref(false);
const loaded = ref(false);
const icon = computed(() => {
  if (props.kind === "artist") return Mic2;
  if (props.kind === "brand") return Music2;
  if (props.kind === "track") return Disc3;
  return ImageOff;
});

watch(() => props.src, () => {
  failed.value = false;
  loaded.value = false;
});

function fail(): void {
  loaded.value = false;
  failed.value = true;
}
</script>

<template>
  <span class="artwork-image" :class="`artwork-image--${kind}`">
    <img
      v-if="src && !failed"
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
