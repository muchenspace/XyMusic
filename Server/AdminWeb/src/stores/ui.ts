import { ref } from "vue";
import { defineStore } from "pinia";
import { randomUuid } from "@/utils/browser-crypto";

export type ToastKind = "success" | "error" | "info" | "warning";

export interface ToastMessage {
  id: string;
  kind: ToastKind;
  title: string;
  detail?: string;
}

export const useUiStore = defineStore("ui", () => {
  const sidebarOpen = ref(false);
  const routePending = ref(false);
  const toasts = ref<ToastMessage[]>([]);

  function notify(kind: ToastKind, title: string, detail?: string): void {
    const id = randomUuid();
    const message: ToastMessage = detail ? { id, kind, title, detail } : { id, kind, title };
    toasts.value.push(message);
    window.setTimeout(() => dismiss(id), kind === "error" ? 8000 : 4500);
  }

  function dismiss(id: string): void {
    toasts.value = toasts.value.filter((item) => item.id !== id);
  }

  return { sidebarOpen, routePending, toasts, notify, dismiss };
});
