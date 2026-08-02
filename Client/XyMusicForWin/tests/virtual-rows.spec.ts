import { mount } from "@vue/test-utils";
import { defineComponent, h, nextTick, ref, type Ref } from "vue";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useVirtualRows } from "../src/presentation/composables/useVirtualRows";

const mountedWrappers: Array<{ unmount: () => void }> = [];

describe("virtual rows", () => {
  const animationFrames: FrameRequestCallback[] = [];
  let cancelAnimationFrame: ReturnType<typeof vi.fn>;
  let viewportHeight: PropertyDescriptor | undefined;

  beforeEach(() => {
    animationFrames.splice(0);
    cancelAnimationFrame = vi.fn();
    vi.stubGlobal("requestAnimationFrame", (callback: FrameRequestCallback) => {
      animationFrames.push(callback);
      return animationFrames.length;
    });
    vi.stubGlobal("cancelAnimationFrame", cancelAnimationFrame);
    vi.spyOn(window, "getComputedStyle").mockImplementation((element) => ({
      overflowY: element instanceof HTMLElement && element.dataset.virtualScrollParent === "true" ? "auto" : "visible",
    } as CSSStyleDeclaration));
    viewportHeight = Object.getOwnPropertyDescriptor(window, "innerHeight");
    Object.defineProperty(window, "innerHeight", { configurable: true, value: 100 });
  });

  afterEach(() => {
    for (const wrapper of mountedWrappers.splice(0)) wrapper.unmount();
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
    if (viewportHeight) Object.defineProperty(window, "innerHeight", viewportHeight);
  });

  it("subscribes to scroll and resize only while the list is virtualized", async () => {
    const count = ref(80);
    const addWindowListener = vi.spyOn(window, "addEventListener");
    const removeWindowListener = vi.spyOn(window, "removeEventListener");
    const wrapper = mountVirtualRows(count, { scrollParent: true });
    const addScrollListener = vi.spyOn(wrapper.scrollParent!, "addEventListener");
    const removeScrollListener = vi.spyOn(wrapper.scrollParent!, "removeEventListener");

    await settle();
    expect(eventCalls(addWindowListener, "resize")).toHaveLength(0);
    expect(eventCalls(addScrollListener, "scroll")).toHaveLength(0);

    count.value = 81;
    await settle();
    expect(eventCalls(addWindowListener, "resize")).toHaveLength(1);
    expect(eventCalls(addScrollListener, "scroll")).toHaveLength(1);
    const resizeRemovals = eventCalls(removeWindowListener, "resize").length;

    count.value = 80;
    await settle();
    expect(eventCalls(removeWindowListener, "resize").length).toBeGreaterThan(resizeRemovals);
    expect(eventCalls(removeScrollListener, "scroll")).toHaveLength(1);
  });

  it("coalesces scroll work and keeps a buffered range stable until it is needed", async () => {
    const count = ref(200);
    const wrapper = mountVirtualRows(count);
    await settle();

    const state = wrapper.virtualRows;
    expect(state.start.value).toBe(0);
    expect(state.end.value).toBe(18);

    wrapper.setListTop(-10);
    window.dispatchEvent(new Event("scroll"));
    window.dispatchEvent(new Event("scroll"));
    window.dispatchEvent(new Event("scroll"));
    expect(animationFrames).toHaveLength(1);

    animationFrames.shift()?.(16);
    await nextTick();
    expect(state.start.value).toBe(0);
    expect(state.end.value).toBe(18);

    wrapper.setListTop(-950);
    window.dispatchEvent(new Event("scroll"));
    animationFrames.shift()?.(32);
    await nextTick();
    expect(state.start.value).toBe(87);
    expect(state.end.value).toBe(113);

    wrapper.setListTop(-1_500);
    window.dispatchEvent(new Event("scroll"));
    animationFrames.shift()?.(48);
    await nextTick();
    expect(state.start.value).toBe(142);
    expect(state.end.value).toBe(168);

    wrapper.wrapper.unmount();
  });

  it("caches element-container geometry while scrolling", async () => {
    const wrapper = mountVirtualRows(ref(200), { scrollParent: true });
    await settle();

    wrapper.rowRect.mockClear();
    wrapper.setScrollTop(950);
    wrapper.scrollParent?.dispatchEvent(new Event("scroll"));
    animationFrames.shift()?.(16);
    await nextTick();

    expect(wrapper.virtualRows.start.value).toBe(87);
    expect(wrapper.virtualRows.end.value).toBe(113);
    expect(wrapper.rowRect).not.toHaveBeenCalled();
    wrapper.wrapper.unmount();
  });

  it("invalidates cached element geometry after a layout resize", async () => {
    const wrapper = mountVirtualRows(ref(200), { scrollParent: true });
    await settle();

    wrapper.rowRect.mockClear();
    wrapper.setListTop(100);
    wrapper.setScrollTop(950);
    window.dispatchEvent(new Event("resize"));
    animationFrames.shift()?.(16);
    await nextTick();

    expect(wrapper.rowRect).toHaveBeenCalledOnce();
    expect(wrapper.virtualRows.start.value).toBe(77);
    expect(wrapper.virtualRows.end.value).toBe(103);
    wrapper.wrapper.unmount();
  });

  it("cancels queued frame work when the list disconnects", async () => {
    const wrapper = mountVirtualRows(ref(200));
    await settle();

    window.dispatchEvent(new Event("scroll"));
    expect(animationFrames).toHaveLength(1);

    wrapper.wrapper.unmount();
    expect(cancelAnimationFrame).toHaveBeenCalledWith(1);
  });
});

function mountVirtualRows(count: Ref<number>, options: { scrollParent?: boolean } = {}) {
  let listTop = 0;
  let scrollTop = 0;
  let virtualRows!: ReturnType<typeof useVirtualRows>;
  let setListTop!: (nextTop: number) => void;
  let setScrollTop!: (nextTop: number) => void;
  let scrollParent: HTMLElement | null = null;
  const rowRect = vi.fn(() => listRect(options.scrollParent ? listTop - scrollTop : listTop));
  const wrapper = mount(defineComponent({
    setup() {
      const rows = ref<HTMLElement | null>(null);
      virtualRows = useVirtualRows(count, rows, { rowHeight: 10 });
      setListTop = (nextTop) => { listTop = nextTop; };
      setScrollTop = (nextTop) => { scrollTop = nextTop; };
      const setRows = (element: Element | null) => {
        rows.value = element instanceof HTMLElement ? element : null;
        if (rows.value) rows.value.getBoundingClientRect = rowRect;
      };
      const setScrollParent = (element: Element | null) => {
        scrollParent = element instanceof HTMLElement ? element : null;
        if (!scrollParent) return;
        Object.defineProperties(scrollParent, {
          scrollTop: {
            configurable: true,
            get: () => scrollTop,
            set: (value: number) => { scrollTop = Number(value); },
          },
          clientHeight: { configurable: true, value: 100 },
        });
        scrollParent.getBoundingClientRect = () => listRect(0);
      };
      return () => {
        const rowGroup = h("div", { ref: setRows });
        if (options.scrollParent) return h("div", { ref: setScrollParent, "data-virtual-scroll-parent": "true" }, [rowGroup]);
        return rowGroup;
      };
    },
  }));
  mountedWrappers.push(wrapper);
  return { wrapper, virtualRows, setListTop, setScrollTop, scrollParent, rowRect };
}

function listRect(top: number): DOMRect {
  return { x: 0, y: top, width: 0, height: 2_000, top, right: 0, bottom: top + 2_000, left: 0, toJSON: () => ({}) } as DOMRect;
}

function eventCalls(spy: ReturnType<typeof vi.spyOn>, type: string): unknown[][] {
  return spy.mock.calls.filter(([eventType]) => eventType === type);
}

async function settle(): Promise<void> {
  await nextTick();
  await nextTick();
}
