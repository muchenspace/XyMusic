import { mount } from "@vue/test-utils";
import { afterEach, describe, expect, it, vi } from "vitest";
import TrackStatusDisc from "@/components/TrackStatusDisc.vue";
import type { TrackSummary } from "@/features/music/domain/models";

afterEach(() => { document.body.innerHTML = ""; });

function track(overrides: Partial<TrackSummary> = {}): TrackSummary {
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
    audioStatus: "READY",
    metadataStatus: "OVERRIDDEN",
    metadataVersion: 2,
    source: { id: "source-1", rootId: "root-1", rootName: "Music", relativePath: "Artist/Test.flac", format: "FLAC", status: "READY", checksumSha256: null, mode: "READ_WRITE", canWriteBack: true, writebackBlockReason: null },
    mediaProcessing: { status: "READY", attempts: 1, maxAttempts: 5, lastError: null, updatedAt: "2026-07-15T00:00:00.000Z" },
    variantSummary: [{ quality: "HIGH", codec: "opus", container: "ogg", bitrate: 192_000, sampleRate: 48_000, status: "READY" }],
    activeWritebackJobId: null,
    publishedAt: "2026-07-15T00:00:00.000Z",
    createdAt: "2026-07-15T00:00:00.000Z",
    updatedAt: "2026-07-15T00:00:00.000Z",
    version: 1,
    ...overrides,
  };
}

describe("TrackStatusDisc", () => {
  it("shows analysis, media and transcoding details on hover", async () => {
    const wrapper = mount(TrackStatusDisc, { props: { track: track() }, attachTo: document.body });
    await wrapper.find(".inline-flex").trigger("mouseenter");
    expect(document.body.textContent).toContain("源文件分析");
    expect(document.body.textContent).toContain("源文件分析完成");
    expect(document.body.textContent).toContain("媒体处理");
    expect(document.body.textContent).toContain("媒体处理完成");
    expect(document.body.textContent).toContain("使用已修改 Tag");
    expect(document.body.textContent).toContain("HIGH");
    expect(document.body.textContent).toContain("可用");
    expect(document.body.textContent).toContain("192 kbps");
    expect(document.body.textContent).not.toContain("READY");
    wrapper.unmount();
  });

  it.each([
    ["PROCESSING", "处理中", "track-disc--processing"],
    ["READY", "可用", "track-disc--ready"],
    ["ERROR", "异常", "track-disc--error"],
    ["ARCHIVED", "已归档", "track-disc--archived"],
  ] as const)("uses audioStatus %s as the main presentation", (audioStatus, label, className) => {
    const wrapper = mount(TrackStatusDisc, { props: { track: track({ audioStatus }) } });
    expect(wrapper.get("button").attributes("aria-label")).toBe(`曲目状态：${label}`);
    expect(wrapper.get("button").classes()).toContain(className);
    wrapper.unmount();
  });

  it("explains a composite processing state without exposing raw enums", async () => {
    const wrapper = mount(TrackStatusDisc, {
      props: { track: track({ audioStatus: "PROCESSING", mediaProcessing: { ...track().mediaProcessing!, status: "READY" } }) },
      attachTo: document.body,
    });
    await wrapper.find(".inline-flex").trigger("mouseenter");
    expect(document.body.textContent).toContain("音源扫描或状态校验中");
    expect(document.body.textContent).not.toContain("PROCESSING");
    wrapper.unmount();
  });

  it("shows a missing or failed source as a precise row-level audio failure", async () => {
    const source = { ...track().source!, status: "MISSING" };
    const wrapper = mount(TrackStatusDisc, {
      props: { track: track({ status: "ERROR", audioStatus: "ERROR", source }) },
      attachTo: document.body,
    });

    expect(wrapper.text()).toContain("源文件处理失败");
    expect(wrapper.get("button").classes()).toContain("track-disc--error");
    await wrapper.find(".inline-flex").trigger("mouseenter");
    expect(document.body.textContent).toContain("源文件处理失败");
    expect(document.body.textContent).toContain("源文件缺失");
    expect(document.body.textContent).not.toContain("曲目已归档");
    wrapper.unmount();
  });

  it("shows generic Tag writeback failures without replacing the audio status", async () => {
    const wrapper = mount(TrackStatusDisc, {
      props: {
        track: track({
          status: "ERROR",
          audioStatus: "ERROR",
          metadataStatus: "WRITE_FAILED",
          latestWritebackErrorCode: "WRITEBACK_VALIDATION_FAILED",
          latestWritebackError: "写回后的 Tag 校验失败",
        }),
      },
      attachTo: document.body,
    });

    expect(wrapper.text()).toContain("异常");
    expect(wrapper.get("button").classes()).toContain("track-disc--error");
    await wrapper.find(".inline-flex").trigger("mouseenter");
    expect(document.body.textContent).toContain("写回源文件失败");
    expect(document.body.textContent).toContain("写回后的 Tag 校验失败");
    wrapper.unmount();
  });

  it("renders unknown audio states safely without an archived fallback", async () => {
    const wrapper = mount(TrackStatusDisc, {
      props: { track: track({ audioStatus: "BROKEN_STATE" as TrackSummary["audioStatus"] }) },
      attachTo: document.body,
    });

    expect(wrapper.text()).toContain("未知状态");
    expect(wrapper.get("button").classes()).toContain("track-disc--error");
    await wrapper.find(".inline-flex").trigger("mouseenter");
    expect(document.body.textContent).toContain("音频状态异常");
    expect(document.body.textContent).not.toContain("曲目已归档");
    wrapper.unmount();
  });

  it("only listens for viewport changes while the tooltip is open", async () => {
    const add = vi.spyOn(window, "addEventListener");
    const remove = vi.spyOn(window, "removeEventListener");
    const wrapper = mount(TrackStatusDisc, { props: { track: track() }, attachTo: document.body });

    expect(add).not.toHaveBeenCalledWith("scroll", expect.any(Function), true);
    await wrapper.find("button").trigger("focus");
    expect(add).toHaveBeenCalledWith("resize", expect.any(Function));
    expect(add).toHaveBeenCalledWith("scroll", expect.any(Function), true);

    await wrapper.find("button").trigger("blur");
    expect(remove).toHaveBeenCalledWith("resize", expect.any(Function));
    expect(remove).toHaveBeenCalledWith("scroll", expect.any(Function), true);
    wrapper.unmount();
  });
});
