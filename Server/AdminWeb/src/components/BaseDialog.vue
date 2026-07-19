<script setup lang="ts">
import { X } from "lucide-vue-next";
import {
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogOverlay,
  DialogPortal,
  DialogRoot,
  DialogTitle,
} from "reka-ui";

const open = defineModel<boolean>({ required: true });
const props = withDefaults(defineProps<{
  title: string;
  description?: string;
  side?: "center" | "right";
  width?: "md" | "lg" | "xl" | "2xl";
  confirmClose?: string;
  preventClose?: boolean;
}>(), { side: "center", width: "md" });

function changeOpen(value: boolean): void {
  if (!value && props.preventClose) return;
  if (!value && props.confirmClose && !window.confirm(props.confirmClose)) return;
  open.value = value;
}
</script>

<template>
  <DialogRoot :open="open" @update:open="changeOpen">
    <DialogPortal>
      <DialogOverlay class="ui-dialog-overlay fixed inset-0 z-50 bg-slate-950/55" />
      <DialogContent
        class="ui-dialog fixed z-50 flex min-w-0 max-h-[calc(100vh-2rem)] flex-col overflow-hidden border border-[var(--border-strong)] bg-[var(--surface-solid)] outline-none"
        :data-side="side"
        :class="[
          side === 'right'
            ? 'inset-y-2 right-2 h-[calc(100vh-1rem)] w-[calc(100vw-1rem)] rounded-md sm:w-[min(680px,calc(100vw-1rem))]'
            : 'w-[calc(100vw-2rem)] rounded-md',
          side === 'center' && width === 'md' && 'max-w-lg',
          side === 'center' && width === 'lg' && 'max-w-2xl',
          side === 'center' && width === 'xl' && 'max-w-4xl',
          side === 'center' && width === '2xl' && 'max-w-6xl',
        ]"
      >
        <header class="flex items-start justify-between gap-5 border-b border-[var(--border)] px-5 py-4 sm:px-6">
          <div class="min-w-0">
          <DialogTitle class="break-words text-base font-bold text-[var(--text)]">{{ title }}</DialogTitle>
            <DialogDescription v-if="description" class="mt-1 break-words text-sm leading-5 text-[var(--muted)]">
              {{ description }}
            </DialogDescription>
          </div>
          <DialogClose class="btn btn-ghost btn-icon -mr-2 -mt-1" aria-label="关闭" :disabled="preventClose">
            <X :size="18" />
          </DialogClose>
        </header>
        <div class="min-h-0 min-w-0 flex-1 overflow-x-hidden overflow-y-auto px-5 py-5 sm:px-6">
          <slot />
        </div>
        <footer v-if="$slots.footer" class="flex flex-wrap justify-end gap-2 border-t border-[var(--border)] px-5 py-4 sm:px-6">
          <slot name="footer" />
        </footer>
      </DialogContent>
    </DialogPortal>
  </DialogRoot>
</template>

<style scoped>
.ui-dialog-overlay[data-state="open"] { animation: overlay-in var(--motion-base) var(--motion-ease-out) both; }
.ui-dialog-overlay[data-state="closed"] { animation: overlay-out var(--motion-fast) var(--motion-ease-in) both; }
.ui-dialog[data-side="center"] { inset: 1rem; height: fit-content; margin: auto; }
.ui-dialog[data-side="center"][data-state="open"] { animation: dialog-center-in var(--motion-base) var(--motion-ease-out) both; }
.ui-dialog[data-side="center"][data-state="closed"] { animation: dialog-center-out var(--motion-fast) var(--motion-ease-in) both; }
.ui-dialog[data-side="right"][data-state="open"] { animation: dialog-side-in var(--motion-base) var(--motion-ease-out) both; }
.ui-dialog[data-side="right"][data-state="closed"] { animation: dialog-side-out var(--motion-fast) var(--motion-ease-in) both; }

@keyframes overlay-in { from { opacity: 0; } }
@keyframes overlay-out { to { opacity: 0; } }
@keyframes dialog-center-in {
  from { opacity: 0; transform: translate3d(0, 8px, 0) scale(.985); }
  to { opacity: 1; transform: none; }
}
@keyframes dialog-center-out {
  from { opacity: 1; transform: none; }
  to { opacity: 0; transform: translate3d(0, 4px, 0) scale(.99); }
}
@keyframes dialog-side-in {
  from { opacity: 0; transform: translate3d(18px, 0, 0); }
  to { opacity: 1; transform: none; }
}
@keyframes dialog-side-out {
  from { opacity: 1; transform: none; }
  to { opacity: 0; transform: translate3d(12px, 0, 0); }
}
</style>
