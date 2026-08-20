import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";
import AppPagination from "@/components/AppPagination.vue";

describe("AppPagination", () => {
  it("moves back to the last available page when data shrinks", async () => {
    const wrapper = mount(AppPagination, {
      props: { page: 5, pageSize: 50, total: 60 },
    });

    await wrapper.vm.$nextTick();

    expect(wrapper.emitted("change")).toEqual([[2]]);
    expect(wrapper.get("nav").attributes("aria-label")).toBe("分页");
  });

  it("offers only the supported page sizes", async () => {
    const wrapper = mount(AppPagination, {
      props: { page: 1, pageSize: 100, total: 1_000 },
    });

    expect(wrapper.findAll("option").map((option) => option.text())).toEqual(["50", "100", "200", "500", "1000"]);

    await wrapper.get("select").setValue("1000");
    expect(wrapper.emitted("pageSizeChange")).toEqual([[1_000]]);
  });

  it("caps server-reported pages at the supported offset", async () => {
    const wrapper = mount(AppPagination, {
      props: { page: 1, pageSize: 100, total: 50_000, totalPages: 500 },
    });

    await wrapper.vm.$nextTick();

    expect(wrapper.text()).toContain("第 1 / 101 页");
    expect(wrapper.text()).toContain("仅开放前 101 页");
    expect(wrapper.emitted("change")).toBeUndefined();
  });
});
