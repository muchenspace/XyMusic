import { h } from "vue";
import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";
import VirtualGrid from "@/components/VirtualGrid.vue";

describe("VirtualGrid", () => {
  it("keeps large card pages bounded to the visible rows", async () => {
    const items = Array.from({ length: 100_000 }, (_, id) => ({ id }));
    const wrapper = mount(VirtualGrid<{ id: number }>, {
      props: {
        items,
        itemHeight: 120,
        minItemWidth: 200,
        gap: 10,
        overscan: 1,
        height: "240px",
        rowKey: (item) => item.id,
      },
      slots: {
        default: ({ item }) => h("article", { "data-card": item.id }, String(item.id)),
      },
    });

    await wrapper.vm.$nextTick();

    expect(wrapper.findAll("article[data-card]").length).toBeLessThan(20);
    expect(wrapper.find("article[data-card=\"0\"]").exists()).toBe(true);
    expect(wrapper.find("article[data-card=\"99999\"]").exists()).toBe(false);

    const viewport = wrapper.element as HTMLElement;
    Object.defineProperty(viewport, "clientWidth", { configurable: true, value: 500 });
    Object.defineProperty(viewport, "clientHeight", { configurable: true, value: 240 });
    Object.defineProperty(viewport, "scrollTop", { configurable: true, writable: true, value: 0 });
    await viewport.dispatchEvent(new Event("scroll"));
    await wrapper.vm.$nextTick();
    viewport.scrollTop = 1_200_000;
    await viewport.dispatchEvent(new Event("scroll"));
    await wrapper.vm.$nextTick();
    expect(wrapper.find("article[data-card=\"20000\"]").exists()).toBe(true);
    expect(wrapper.find("article[data-card=\"0\"]").exists()).toBe(false);
  });
});
