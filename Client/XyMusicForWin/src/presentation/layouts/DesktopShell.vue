<script setup lang="ts">
import { ref } from "vue";

defineProps<{ hasPlayer: boolean; contentInert?: boolean }>();
const mainView = ref<HTMLElement | null>(null);

defineExpose({
  scrollElement: () => mainView.value,
});
</script>

<template>
  <div class="app-shell" :class="{ 'has-player': hasPlayer }">
    <slot name="sidebar" />
    <main ref="mainView" class="main-view" :inert="contentInert || undefined">
      <slot />
    </main>
    <slot name="player" />
    <slot name="overlays" />
  </div>
</template>
