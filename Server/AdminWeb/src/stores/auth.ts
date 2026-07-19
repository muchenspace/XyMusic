import { computed, ref } from "vue";
import { defineStore } from "pinia";
import { setCsrfToken } from "@/api/client";
import { ApiError } from "@/shared/application/api-error";
import type { AdminSession } from "@/features/auth/domain/models";
import { useAuth } from "@/app/services/auth";
import { clearAdminQueryCache } from "@/app/query-client";

export const useAuthStore = defineStore("auth", () => {
  const auth = useAuth();
  const session = ref<AdminSession | null>(null);
  const checked = ref(false);
  const loading = ref(false);
  let pending: Promise<AdminSession | null> | undefined;
  let sessionEpoch = 0;
  let loadingOperations = 0;

  function beginLoading(): void {
    loadingOperations += 1;
    loading.value = true;
  }

  function endLoading(): void {
    loadingOperations = Math.max(0, loadingOperations - 1);
    loading.value = loadingOperations > 0;
  }

  const profile = computed(() => session.value?.user ?? null);
  const isAuthenticated = computed(() => Boolean(session.value));

  async function ensureSession(force = false): Promise<AdminSession | null> {
    if (checked.value && !force) return session.value;
    if (pending) return pending;
    const requestEpoch = sessionEpoch;
    beginLoading();
    pending = auth.session()
      .then(async (value) => {
        if (requestEpoch !== sessionEpoch) return session.value;
        if (session.value?.user.id && session.value.user.id !== value.user.id) {
          await clearAdminQueryCache();
        }
        session.value = value;
        setCsrfToken(value.csrfToken);
        checked.value = true;
        return value;
      })
      .catch((error: unknown) => {
        if (requestEpoch !== sessionEpoch) return session.value;
        if (error instanceof ApiError && error.status === 401) {
          const hadSession = Boolean(session.value);
          checked.value = true;
          session.value = null;
          setCsrfToken();
          return hadSession ? clearAdminQueryCache().then(() => null) : null;
        }
        throw error;
      })
      .finally(() => {
        endLoading();
        pending = undefined;
      });
    return pending;
  }

  async function login(username: string, password: string): Promise<void> {
    beginLoading();
    try {
      const nextSession = await auth.login(username, password);
      sessionEpoch += 1;
      if (session.value?.user.id !== nextSession.user.id) await clearAdminQueryCache();
      session.value = nextSession;
      setCsrfToken(session.value.csrfToken);
      checked.value = true;
    } finally {
      endLoading();
    }
  }

  async function logout(): Promise<void> {
    sessionEpoch += 1;
    let failure: unknown;
    try {
      await auth.logout();
    } catch (error) {
      failure = error;
    } finally {
      session.value = null;
      setCsrfToken();
      checked.value = true;
      await clearAdminQueryCache();
    }
    if (failure) throw failure;
  }

  async function clear(): Promise<void> {
    sessionEpoch += 1;
    session.value = null;
    setCsrfToken();
    checked.value = true;
    await clearAdminQueryCache();
  }

  return { session, checked, loading, profile, isAuthenticated, ensureSession, login, logout, clear };
});
