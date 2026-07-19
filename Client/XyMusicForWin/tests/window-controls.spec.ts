import { mount } from "@vue/test-utils";
import { createPinia } from "pinia";
import { nextTick } from "vue";
import { describe, expect, it, vi } from "vitest";
import type { ApplicationServices } from "../src/application/services";
import TopBar from "../src/presentation/components/TopBar.vue";
import { applicationServicesKey } from "../src/presentation/services";

describe("window controls", () => {
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
            desktopWindow: {
              minimize: vi.fn(async () => undefined),
              toggleMaximize,
              isMaximized: vi.fn(async () => false),
              close: vi.fn(async () => undefined),
              onResized: vi.fn(async () => () => undefined),
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
    await wrapper.get(".titlebar-drag-region").trigger("dblclick");
    expect(toggleMaximize).not.toHaveBeenCalled();

    await wrapper.setProps({ fullscreen: false });
    expect(wrapper.get(".window-controls").findAll("button")).toHaveLength(3);
    await wrapper.get(".titlebar-drag-region").trigger("dblclick");
    expect(toggleMaximize).toHaveBeenCalledOnce();

    wrapper.unmount();
  });
});
