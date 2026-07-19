import { mount } from "@vue/test-utils";
import { nextTick } from "vue";
import { afterEach, describe, expect, it, vi } from "vitest";
import DistributionChart from "@/components/DistributionChart.vue";
import PageHeader from "@/components/PageHeader.vue";
import TrackStatusDisc from "@/components/TrackStatusDisc.vue";
import type { TrackSummary } from "@/features/music/domain/models";

afterEach(() => {
  vi.useRealTimers();
  document.body.innerHTML = "";
});

function track(): TrackSummary {
  return {
    id: "track-1",
    title: "Test track",
    artistCredits: [],
    artists: ["Artist"],
    album: null,
    artwork: null,
    durationMs: 123_000,
    trackNumber: 1,
    discNumber: 1,
    status: "READY",
    audioStatus: "PROCESSING",
    metadataStatus: "ORIGINAL",
    metadataVersion: 1,
    source: {
      id: "source-1",
      rootId: "root-1",
      rootName: "Music",
      relativePath: "Artist/Test.flac",
      format: "FLAC",
      status: "PROCESSING",
      checksumSha256: null,
      mode: "READ_ONLY",
      canWriteBack: false,
      writebackBlockReason: null,
    },
    mediaProcessing: null,
    variantSummary: [],
    activeWritebackJobId: null,
    publishedAt: null,
    createdAt: "2026-07-18T00:00:00.000Z",
    updatedAt: "2026-07-18T00:00:00.000Z",
    version: 1,
  };
}

describe("motion component polish", () => {
  it("renders the PageHeader eyebrow and description without disturbing actions", () => {
    const wrapper = mount(PageHeader, {
      props: { title: "仪表盘", description: "实时数据汇总" },
      slots: { eyebrow: "运营概览", actions: "刷新" },
    });

    expect(wrapper.get("h1").text()).toBe("仪表盘");
    expect(wrapper.text()).toContain("运营概览");
    expect(wrapper.text()).toContain("实时数据汇总");
    expect(wrapper.text()).toContain("刷新");
  });

  it("keeps chart segments mounted while their geometry changes", async () => {
    const wrapper = mount(DistributionChart, {
      props: { values: { READY: 4, ERROR: 0 } },
    });
    const errorBefore = wrapper.findAll(".distribution-segment")
      .find((segment) => segment.attributes("aria-label")?.startsWith("异常"));

    expect(errorBefore).toBeDefined();
    expect(errorBefore!.classes()).toContain("distribution-segment--empty");
    expect(errorBefore!.attributes("tabindex")).toBe("-1");
    const element = errorBefore!.element;
    const dasharray = errorBefore!.attributes("stroke-dasharray");

    await wrapper.setProps({ values: { READY: 1, ERROR: 3 } });
    const errorAfter = wrapper.findAll(".distribution-segment")
      .find((segment) => segment.attributes("aria-label")?.startsWith("异常"));

    expect(errorAfter).toBeDefined();
    expect(errorAfter!.element).toBe(element);
    expect(errorAfter!.classes()).not.toContain("distribution-segment--empty");
    expect(errorAfter!.attributes("tabindex")).toBe("0");
    expect(errorAfter!.attributes("stroke-dasharray")).not.toBe(dasharray);
  });

  it("allows the pointer to cross the tooltip gap before closing", async () => {
    vi.useFakeTimers();
    const wrapper = mount(TrackStatusDisc, { props: { track: track() }, attachTo: document.body });
    const triggerArea = wrapper.get(".inline-flex");

    await triggerArea.trigger("mouseenter");
    expect(document.body.querySelector('[role="tooltip"]')).not.toBeNull();

    await triggerArea.trigger("mouseleave");
    await vi.advanceTimersByTimeAsync(99);
    expect(document.body.querySelector('[role="tooltip"]')).not.toBeNull();

    const tooltip = document.body.querySelector('[role="tooltip"]') as HTMLElement;
    tooltip.dispatchEvent(new MouseEvent("mouseenter"));
    await nextTick();
    await vi.advanceTimersByTimeAsync(1);
    expect(document.body.querySelector('[role="tooltip"]')).not.toBeNull();

    tooltip.dispatchEvent(new MouseEvent("mouseleave"));
    await vi.advanceTimersByTimeAsync(100);
    await nextTick();
    expect(document.body.querySelector('[role="tooltip"]')).toBeNull();

    wrapper.unmount();
  });
});
