import { createApp, defineComponent, h, nextTick } from "vue";
import { createPinia } from "pinia";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { ApplicationServices } from "../src/application/services";
import { applicationServicesKey } from "../src/presentation/services";
import { useThemeStore } from "../src/presentation/stores/themeStore";

const initialDocumentTheme = document.documentElement.dataset.theme;
const initialColorScheme = document.documentElement.style.colorScheme;

describe("theme store", () => {
  afterEach(() => {
    vi.restoreAllMocks();
    if (initialDocumentTheme === undefined) delete document.documentElement.dataset.theme;
    else document.documentElement.dataset.theme = initialDocumentTheme;
    document.documentElement.style.colorScheme = initialColorScheme;
  });

  it("applies the initial system theme to native chrome once", async () => {
    const mediaQuery = {
      matches: true,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    } as unknown as MediaQueryList;
    vi.spyOn(window, "matchMedia").mockReturnValue(mediaQuery);
    const setTheme = vi.fn(async () => undefined);
    const app = createApp(defineComponent({
      setup() {
        useThemeStore().initialize();
        return () => h("div");
      },
    }));
    app.use(createPinia());
    app.provide(applicationServicesKey, {
      desktopWindowController: { setTheme },
      uiPreferences: { readTheme: () => "system", writeTheme: vi.fn() },
    } as unknown as ApplicationServices);
    const element = document.createElement("div");
    document.body.appendChild(element);
    app.mount(element);
    await nextTick();

    expect(document.documentElement.dataset.theme).toBe("light");
    expect(setTheme).toHaveBeenCalledExactlyOnceWith("light");

    app.unmount();
    element.remove();
  });
});
