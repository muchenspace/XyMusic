<script setup lang="ts">
defineProps<{ title: string; description?: string }>();
</script>

<template>
  <header class="flex flex-col gap-3 border-b border-[var(--border)] pb-4 sm:flex-row sm:items-center sm:justify-between">
    <div class="min-w-0">
      <div v-if="$slots.eyebrow" class="mb-1 text-[11px] font-bold uppercase tracking-[0.12em] text-[var(--primary)]">
        <slot name="eyebrow" />
      </div>
      <h1 class="text-xl font-bold text-[var(--text)] sm:text-2xl">{{ title }}</h1>
      <p v-if="description" class="mt-1 max-w-3xl text-sm leading-5 text-[var(--muted)]">{{ description }}</p>
    </div>
    <div v-if="$slots.actions" class="flex flex-wrap items-center gap-2"><slot name="actions" /></div>
  </header>
</template>
