<script setup lang="ts">
import { AlertCircle, CheckCircle2, Info, TriangleAlert, X } from "lucide-vue-next";
import { useUiStore } from "@/stores/ui";

const ui = useUiStore();
const icons = { success: CheckCircle2, error: AlertCircle, info: Info, warning: TriangleAlert };
</script>

<template>
  <div class="pointer-events-none fixed inset-x-3 top-3 z-[100] flex flex-col items-end gap-2 sm:left-auto sm:right-5 sm:top-5 sm:w-[390px]" aria-label="通知">
    <TransitionGroup name="toast">
      <article
        v-for="toast in ui.toasts"
        :key="toast.id"
        class="pointer-events-auto flex w-full gap-3 rounded-md border border-[var(--border-strong)] bg-[var(--surface-solid)] p-4 shadow-lg"
        :role="toast.kind === 'error' ? 'alert' : 'status'"
        :aria-live="toast.kind === 'error' ? 'assertive' : 'polite'"
        aria-atomic="true"
      >
        <component :is="icons[toast.kind]" :size="19" class="mt-0.5 shrink-0" :class="{
          'text-emerald-500': toast.kind === 'success', 'text-rose-500': toast.kind === 'error',
          'text-violet-500': toast.kind === 'info', 'text-amber-500': toast.kind === 'warning',
        }" />
        <div class="min-w-0 flex-1">
          <p class="font-semibold text-[var(--text)]">{{ toast.title }}</p>
          <p v-if="toast.detail" class="mt-0.5 text-sm leading-5 text-[var(--muted)]">{{ toast.detail }}</p>
        </div>
        <button type="button" class="-mr-1 -mt-1 grid h-8 w-8 place-items-center rounded-lg text-[var(--muted)] hover:bg-[var(--surface-muted)]" aria-label="关闭通知" @click="ui.dismiss(toast.id)"><X :size="15" /></button>
      </article>
    </TransitionGroup>
  </div>
</template>

<style scoped>
.toast-enter-active,
.toast-leave-active { transition: opacity var(--motion-base) var(--motion-ease-out), transform var(--motion-base) var(--motion-ease-out); }
.toast-leave-active { position: absolute; }
.toast-move { transition: transform var(--motion-base) var(--motion-ease-out); }
.toast-enter-from,
.toast-leave-to { opacity: 0; transform: translate3d(0, -8px, 0) scale(.985); }
</style>
