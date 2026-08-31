import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";
import { h } from "vue";
import VirtualTable from "@/components/VirtualTable.vue";

describe("VirtualTable", () => {
  it("renders only a small viewport window for a very large page", async () => {
    const items = Array.from({ length: 100_000 }, (_, id) => ({ id }));
    const wrapper = mount(VirtualTable<{ id: number }>, {
      props: {
        items,
        columns: 1,
        rowHeight: 40,
        overscan: 4,
        height: "200px",
        rowKey: (item) => item.id,
      },
      slots: {
        header: "<tr><th>Item</th></tr>",
        default: ({ item }) => h("tr", { "data-row": item.id }, [h("td", item.id)]),
      },
    });

    await wrapper.vm.$nextTick();

    expect(wrapper.findAll("tbody tr[data-row]").length).toBeLessThan(20);
    expect(wrapper.find("tbody tr[data-row=\"0\"]").exists()).toBe(true);
    expect(wrapper.find("tbody tr[data-row=\"99999\"]").exists()).toBe(false);

    const viewport = wrapper.element as HTMLElement;
    Object.defineProperty(viewport, "clientHeight", { configurable: true, value: 200 });
    Object.defineProperty(viewport, "scrollHeight", { configurable: true, value: 4_000_000 });
    Object.defineProperty(viewport, "scrollTop", { configurable: true, writable: true, value: 2_000_000 });
    await viewport.dispatchEvent(new Event("scroll"));
    await wrapper.vm.$nextTick();

    expect(wrapper.find("tbody tr[data-row=\"50000\"]").exists()).toBe(true);
    expect(wrapper.find("tbody tr[data-row=\"0\"]").exists()).toBe(false);
  });
});
