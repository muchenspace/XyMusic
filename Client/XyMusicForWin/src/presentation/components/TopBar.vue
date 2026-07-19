<script setup lang="ts">
import { ChevronLeft, ChevronRight, Copy, LoaderCircle, Minus, Moon, Search, Square, Sun, X } from "@lucide/vue";
import { onMounted, onUnmounted, ref } from "vue";
import { useApplicationServices } from "../services";
import { useThemeStore } from "../stores/themeStore";

const MAXIMIZED_STATE_SYNC_DELAY_MS = 120;
const props = withDefaults(defineProps<{
  modelValue: string;
  searching: boolean;
  searchEnabled?: boolean;
  canGoBack?: boolean;
  canGoForward?: boolean;
  fullscreen?: boolean;
}>(), {
  searchEnabled: true,
  canGoBack: false,
  canGoForward: false,
  fullscreen: false,
});
const emit = defineEmits<{ "update:modelValue": [value: string]; back: []; forward: [] }>();
const theme = useThemeStore();
const desktopWindow = useApplicationServices().desktopWindow;
const searchInput = ref<HTMLInputElement | null>(null);
const maximized = ref(false);
let removeResizeListener: (() => void) | undefined;
let componentMounted = false;
let resizeSyncTimer: number | undefined;

function focusSearch(event: KeyboardEvent) {
  if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "k") {
    event.preventDefault();
    searchInput.value?.focus();
    searchInput.value?.select();
  }
}

function minimizeWindow() { void desktopWindow.minimize().catch(() => undefined); }
async function syncWindowState() {
  try {
    const nextMaximized = await desktopWindow.isMaximized();
    if (componentMounted) maximized.value = nextMaximized;
  } catch { /* Browser and test environments do not expose a native window. */ }
}
function scheduleWindowStateSync() {
  if (resizeSyncTimer !== undefined) window.clearTimeout(resizeSyncTimer);
  resizeSyncTimer = window.setTimeout(() => {
    resizeSyncTimer = undefined;
    void syncWindowState();
  }, MAXIMIZED_STATE_SYNC_DELAY_MS);
}
async function toggleMaximizeWindow() {
  if (props.fullscreen) return;
  try {
    await desktopWindow.toggleMaximize();
    await syncWindowState();
  } catch { /* Browser and test environments do not expose a native window. */ }
}
function closeWindow() { void desktopWindow.close().catch(() => undefined); }

onMounted(() => {
  componentMounted = true;
  window.addEventListener("keydown", focusSearch);
  void syncWindowState();
  void desktopWindow.onResized(scheduleWindowStateSync)
    .then((unlisten) => {
      if (componentMounted) removeResizeListener = unlisten;
      else unlisten();
    })
    .catch(() => undefined);
});
onUnmounted(() => {
  componentMounted = false;
  if (resizeSyncTimer !== undefined) window.clearTimeout(resizeSyncTimer);
  resizeSyncTimer = undefined;
  removeResizeListener?.();
  window.removeEventListener("keydown", focusSearch);
});
</script>

<template>
  <header class="topbar" :class="{ 'without-search': !searchEnabled }">
    <nav v-if="searchEnabled" class="history-actions" aria-label="浏览历史">
      <button type="button" class="icon-button" title="返回" aria-label="返回" :disabled="!props.canGoBack" @click="emit('back')"><ChevronLeft :size="19" /></button>
      <button type="button" class="icon-button" title="前进" aria-label="前进" :disabled="!props.canGoForward" @click="emit('forward')"><ChevronRight :size="19" /></button>
    </nav>
    <label v-if="searchEnabled" class="search-field">
      <LoaderCircle v-if="searching" class="spin" :size="18" aria-hidden="true" />
      <Search v-else :size="18" aria-hidden="true" />
      <span class="visually-hidden">搜索音乐</span>
      <input
        ref="searchInput"
        :value="modelValue"
        type="search"
        maxlength="200"
        autocomplete="off"
        placeholder="搜索歌曲、专辑或歌手"
        aria-keyshortcuts="Control+K Meta+K"
        @input="emit('update:modelValue', ($event.target as HTMLInputElement).value)"
      />
    </label>

    <div class="topbar-actions">
      <button
        type="button"
        class="icon-button theme-toggle"
        :title="theme.theme === 'dark' ? '切换到浅色模式' : '切换到深色模式'"
        :aria-label="theme.theme === 'dark' ? '切换到浅色模式' : '切换到深色模式'"
        :aria-pressed="theme.theme === 'light'"
        @click="theme.toggle"
      >
        <Sun v-if="theme.theme === 'dark'" :size="18" aria-hidden="true" />
        <Moon v-else :size="18" aria-hidden="true" />
      </button>
    </div>

    <div class="titlebar-drag-region" data-tauri-drag-region @dblclick="toggleMaximizeWindow"></div>
    <div v-if="!props.fullscreen" class="window-controls" aria-label="窗口控制">
      <button type="button" class="window-control" title="最小化" aria-label="最小化" @click="minimizeWindow"><Minus :size="17" /></button>
      <button type="button" class="window-control" :title="maximized ? '还原' : '最大化'" :aria-label="maximized ? '还原' : '最大化'" @click="toggleMaximizeWindow"><Copy v-if="maximized" :size="13" /><Square v-else :size="13" /></button>
      <button type="button" class="window-control close" title="关闭" aria-label="关闭" @click="closeWindow"><X :size="18" /></button>
    </div>
  </header>
</template>
