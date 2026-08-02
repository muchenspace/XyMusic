import { onScopeDispose, ref, watch } from "vue";
import { defineStore } from "pinia";
import type { ThemePreference } from "../../application/ports/UserInterfacePreferences";
import { useApplicationServices } from "../services";

export type Theme = "dark" | "light";
export type { ThemePreference } from "../../application/ports/UserInterfacePreferences";

export const useThemeStore = defineStore("theme", () => {
  const services = useApplicationServices();
  const desktopWindowController = services.desktopWindowController;
  const uiPreferences = services.uiPreferences;
  const theme = ref<Theme>("dark");
  const preference = ref<ThemePreference>(uiPreferences.readTheme());
  let initialized = false;
  let mediaQuery: MediaQueryList | null = null;

  function initialize() {
    if (initialized) return;
    initialized = true;
    mediaQuery = window.matchMedia("(prefers-color-scheme: light)");
    const initialTheme = resolveTheme(preference.value, mediaQuery.matches);
    if (theme.value === initialTheme) applyTheme();
    else theme.value = initialTheme;
    mediaQuery.addEventListener("change", syncSystemTheme);
  }

  function applyTheme() {
    document.documentElement.dataset.theme = theme.value;
    document.documentElement.style.colorScheme = theme.value;
    void desktopWindowController.setTheme(theme.value).catch(() => undefined);
  }

  function toggle() {
    setPreference(theme.value === "dark" ? "light" : "dark");
  }

  function setPreference(value: ThemePreference): void {
    preference.value = value;
  }

  function syncSystemTheme(event: MediaQueryListEvent): void {
    if (preference.value === "system") theme.value = event.matches ? "light" : "dark";
  }

  watch(preference, (value) => {
    uiPreferences.writeTheme(value);
    theme.value = resolveTheme(value, mediaQuery?.matches ?? window.matchMedia("(prefers-color-scheme: light)").matches);
  });
  // Apply theme changes in the same turn so first paint and native chrome agree.
  watch(theme, applyTheme, { flush: "sync" });

  onScopeDispose(() => mediaQuery?.removeEventListener("change", syncSystemTheme));

  return { theme, preference, initialize, toggle, setPreference };
});

function resolveTheme(preference: ThemePreference, systemLight: boolean): Theme {
  return preference === "system" ? systemLight ? "light" : "dark" : preference;
}
