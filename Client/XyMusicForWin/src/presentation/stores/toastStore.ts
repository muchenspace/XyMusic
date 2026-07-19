import { ref } from "vue";
import { defineStore } from "pinia";

export type ToastTone = "success" | "error" | "warning" | "info";
export interface ToastMessage { id: string; message: string; tone: ToastTone }

export const useToastStore = defineStore("toast", () => {
  const messages = ref<ToastMessage[]>([]);
  const timers = new Map<string, number>();
  let nextId = 1;

  function show(message: string, tone: ToastTone = "info", duration = 3200): string {
    const id = `toast-${nextId++}`;
    while (messages.value.length >= MAX_TOASTS) dismiss(messages.value[0]!.id);
    messages.value.push({ id, message, tone });
    if (duration > 0) timers.set(id, window.setTimeout(() => dismiss(id), duration));
    return id;
  }

  function dismiss(id: string): void {
    const timer = timers.get(id);
    if (timer !== undefined) window.clearTimeout(timer);
    timers.delete(id);
    messages.value = messages.value.filter((message) => message.id !== id);
  }

  function clear(): void {
    for (const timer of timers.values()) window.clearTimeout(timer);
    timers.clear();
    messages.value = [];
  }

  return { messages, show, dismiss, clear };
});

const MAX_TOASTS = 5;
