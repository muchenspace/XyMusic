<script setup lang="ts">
withDefaults(defineProps<{ count?: number; label?: string; compact?: boolean }>(), {
  count: 3,
  label: "正在加载",
  compact: false,
});
</script>

<template>
  <div class="loading-skeleton" :class="{ 'loading-skeleton--compact': compact }" role="status" :aria-label="label">
    <span v-for="item in count" :key="item" aria-hidden="true"></span>
  </div>
</template>
