<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from "vue";
import { Clipboard, RefreshCw, Trash2 } from "@lucide/vue";
import type { ServerConfig } from "../../application/ports/SessionRepository";
import { useApplicationServices } from "../services";

const props = defineProps<{ serverConfig: ServerConfig }>();
const diagnostics = useApplicationServices().diagnostics;
const initialEntries = diagnostics?.entries() ?? [];
let entriesSignature = signatureOf(initialEntries);
const entries = ref(initialEntries.reverse());
const copyState = ref("");
let refreshTimer = 0;
let copyTimer = 0;

const runtime = computed(() => typeof window !== "undefined" && "__TAURI_INTERNALS__" in window ? "Tauri 桌面端" : "浏览器预览");
const serverAddress = computed(() => `${props.serverConfig.protocol}://${props.serverConfig.host}:${props.serverConfig.port}`);

function refresh(): void {
  const nextEntries = diagnostics?.entries() ?? [];
  const nextSignature = signatureOf(nextEntries);
  if (nextSignature === entriesSignature) return;
  entriesSignature = nextSignature;
  entries.value = nextEntries.reverse();
}
function clear(): void { diagnostics?.clear(); refresh(); }

function signatureOf(value: readonly { id: string }[]): string {
  return `${value.length}:${value[0]?.id ?? ""}:${value[value.length - 1]?.id ?? ""}`;
}

async function copy(): Promise<void> {
  const lines = [
    `Runtime: ${runtime.value}`,
    `Server: ${serverAddress.value}`,
    `User agent: ${navigator.userAgent}`,
    "",
    ...entries.value.slice().reverse().map((entry) => `${entry.timestamp} [${entry.level.toUpperCase()}] [${entry.category}] ${entry.message}`),
  ];
  try {
    await navigator.clipboard.writeText(lines.join("\n"));
    copyState.value = "已复制";
  } catch {
    copyState.value = "复制失败";
  }
  window.clearTimeout(copyTimer);
  copyTimer = window.setTimeout(() => { copyState.value = ""; }, 1800);
}

onMounted(() => {
  refresh();
  refreshTimer = window.setInterval(refresh, 1500);
});
onUnmounted(() => {
  window.clearInterval(refreshTimer);
  window.clearTimeout(copyTimer);
});
</script>

<template>
  <section class="page-intro diagnostics-intro">
    <p class="eyebrow">支持与排错</p><h1>诊断</h1>
    <p>查看当前运行环境和本次启动期间的关键事件。日志不会记录密码或访问令牌。</p>
  </section>
  <section class="diagnostics-summary" aria-label="运行信息">
    <div><span>运行环境</span><strong>{{ runtime }}</strong></div>
    <div><span>服务器</span><strong>{{ serverAddress }}</strong></div>
    <div><span>日志数量</span><strong>{{ entries.length }}</strong></div>
  </section>
  <section class="content-section diagnostics-section">
    <div class="section-heading diagnostics-heading">
      <div><h2>运行日志</h2><p>系统日志同时写入 Tauri 应用日志目录。</p></div>
      <div class="diagnostics-actions">
        <button class="secondary-button" type="button" @click="refresh"><RefreshCw :size="15" />刷新</button>
        <button class="secondary-button" type="button" @click="copy"><Clipboard :size="15" />{{ copyState || "复制" }}</button>
        <button class="danger-button" type="button" :disabled="!entries.length" @click="clear"><Trash2 :size="15" />清空</button>
      </div>
    </div>
    <div class="diagnostics-log" role="log" aria-live="polite">
      <article v-for="entry in entries" :key="entry.id" class="diagnostic-entry" :class="`level-${entry.level}`">
        <time :datetime="entry.timestamp">{{ new Date(entry.timestamp).toLocaleString("zh-CN") }}</time>
        <strong>{{ entry.level.toUpperCase() }}</strong><span>{{ entry.category }}</span><p>{{ entry.message }}</p>
      </article>
      <p v-if="!entries.length" class="diagnostics-empty">本次启动暂时没有诊断事件。</p>
    </div>
  </section>
</template>
