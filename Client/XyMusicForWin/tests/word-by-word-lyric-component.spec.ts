import { mount } from "@vue/test-utils";
import { nextTick } from "vue";
import { afterEach, describe, expect, it, vi } from "vitest";
import WordByWordLyricText from "../src/shared/lyrics/WordByWordLyricText.vue";

describe("word-by-word lyric component layout", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it("drops stale overlay geometry before rebuilding after a resize", async () => {
    let observerCallback!: ResizeObserverCallback;
    let observerInstance!: ResizeObserver;
    class ResizeObserverStub {
      constructor(callback: ResizeObserverCallback) {
        observerCallback = callback;
        observerInstance = this as unknown as ResizeObserver;
      }

      observe() {}
      unobserve() {}
      disconnect() {}
    }
    vi.stubGlobal("ResizeObserver", ResizeObserverStub);

    let wrapped = false;
    const layoutSize = () => wrapped
      ? { width: 10, height: 40 }
      : { width: 100, height: 20 };
    vi.spyOn(HTMLElement.prototype, "offsetWidth", "get").mockImplementation(() => layoutSize().width);
    vi.spyOn(HTMLElement.prototype, "offsetHeight", "get").mockImplementation(() => layoutSize().height);
    vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockImplementation(() => domRect(
      0,
      0,
      layoutSize().width,
      layoutSize().height,
    ));
    vi.spyOn(document, "createRange").mockImplementation(() => {
      let startOffset = 0;
      return {
        setStart(_node: Node, offset: number) { startOffset = offset; },
        setEnd() {},
        getClientRects() {
          const rect = wrapped
            ? domRect(0, startOffset * 20, 10, 20)
            : domRect(startOffset * 10, 0, 10, 20);
          return [rect] as unknown as DOMRectList;
        },
      } as unknown as Range;
    });

    const wrapper = mount(WordByWordLyricText, {
      props: {
        text: "AB",
        words: [
          { time: 0, endTime: 1, text: "A" },
          { time: 1, endTime: 2, text: "B" },
        ],
        progresses: [1, 1],
        containerClass: "test-lyric",
        highlightColor: "#ffffff",
      },
      slots: { default: () => "AB" },
    });

    await nextTick();
    await nextTick();
    expect(wrapper.get(".word-by-word-lyric-overlay").attributes("viewBox")).toBe("0 0 100 20");

    wrapped = true;
    observerCallback([], observerInstance);
    await nextTick();
    expect(wrapper.find(".word-by-word-lyric-overlay").exists()).toBe(false);

    await nextTick();
    const rebuiltOverlay = wrapper.get(".word-by-word-lyric-overlay");
    expect(rebuiltOverlay.attributes("viewBox")).toBe("0 0 10 40");
    expect(rebuiltOverlay.findAll("rect")[1]?.attributes("y")).toBe("20");

    wrapper.unmount();
  });
});

function domRect(left: number, top: number, width: number, height: number): DOMRect {
  return {
    x: left,
    y: top,
    left,
    top,
    right: left + width,
    bottom: top + height,
    width,
    height,
    toJSON: () => ({}),
  } as DOMRect;
}
