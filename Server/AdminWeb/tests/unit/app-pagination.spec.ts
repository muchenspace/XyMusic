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

    expect(wrapper.findAll("option").map((option) => option.text())).toEqual(["50", "100", "200", "500", "1000", "5000", "10000", "100000"]);

    await wrapper.get("select").setValue("100000");
    expect(wrapper.emitted("pageSizeChange")).toEqual([[100_000]]);
  });

  it("keeps the full logical range without a total-row cap", async () => {
    const wrapper = mount(AppPagination, {
      props: { page: 1, pageSize: 100, total: 200_000, totalPages: 2_000 },
    });

    await wrapper.vm.$nextTick();

    expect(wrapper.text()).toContain("2000");
    expect(wrapper.emitted("change")).toBeUndefined();
  });

  it("keeps the full logical range for cursor pagination", async () => {
    const wrapper = mount(AppPagination, {
      props: { page: 10_000, pageSize: 100, total: 2_000_000, totalPages: 20_000, cursor: true },
    });

    await wrapper.vm.$nextTick();

    expect(wrapper.text()).toContain("10000 / 20000");
    expect(wrapper.emitted("change")).toBeUndefined();
  });
});
