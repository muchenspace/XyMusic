import { mount } from "@vue/test-utils";
import { afterEach, describe, expect, it, vi } from "vitest";
import AnimatedNumber from "@/components/AnimatedNumber.vue";

afterEach(() => {
  vi.restoreAllMocks();
});

describe("AnimatedNumber", () => {
  it("renders the initial value without replaying an entrance count", () => {
    const wrapper = mount(AnimatedNumber, { props: { value: 12_345 } });
    expect(wrapper.text()).toBe((12_345).toLocaleString());
  });

  it("moves to the latest value over animation frames", async () => {
    let callback: FrameRequestCallback | undefined;
    vi.spyOn(window, "requestAnimationFrame").mockImplementation((next) => { callback = next; return 1; });
    vi.spyOn(window, "cancelAnimationFrame").mockImplementation(() => undefined);
    vi.spyOn(performance, "now").mockReturnValue(100);
    Object.defineProperty(window, "matchMedia", {
      configurable: true,
      value: vi.fn(() => ({ matches: false })),
    });
    const wrapper = mount(AnimatedNumber, { props: { value: 10, duration: 100 } });

    await wrapper.setProps({ value: 20 });
    callback?.(150);
    await wrapper.vm.$nextTick();
    expect(Number(wrapper.text().replace(/,/g, ""))).toBeGreaterThan(10);
    expect(Number(wrapper.text().replace(/,/g, ""))).toBeLessThanOrEqual(20);

    callback?.(200);
    await wrapper.vm.$nextTick();
    expect(wrapper.text()).toBe("20");
  });

  it("updates immediately when reduced motion is preferred", async () => {
    Object.defineProperty(window, "matchMedia", {
      configurable: true,
      value: vi.fn(() => ({ matches: true })),
    });
    const wrapper = mount(AnimatedNumber, { props: { value: 1 } });
    await wrapper.setProps({ value: 99 });
    expect(wrapper.text()).toBe("99");
  });
});
