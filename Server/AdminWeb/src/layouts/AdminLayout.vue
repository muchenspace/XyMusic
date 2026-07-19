<script setup lang="ts">
import {
  Activity,
  Album,
  Camera,
  ChevronDown,
  FolderSync,
  LayoutDashboard,
  ListMusic,
  ListTodo,
  LogOut,
  Menu,
  Moon,
  ScrollText,
  Search,
  Settings,
  Sun,
  Users,
  X,
} from "lucide-vue-next";
import { useColorMode } from "@vueuse/core";
import { useQuery } from "@tanstack/vue-query";
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import { loadRouteLocation, useRoute, useRouter } from "vue-router";
import { useAuthStore } from "@/stores/auth";
import { useUiStore } from "@/stores/ui";
import { serviceReadiness } from "@/api/client";
import ArtworkUploadField from "@/components/ArtworkUploadField.vue";
import BaseDialog from "@/components/BaseDialog.vue";
import xymusicIcon from "@/assets/xymusic.png";

const route = useRoute();
const router = useRouter();
const auth = useAuthStore();
const ui = useUiStore();
const colorMode = useColorMode({ storageKey: "xymusic-theme", attribute: "class" });
const catalogOpen = ref(true);
const quickSearch = ref("");
const avatarOpen = ref(false);
const readiness = useQuery({
  queryKey: ["service", "readiness"],
  queryFn: ({ signal }) => serviceReadiness(signal),
  refetchInterval: 30_000,
  retry: false,
});
const serviceReady = computed(() => readiness.isSuccess.value && readiness.data.value?.status === "ready");
const serviceLabel = computed(() => readiness.isPending.value ? "检查服务状态" : serviceReady.value ? "服务已连接" : "服务不可用");

const navigation = [
  { label: "仪表盘", icon: LayoutDashboard, to: "/dashboard" },
  { label: "用户管理", icon: Users, to: "/users" },
] as const;
const catalog = [
  { label: "曲目", icon: ListMusic, to: "/music/tracks" },
  { label: "专辑", icon: Album, to: "/music/albums" },
  { label: "艺术家", icon: Activity, to: "/music/artists" },
] as const;
const operations = [
  { label: "音源与扫描", icon: FolderSync, to: "/sources" },
  { label: "后台任务", icon: ListTodo, to: "/jobs" },
  { label: "审计日志", icon: ScrollText, to: "/audit" },
  { label: "系统设置", icon: Settings, to: "/settings" },
] as const;
const preloadedRoutes = new Set<string>();
let preloadTimer: number | undefined;

const initials = computed(() => (auth.profile?.displayName || auth.profile?.username || "A").slice(0, 2).toUpperCase());
async function avatarCompleted(): Promise<void> {
  await auth.ensureSession(true);
  ui.notify("success", "管理员头像已更新");
}
function active(path: string): boolean { return route.path === path || route.path.startsWith(`${path}/`); }
function preload(path: string): void {
  if (preloadedRoutes.has(path)) return;
  preloadedRoutes.add(path);
  void loadRouteLocation(router.resolve(path)).catch(() => { preloadedRoutes.delete(path); });
}
function scheduleRoutePreloads(): void {
  const paths = [...navigation, ...catalog, ...operations].map((item) => item.to);
  let index = 0;
  const preloadNext = (): void => {
    if (index >= paths.length) {
      preloadTimer = undefined;
      return;
    }
    if (document.visibilityState !== "visible") {
      preloadTimer = window.setTimeout(preloadNext, 500);
      return;
    }
    const path = paths[index++];
    if (path && !active(path)) preload(path);
    preloadTimer = window.setTimeout(preloadNext, 120);
  };
  preloadTimer = window.setTimeout(preloadNext, 700);
}
function navigate(path: string): void {
  ui.sidebarOpen = false;
  if (active(path)) return;
  preload(path);
  void router.push(path);
}
function search(): void {
  const value = quickSearch.value.trim();
  if (!value) return;
  void router.push({ path: "/music/tracks", query: { search: value } });
  quickSearch.value = "";
}
async function logout(): Promise<void> {
  try {
    await auth.logout();
  } catch (error) {
    ui.notify("warning", "服务端会话撤销失败", error instanceof Error ? error.message : undefined);
  } finally {
    await router.replace("/login");
  }
}
onMounted(scheduleRoutePreloads);
onBeforeUnmount(() => { if (preloadTimer) window.clearTimeout(preloadTimer); });
</script>

<template>
  <div class="min-h-screen bg-[var(--bg)]">
    <Transition name="nav-backdrop">
      <div v-if="ui.sidebarOpen" class="fixed inset-0 z-30 bg-slate-950/40 lg:hidden" @click="ui.sidebarOpen = false" />
    </Transition>
    <aside class="fixed inset-y-0 left-0 z-40 flex w-[248px] flex-col border-r border-[var(--border)] bg-[var(--surface-solid)] transition-transform duration-200 will-change-transform lg:translate-x-0 lg:will-change-auto"
      :class="ui.sidebarOpen ? 'translate-x-0' : '-translate-x-full'">
      <div class="flex h-14 items-center gap-3 border-b border-[var(--border)] px-4">
        <img :src="xymusicIcon" class="h-8 w-8 shrink-0 object-contain" alt="" width="32" height="32" aria-hidden="true" />
        <div>
          <p class="text-sm font-bold">XyMusic</p>
          <p class="text-[11px] text-[var(--muted)]">管理后台</p>
        </div>
        <button class="mobile-nav-control btn btn-ghost btn-icon ml-auto lg:hidden" type="button" aria-label="关闭导航" @click="ui.sidebarOpen = false"><X :size="18" /></button>
      </div>

      <nav class="min-h-0 flex-1 overflow-y-auto px-2 py-4" aria-label="主导航">
        <p class="mb-2 px-3 text-[11px] font-semibold text-[var(--muted)]">概览</p>
        <button v-for="item in navigation" :key="item.to" type="button" class="nav-item mb-1 flex w-full items-center border-l-2 px-3 py-2 text-left text-sm font-medium"
          :class="active(item.to) ? 'nav-item-active border-[var(--primary)] bg-[var(--primary-soft)] text-[var(--primary)]' : 'border-transparent text-[var(--muted)] hover:bg-[var(--surface-muted)] hover:text-[var(--text)]'" @pointerenter="preload(item.to)" @focus="preload(item.to)" @click="navigate(item.to)">
          <span class="nav-item-content"><component :is="item.icon" :size="18" /><span>{{ item.label }}</span></span>
        </button>

        <button type="button" class="mb-1 mt-5 flex w-full items-center justify-between px-3 text-[11px] font-semibold text-[var(--muted)]" @click="catalogOpen = !catalogOpen">
          <span>音乐资料库</span><ChevronDown :size="14" class="transition" :class="catalogOpen ? '' : '-rotate-90'" />
        </button>
        <div class="nav-group-grid" :class="{ 'nav-group-grid-open': catalogOpen }" :aria-hidden="!catalogOpen" :inert="!catalogOpen">
          <div class="nav-group-inner">
            <button v-for="item in catalog" :key="item.to" type="button" class="nav-item mb-1 flex w-full items-center border-l-2 px-3 py-2 text-left text-sm font-medium"
              :class="active(item.to) ? 'nav-item-active border-[var(--primary)] bg-[var(--primary-soft)] text-[var(--primary)]' : 'border-transparent text-[var(--muted)] hover:bg-[var(--surface-muted)] hover:text-[var(--text)]'" @pointerenter="preload(item.to)" @focus="preload(item.to)" @click="navigate(item.to)">
              <span class="nav-item-content"><component :is="item.icon" :size="18" /><span>{{ item.label }}</span></span>
            </button>
          </div>
        </div>

        <p class="mb-2 mt-5 px-3 text-[11px] font-semibold text-[var(--muted)]">系统</p>
        <button v-for="item in operations" :key="item.to" type="button" class="nav-item mb-1 flex w-full items-center border-l-2 px-3 py-2 text-left text-sm font-medium"
          :class="active(item.to) ? 'nav-item-active border-[var(--primary)] bg-[var(--primary-soft)] text-[var(--primary)]' : 'border-transparent text-[var(--muted)] hover:bg-[var(--surface-muted)] hover:text-[var(--text)]'" @pointerenter="preload(item.to)" @focus="preload(item.to)" @click="navigate(item.to)">
          <span class="nav-item-content"><component :is="item.icon" :size="18" /><span>{{ item.label }}</span></span>
        </button>
      </nav>

      <div class="border-t border-[var(--border)] p-3">
        <div class="flex items-center gap-3 px-1 py-1">
          <button type="button" class="group relative grid h-8 w-8 shrink-0 place-items-center overflow-hidden rounded-full bg-[var(--surface-strong)] text-xs font-bold text-[var(--text)] ring-offset-2 ring-offset-[var(--surface-solid)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--primary)]" aria-label="修改管理员头像" @click="avatarOpen = true">
            <img v-if="auth.profile?.avatar" :src="auth.profile.avatar.url" alt="管理员头像" class="h-full w-full object-cover" width="32" height="32" decoding="async" />
            <span v-else>{{ initials }}</span>
            <span class="absolute inset-0 grid place-items-center bg-slate-950/55 text-white opacity-0 transition-opacity group-hover:opacity-100 group-focus-visible:opacity-100"><Camera :size="13" /></span>
          </button>
          <div class="min-w-0 flex-1">
            <p class="truncate text-sm font-semibold">{{ auth.profile?.displayName }}</p>
            <p class="truncate text-xs text-[var(--muted)]">@{{ auth.profile?.username }}</p>
          </div>
          <button type="button" class="btn btn-ghost btn-icon" aria-label="退出登录" @click="logout"><LogOut :size="17" /></button>
        </div>
      </div>
    </aside>

    <div class="lg:pl-[248px]">
      <header class="sticky top-0 z-20 flex h-14 items-center gap-3 border-b border-[var(--border)] bg-[var(--surface-solid)] px-4 sm:px-6">
        <button type="button" class="mobile-nav-control btn btn-secondary btn-icon lg:hidden" aria-label="打开导航" @click="ui.sidebarOpen = true"><Menu :size="18" /></button>
        <form class="relative hidden w-full max-w-md sm:block" role="search" @submit.prevent="search">
          <Search :size="17" class="pointer-events-none absolute left-3.5 top-1/2 -translate-y-1/2 text-[var(--muted)]" />
          <input v-model="quickSearch" class="ui-input !pl-10" type="search" placeholder="搜索曲目、专辑或艺术家" aria-label="全局搜索" />
          <kbd class="pointer-events-none absolute right-3 top-1/2 -translate-y-1/2 rounded border border-[var(--border)] px-1.5 py-0.5 text-[10px] text-[var(--muted)]">Enter</kbd>
        </form>
        <div class="ml-auto flex items-center gap-2">
          <button type="button" class="theme-toggle btn btn-secondary btn-icon" :aria-label="colorMode === 'dark' ? '切换到浅色' : '切换到深色'" @click="colorMode = colorMode === 'dark' ? 'light' : 'dark'">
            <span class="theme-icon-stage" aria-hidden="true">
              <Transition name="theme-icon">
                <Sun v-if="colorMode === 'dark'" key="sun" :size="17" />
                <Moon v-else key="moon" :size="17" />
              </Transition>
            </span>
          </button>
          <div class="hidden items-center gap-2 border border-[var(--border)] bg-[var(--surface-muted)] px-2.5 py-1.5 text-xs font-medium text-[var(--muted)] md:flex" :title="readiness.error.value instanceof Error ? readiness.error.value.message : undefined">
            <span class="h-1.5 w-1.5 rounded-full" :class="serviceReady ? 'bg-emerald-500' : readiness.isPending.value ? 'animate-pulse bg-amber-500' : 'bg-rose-500'" />{{ serviceLabel }}
          </div>
        </div>
        <Transition name="route-progress-fade">
          <span v-if="ui.routePending" class="route-progress" aria-hidden="true" />
        </Transition>
      </header>
      <main class="w-full px-4 py-5 sm:px-6 lg:px-8">
        <div class="relative min-h-[calc(100vh-6rem)]">
          <RouterView v-slot="{ Component, route: viewRoute }">
            <div :key="viewRoute.path" :data-route-path="viewRoute.path" class="min-h-[calc(100vh-6rem)] bg-[var(--bg)]">
              <component :is="Component" />
            </div>
          </RouterView>
        </div>
      </main>
    </div>
    <BaseDialog v-model="avatarOpen" title="管理员头像" description="头像会显示在管理端侧栏，并同步到当前管理员账户。">
      <div v-if="auth.profile" class="flex flex-col items-center gap-4 py-3">
        <ArtworkUploadField :target-id="auth.profile.id" purpose="USER_AVATAR" :image-url="auth.profile.avatar?.url" :alt="`${auth.profile.displayName}的头像`" noun="头像" shape="circle" @completed="avatarCompleted">
          <span class="text-3xl font-extrabold">{{ initials }}</span>
        </ArtworkUploadField>
        <p class="max-w-sm text-center text-xs leading-5 text-[var(--muted)]">支持 JPEG、PNG、WebP；上传完成后当前会话会立即刷新。</p>
      </div>
      <template #footer><button type="button" class="btn btn-secondary" @click="avatarOpen = false">关闭</button></template>
    </BaseDialog>
  </div>
</template>

<style scoped>
.nav-backdrop-enter-active,
.nav-backdrop-leave-active { transition: opacity var(--motion-base) ease; }
.nav-backdrop-enter-from,
.nav-backdrop-leave-to { opacity: 0; }
.nav-group-grid {
  display: grid;
  grid-template-rows: 0fr;
  opacity: 0;
  visibility: hidden;
  transition: grid-template-rows var(--motion-base) var(--motion-ease-out), opacity var(--motion-fast) ease, visibility 0s linear var(--motion-base);
}
.nav-group-grid-open {
  grid-template-rows: 1fr;
  opacity: 1;
  visibility: visible;
  transition-delay: 0s;
}
.nav-group-inner { min-height: 0; overflow: hidden; }
.nav-item {
  transition: background-color var(--motion-fast) ease, border-color var(--motion-fast) ease, color var(--motion-fast) ease;
}
.nav-item-content {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: .75rem;
  transition: transform var(--motion-base) var(--motion-ease-out);
}
.nav-item:hover .nav-item-content,
.nav-item:focus-visible .nav-item-content { transform: translateX(3px); }
.nav-item-active .nav-item-content { transform: translateX(2px); }
.nav-item-active:hover .nav-item-content,
.nav-item-active:focus-visible .nav-item-content { transform: translateX(3px); }
.nav-item:active .nav-item-content { transform: translateX(2px) scale(.985); }
.theme-toggle { overflow: hidden; }
.theme-icon-stage {
  position: relative;
  display: grid;
  width: 17px;
  height: 17px;
  place-items: center;
}
.theme-icon-stage :deep(svg) {
  position: absolute;
  inset: 0;
  transform-origin: center;
}
.theme-icon-enter-active,
.theme-icon-leave-active { transition: opacity var(--motion-fast) ease, transform var(--motion-base) var(--motion-ease-out); }
.theme-icon-enter-from { opacity: 0; transform: rotate(-90deg) scale(.55); }
.theme-icon-leave-to { opacity: 0; transform: rotate(90deg) scale(.55); }
@media (min-width: 1024px) {
  .mobile-nav-control { display: none !important; }
}
</style>
