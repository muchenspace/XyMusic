<script setup lang="ts">
import { RouterView } from "vue-router";
import { onBeforeUnmount } from "vue";
import ToastHost from "@/components/ToastHost.vue";
import { ADMIN_QUERY_ERROR_EVENT } from "@/app/query-client";
import { useUiStore } from "@/stores/ui";

const ui = useUiStore();
let lastQueryError = "";
let lastQueryErrorAt = 0;
function showQueryError(event: Event): void {
  const message = event instanceof CustomEvent && typeof event.detail === "string" ? event.detail.trim() : "";
  if (!message) return;
  const now = Date.now();
  if (message === lastQueryError && now - lastQueryErrorAt < 5_000) return;
  lastQueryError = message;
  lastQueryErrorAt = now;
  ui.notify("error", "数据加载失败", message);
}
window.addEventListener(ADMIN_QUERY_ERROR_EVENT, showQueryError);
onBeforeUnmount(() => window.removeEventListener(ADMIN_QUERY_ERROR_EVENT, showQueryError));
</script>

<template>
  <RouterView />
  <ToastHost />
</template>
