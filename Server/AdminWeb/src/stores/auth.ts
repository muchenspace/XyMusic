import { computed, ref } from "vue";
import { defineStore } from "pinia";
import type { AdminSession } from "@/features/auth/domain/models";
import { createAuthSessionCoordinator } from "@/app/services/auth";

export const useAuthStore = defineStore("auth", () => {
  const session = ref<AdminSession | null>(null);
  const checked = ref(false);
  const loading = ref(false);
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

  const coordinator = createAuthSessionCoordinator({
    getSession: () => session.value,
    setSession: (value) => { session.value = value; },
    getChecked: () => checked.value,
    setChecked: (value) => { checked.value = value; },
    beginLoading,
    endLoading,
  });

  function ensureSession(force = false): Promise<AdminSession | null> {
    return coordinator.ensureSession(force);
  }

  function login(username: string, password: string): Promise<void> {
    return coordinator.login(username, password);
  }

  function logout(): Promise<void> {
    return coordinator.logout();
  }

  function clear(): Promise<void> {
    return coordinator.clear();
  }

  return { session, checked, loading, profile, isAuthenticated, ensureSession, login, logout, clear };
});
