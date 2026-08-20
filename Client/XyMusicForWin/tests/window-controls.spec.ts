import { mount } from "@vue/test-utils";
import { createPinia } from "pinia";
import { nextTick } from "vue";
import { describe, expect, it, vi } from "vitest";
import type { ApplicationServices } from "../src/application/services";
import TopBar from "../src/presentation/components/TopBar.vue";
import { applicationServicesKey } from "../src/presentation/services";

describe("window controls", () => {
  it("reflects native maximization updates in the restore control", async () => {
    let stateListener!: (state: { maximized: boolean; fullscreen: boolean }) => void;
    const wrapper = mount(TopBar, {
      props: { modelValue: "", searching: false },
      global: {
        plugins: [createPinia()],
        provide: {
          [applicationServicesKey as symbol]: {
            desktopWindowController: {
              state: () => ({ maximized: false, fullscreen: false }),
              subscribe: (listener: (state: { maximized: boolean; fullscreen: boolean }) => void) => {
                stateListener = listener;
                listener({ maximized: false, fullscreen: false });
                return () => undefined;
              },
              minimize: vi.fn(async () => undefined),
              toggleMaximize: vi.fn(async () => undefined),
              close: vi.fn(async () => undefined),
            },
            uiPreferences: { readTheme: () => "dark", writeTheme: vi.fn() },
          } as unknown as ApplicationServices,
        },
      },
    });

    const maximizeControl = () => wrapper.get(".window-controls").findAll("button")[1]!;
    expect(maximizeControl().attributes("aria-label")).toBe("最大化");

    stateListener({ maximized: true, fullscreen: false });
    await nextTick();

    expect(maximizeControl().attributes("aria-label")).toBe("还原");
    wrapper.unmount();
  });

  it("hides controls and ignores titlebar double-clicks in fullscreen", async () => {
    const toggleMaximize = vi.fn(async () => undefined);
    const wrapper = mount(TopBar, {
      props: {
        modelValue: "",
        searching: false,
        fullscreen: true,
      },
      global: {
        plugins: [createPinia()],
        provide: {
          [applicationServicesKey as symbol]: {
            desktopWindowController: {
              state: () => ({ maximized: false, fullscreen: false }),
              subscribe: vi.fn(() => () => undefined),
              minimize: vi.fn(async () => undefined),
              toggleMaximize,
              close: vi.fn(async () => undefined),
            },
            uiPreferences: {
              readTheme: () => "dark",
              writeTheme: vi.fn(),
            },
          } as unknown as ApplicationServices,
        },
      },
    });

    await nextTick();
    expect(wrapper.find(".window-controls").exists()).toBe(false);
    expect(wrapper.get(".titlebar-drag-region").attributes("data-tauri-drag-region")).toBeUndefined();
    await wrapper.get(".titlebar-drag-region").trigger("dblclick");
    expect(toggleMaximize).not.toHaveBeenCalled();

    await wrapper.setProps({ fullscreen: false });
    expect(wrapper.get(".window-controls").findAll("button")).toHaveLength(3);
    expect(wrapper.get(".titlebar-drag-region").attributes("data-tauri-drag-region")).toBe("");
    await wrapper.get(".titlebar-drag-region").trigger("dblclick");
    expect(toggleMaximize).toHaveBeenCalledOnce();

    wrapper.unmount();
  });
});
