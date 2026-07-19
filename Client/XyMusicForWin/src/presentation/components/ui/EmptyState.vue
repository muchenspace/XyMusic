<script setup lang="ts">
withDefaults(defineProps<{ title?: string; description?: string; compact?: boolean }>(), {
  title: "暂无内容",
  description: "",
  compact: false,
});
</script>

<template>
  <div class="empty-state" :class="{ 'empty-state--compact': compact }" role="status">
    <span v-if="$slots.icon" class="empty-state__icon"><slot name="icon" /></span>
    <strong>{{ title }}</strong>
    <p v-if="description">{{ description }}</p>
    <div v-if="$slots.actions" class="empty-state__actions"><slot name="actions" /></div>
  </div>
</template>
