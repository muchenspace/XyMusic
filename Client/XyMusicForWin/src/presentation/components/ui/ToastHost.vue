<script setup lang="ts">
import { AlertCircle, CheckCircle2, Info, TriangleAlert, X } from "@lucide/vue";

export interface ToastMessage {
  id: string;
  message: string;
  title?: string;
  tone?: "info" | "success" | "warning" | "error";
}

withDefaults(defineProps<{ toasts?: ToastMessage[] }>(), { toasts: () => [] });
defineEmits<{ dismiss: [id: string] }>();

const icons = { info: Info, success: CheckCircle2, warning: TriangleAlert, error: AlertCircle } as const;
</script>

<template>
  <div class="toast-host" aria-live="polite" aria-relevant="additions removals">
    <TransitionGroup name="toast">
      <article v-for="toast in toasts" :key="toast.id" class="toast" :class="`toast--${toast.tone ?? 'info'}`" :role="toast.tone === 'error' ? 'alert' : 'status'">
        <component :is="icons[toast.tone ?? 'info']" :size="18" aria-hidden="true" />
        <div><strong v-if="toast.title">{{ toast.title }}</strong><p>{{ toast.message }}</p></div>
        <button type="button" :aria-label="`关闭${toast.title || '通知'}`" @click="$emit('dismiss', toast.id)"><X :size="16" /></button>
      </article>
    </TransitionGroup>
  </div>
</template>
