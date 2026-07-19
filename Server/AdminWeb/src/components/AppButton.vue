<script setup lang="ts">
import { LoaderCircle } from "lucide-vue-next";

withDefaults(defineProps<{
  variant?: "primary" | "secondary" | "ghost" | "danger";
  type?: "button" | "submit" | "reset";
  loading?: boolean;
  disabled?: boolean;
  iconOnly?: boolean;
}>(), {
  variant: "secondary",
  type: "button",
  loading: false,
  disabled: false,
  iconOnly: false,
});
</script>

<template>
  <button
    :type="type"
    class="btn"
    :class="[`btn-${variant}`, { 'btn-icon': iconOnly }]"
    :disabled="disabled || loading"
    :aria-busy="loading"
  >
    <LoaderCircle v-if="loading" :size="16" class="animate-spin" aria-hidden="true" />
    <slot v-else name="icon" />
    <span v-if="!iconOnly"><slot /></span>
    <span v-else class="sr-only"><slot /></span>
  </button>
</template>
