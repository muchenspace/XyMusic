import { createRouter, createWebHistory, type RouteRecordRaw } from "vue-router";
import type { SetupStatus } from "@/features/setup/domain/models";
import { useAuthStore } from "@/stores/auth";
import { useUiStore } from "@/stores/ui";
import { ADMIN_AUTH_SYNC_STORAGE_KEY } from "@/shared/application/admin-auth-sync";
import { clearSetupState, currentSetupState } from "@/app/setup-status";

export {
  SETUP_REQUIRED_DEDUP_MS,
  SETUP_STATUS_STALE_MS,
  cacheSetupState,
  canReuseSetupState,
  invalidateSetupState,
} from "@/app/setup-status";

declare module "vue-router" {
  interface RouteMeta {
    title?: string;
    requiresAuth?: boolean;
  }
}

function serviceUnavailableRedirect(redirect: string) {
  return { name: "service-unavailable", query: { redirect } } as const;
}

const routes: RouteRecordRaw[] = [
  { path: "/unavailable", name: "service-unavailable", component: () => import("@/pages/ServiceUnavailablePage.vue"), meta: { title: "服务不可用" } },
  { path: "/setup", name: "setup", component: () => import("@/pages/setup/SetupPage.vue"), meta: { title: "首次配置" } },
  { path: "/login", name: "login", component: () => import("@/pages/auth/LoginPage.vue"), meta: { title: "登录" } },
  {
    path: "/",
    component: () => import("@/layouts/AdminLayout.vue"),
    meta: { requiresAuth: true },
    children: [
      { path: "", redirect: "/dashboard" },
      { path: "dashboard", name: "dashboard", component: () => import("@/pages/dashboard/DashboardPage.vue"), meta: { title: "仪表盘", requiresAuth: true } },
      { path: "users", name: "users", component: () => import("@/pages/users/UsersPage.vue"), meta: { title: "用户管理", requiresAuth: true } },
      { path: "music", redirect: "/music/tracks" },
      { path: "music/tracks", name: "tracks", component: () => import("@/pages/music/TracksPage.vue"), meta: { title: "曲目", requiresAuth: true } },
      { path: "music/albums", name: "albums", component: () => import("@/pages/music/AlbumsPage.vue"), meta: { title: "专辑", requiresAuth: true } },
      { path: "music/albums/:id", name: "album-detail", component: () => import("@/pages/music/AlbumDetailPage.vue"), meta: { title: "专辑详情", requiresAuth: true } },
      { path: "music/artists", name: "artists", component: () => import("@/pages/music/ArtistsPage.vue"), meta: { title: "艺术家", requiresAuth: true } },
      { path: "sources", name: "sources", component: () => import("@/pages/sources/SourcesPage.vue"), meta: { title: "音源与扫描", requiresAuth: true } },
      { path: "jobs", name: "jobs", component: () => import("@/pages/jobs/JobsPage.vue"), meta: { title: "后台任务", requiresAuth: true } },
      { path: "settings", name: "settings", component: () => import("@/pages/settings/SettingsPage.vue"), meta: { title: "系统设置", requiresAuth: true } },
    ],
  },
  { path: "/:pathMatch(.*)*", name: "not-found", component: () => import("@/pages/NotFoundPage.vue"), meta: { title: "页面不存在" } },
];

export const router = createRouter({
  history: createWebHistory("/admin/"),
  routes,
  scrollBehavior: () => ({ top: 0 }),
});

router.beforeEach(async (to, from) => {
  useUiStore().routePending = true;
  if (to.name === "service-unavailable") return true;
  if (to.meta.requiresAuth && from.meta.requiresAuth) {
    const auth = useAuthStore();
    if (auth.checked && auth.isAuthenticated) return true;
  }
  let setupStatus: SetupStatus;
  try {
    setupStatus = await currentSetupState();
  } catch {
    clearSetupState();
    return serviceUnavailableRedirect(to.fullPath);
  }
  const needsSetup = setupStatus.setupRequired;
  if (needsSetup && to.name !== "setup") return { name: "setup" };
  if (!needsSetup && to.name === "setup") {
    const auth = useAuthStore();
    let session;
    try {
      session = await auth.ensureSession();
    } catch {
      return serviceUnavailableRedirect(to.fullPath);
    }
    return { name: session ? "dashboard" : "login" };
  }
  if (to.meta.requiresAuth) {
    const auth = useAuthStore();
    let session;
    try {
      session = await auth.ensureSession();
    } catch {
      return serviceUnavailableRedirect(to.fullPath);
    }
    if (!session) return { name: "login", query: { redirect: to.fullPath } };
  }
  if (to.name === "login") {
    const auth = useAuthStore();
    let session;
    try {
      session = await auth.ensureSession();
    } catch {
      return serviceUnavailableRedirect(to.fullPath);
    }
    if (session) return { name: "dashboard" };
  }
  return true;
});

router.afterEach((to) => {
  useUiStore().routePending = false;
  document.title = to.meta.title ? `${to.meta.title} · XyMusic` : "XyMusic 管理控制台";
});

router.onError(() => {
  useUiStore().routePending = false;
});

function handleUnauthorized(): void {
  const auth = useAuthStore();
  void auth.clear();
  if (router.currentRoute.value.meta.requiresAuth) {
    void router.replace({ name: "login", query: { redirect: router.currentRoute.value.fullPath } });
  }
}

async function handleExternalAuthChange(event: StorageEvent): Promise<void> {
  if (event.key !== ADMIN_AUTH_SYNC_STORAGE_KEY || event.storageArea !== window.localStorage) return;
  const auth = useAuthStore();
  try {
    const session = await auth.ensureSession(true);
    if (!session && router.currentRoute.value.meta.requiresAuth) {
      await router.replace({ name: "login", query: { redirect: router.currentRoute.value.fullPath } });
    } else if (session && router.currentRoute.value.name === "login") {
      await router.replace({ name: "dashboard" });
    }
  } catch {
    if (router.currentRoute.value.name !== "service-unavailable") {
      await router.replace(serviceUnavailableRedirect(router.currentRoute.value.fullPath));
    }
  }
}

window.addEventListener("xymusic:unauthorized", handleUnauthorized);
window.addEventListener("storage", handleExternalAuthChange);
if (import.meta.hot) {
  import.meta.hot.dispose(() => {
    window.removeEventListener("xymusic:unauthorized", handleUnauthorized);
    window.removeEventListener("storage", handleExternalAuthChange);
  });
}
