<script setup lang="ts">
import { nextTick, onBeforeUnmount, ref, watch } from "vue";
import { X } from "@lucide/vue";

let dialogSequence = 0;

const props = withDefaults(defineProps<{
  open: boolean;
  title: string;
  description?: string;
  closeLabel?: string;
  dismissible?: boolean;
}>(), {
  description: "",
  closeLabel: "关闭对话框",
  dismissible: true,
});

const emit = defineEmits<{ close: [] }>();
const dialogElement = ref<HTMLElement | null>(null);
const titleId = `app-dialog-title-${++dialogSequence}`;
const descriptionId = `app-dialog-description-${dialogSequence}`;
let previouslyFocused: HTMLElement | null = null;
let focusGuardActive = false;

const focusableSelector = [
  "button:not([disabled])",
  "[href]",
  "input:not([disabled])",
  "select:not([disabled])",
  "textarea:not([disabled])",
  "[tabindex]:not([tabindex='-1'])",
].join(",");

watch(() => props.open, async (open) => {
  if (open) {
    previouslyFocused = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    setFocusGuard(true);
    await nextTick();
    const initial = dialogElement.value?.querySelector<HTMLElement>("[autofocus]")
      ?? dialogElement.value?.querySelector<HTMLElement>(focusableSelector);
    (initial ?? dialogElement.value)?.focus();
    return;
  }
  setFocusGuard(false);
  previouslyFocused?.focus();
  previouslyFocused = null;
}, { immediate: true });

function onKeydown(event: KeyboardEvent) {
  if (event.key === "Escape") {
    if (!props.dismissible) return;
    event.preventDefault();
    emit("close");
    return;
  }
  if (event.key !== "Tab" || !dialogElement.value) return;
  const focusable = Array.from(dialogElement.value.querySelectorAll<HTMLElement>(focusableSelector));
  if (!focusable.length) {
    event.preventDefault();
    dialogElement.value.focus();
    return;
  }
  const first = focusable[0]!;
  const last = focusable[focusable.length - 1]!;
  if (event.shiftKey && document.activeElement === first) {
    event.preventDefault();
    last.focus();
  } else if (!event.shiftKey && document.activeElement === last) {
    event.preventDefault();
    first.focus();
  }
}

function keepFocusInside(event: Event): void {
  const dialog = dialogElement.value;
  if (!props.open || !dialog || dialog.contains(event.target as Node)) return;
  const fallback = dialog.querySelector<HTMLElement>("[autofocus]")
    ?? dialog.querySelector<HTMLElement>(focusableSelector)
    ?? dialog;
  fallback.focus();
}

function setFocusGuard(enabled: boolean): void {
  if (enabled === focusGuardActive) return;
  focusGuardActive = enabled;
  if (enabled) document.addEventListener("focusin", keepFocusInside);
  else document.removeEventListener("focusin", keepFocusInside);
}

onBeforeUnmount(() => {
  setFocusGuard(false);
  previouslyFocused?.focus();
});
</script>

<template>
  <Transition name="dialog-fade">
    <div v-if="open" class="dialog-backdrop" @pointerdown.self="dismissible && emit('close')">
      <section
        ref="dialogElement"
        class="app-dialog"
        role="dialog"
        aria-modal="true"
        :aria-labelledby="titleId"
        :aria-describedby="description ? descriptionId : undefined"
        tabindex="-1"
        @keydown="onKeydown"
      >
        <header class="app-dialog__header">
          <div>
            <h2 :id="titleId">{{ title }}</h2>
            <p v-if="description" :id="descriptionId">{{ description }}</p>
          </div>
          <button type="button" class="icon-button" :aria-label="closeLabel" :title="closeLabel" :disabled="!dismissible" @click="emit('close')">
            <X :size="18" aria-hidden="true" />
          </button>
        </header>
        <div class="app-dialog__body"><slot /></div>
        <footer v-if="$slots.actions" class="dialog-actions"><slot name="actions" /></footer>
      </section>
    </div>
  </Transition>
</template>
