import { flushPromises, mount } from "@vue/test-utils";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "@/shared/application/api-error";
import BatchTagScrapeDialog from "@/components/BatchTagScrapeDialog.vue";
import type { TrackSummary } from "@/features/music/domain/models";
import type { TagScrapingBatch } from "@/features/scraping/domain/models";

const scraping = vi.hoisted(() => ({
  createBatch: vi.fn(),
  batch: vi.fn(),
  cancelBatch: vi.fn(),
  retryBatch: vi.fn(),
}));

vi.mock("@/app/services/scraping", () => ({ useTagScraping: () => scraping }));

const dialogStub = {
  props: ["modelValue", "title", "description"],
  emits: ["update:modelValue"],
  template: "<section><h2>{{ title }}</h2><slot /><footer><slot name='footer' /></footer></section>",
};

function source(canWriteBack: boolean): NonNullable<TrackSummary["source"]> {
  return {
    id: `source-${canWriteBack ? "writable" : "readonly"}`,
    rootId: "root-1",
    rootName: "Music",
    relativePath: "Artist/Track.flac",
    format: "FLAC",
    status: "READY",
    checksumSha256: null,
    mode: canWriteBack ? "READ_WRITE" : "READ_ONLY",
    canWriteBack,
    writebackBlockReason: canWriteBack ? null : "音源为只读模式",
  };
}

function track(id: string, trackSource: TrackSummary["source"] = null, status: TrackSummary["status"] = "READY"): TrackSummary {
  return {
    id,
    title: `Track ${id}`,
    artistCredits: [],
    artists: ["Artist"],
    album: null,
    artwork: null,
    durationMs: 120_000,
    trackNumber: 1,
    discNumber: 1,
    status,
    audioStatus: status,
    metadataStatus: "NORMAL",
    metadataVersion: 1,
    source: trackSource,
    activeWritebackJobId: null,
    publishedAt: null,
    createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-01T00:00:00Z",
    version: 1,
  };
}

function completedBatch(): TagScrapingBatch {
  return {
    id: "batch-1",
    status: "COMPLETED",
    total: 1,
    processed: 1,
    succeeded: 0,
    skipped: 1,
    failed: 0,
    unsuccessful: 1,
    cancelRequested: false,
    items: [{
      id: "item-1",
      trackId: "track-1",
      position: 0,
      status: "SKIPPED",
      attempts: 1,
      maxAttempts: 3,
      nextAttemptAt: "2026-01-01T00:00:00Z",
      source: null,
      message: "No reliable match was found",
      candidate: null,
    }],
    createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-01T00:00:01Z",
    completedAt: "2026-01-01T00:00:01Z",
  };
}

function mountDialog(tracks: TrackSummary[]) {
  return mount(BatchTagScrapeDialog, {
    props: { modelValue: true, tracks },
    global: { stubs: { BaseDialog: dialogStub } },
  });
}

function startButton(wrapper: ReturnType<typeof mountDialog>) {
  return wrapper.get("footer").findAll("button").find((button) => button.text() === "开始刮削")!;
}

afterEach(() => vi.clearAllMocks());

describe("BatchTagScrapeDialog", () => {
  it("blocks archived tracks before creating a batch", async () => {
    const wrapper = mountDialog([track("archived", source(false), "ARCHIVED")]);
    const button = startButton(wrapper);

    expect(button.element.disabled).toBe(true);
    expect(wrapper.text()).toContain("需先恢复后才能批量刮削");
    expect(scraping.createBatch).not.toHaveBeenCalled();
  });

  it("separates condition exclusions, skipped items and failures", async () => {
    scraping.createBatch.mockResolvedValue(completedBatch());
    const wrapper = mountDialog([track("track-1"), track("2"), track("3")]);

    await wrapper.get("select[data-testid='batch-verbatim']").setValue("true");
    expect(wrapper.find("input[type='checkbox'][value='netease']").exists()).toBe(true);
    expect(wrapper.find("input[type='checkbox'][value='kugou']").exists()).toBe(true);
    await startButton(wrapper).trigger("click");
    await flushPromises();

    expect(scraping.createBatch).toHaveBeenCalledWith(expect.objectContaining({
      options: expect.objectContaining({ sources: ["qmusic", "netease", "kugou"], verbatim: true }),
    }));



    expect(wrapper.text()).toContain("已完成");
    expect(wrapper.text()).toContain("条件排除 2");
    expect(wrapper.text()).toContain("成功 0 · 已跳过 1 · 失败 0");
    expect(wrapper.text()).toContain("已跳过");
    expect(wrapper.text()).toContain("未找到可靠匹配");
    expect(wrapper.text()).toContain("#1 · Track track-1");
    expect(wrapper.text()).not.toContain("未成功");
    expect(wrapper.text()).not.toContain("SKIPPED");
    expect(wrapper.get("[data-testid='batch-item-status']").classes()).not.toContain("text-rose-700");

    await wrapper.setProps({ tracks: [] });
    expect(wrapper.text()).toContain("#1 · Track track-1");
    expect(wrapper.text()).toContain("共选择 3 首，1 首进入刮削任务");
    expect(wrapper.text()).toContain("批量刮削 3 首曲目");
  });

  it("shows an all-excluded validation response as a neutral no-op", async () => {
    scraping.createBatch.mockRejectedValue(new ApiError({
      title: "无需刮削",
      status: 422,
      code: "VALIDATION_ERROR",
      detail: "所选曲目均已包含指定字段，无需刮削",
    }));
    const wrapper = mountDialog([track("1"), track("2")]);
    await wrapper.get<HTMLInputElement>('input[value="lyrics"]').setValue(true);

    await startButton(wrapper).trigger("click");
    await flushPromises();

    expect(scraping.createBatch).toHaveBeenCalledWith(expect.objectContaining({
      options: expect.objectContaining({ missingFields: ["lyrics"] }),
    }));
    expect(wrapper.text()).toContain("无需刮削");
    expect(wrapper.text()).toContain("所选曲目均已包含指定字段，无需刮削");
    expect(wrapper.text()).toContain("条件排除 2 首");
    expect(wrapper.html()).not.toContain("bg-rose-500/10");
  });

  it("submits batches larger than 1000 without dropping expected versions", async () => {
    const tracks = Array.from({ length: 1001 }, (_, index) => track(`track-${index}`));
    scraping.createBatch.mockResolvedValue({ ...completedBatch(), total: tracks.length, processed: tracks.length });
    const wrapper = mountDialog(tracks);

    await startButton(wrapper).trigger("click");
    await flushPromises();

    const request = scraping.createBatch.mock.calls[0]?.[0];
    expect(request.items).toHaveLength(1001);
    expect(request.items.every((item: { expectedVersion: number }) => item.expectedVersion === 1)).toBe(true);
  });

  it("uses the backend initialization version for tracks without metadata rows", async () => {
    const missingMetadata = track("missing-metadata");
    missingMetadata.metadataVersion = null;
    scraping.createBatch.mockResolvedValue(completedBatch());
    const wrapper = mountDialog([missingMetadata]);

    await startButton(wrapper).trigger("click");
    await flushPromises();

    expect(scraping.createBatch).toHaveBeenCalledWith(expect.objectContaining({
      items: [{ trackId: "missing-metadata", expectedVersion: 1 }],
    }));
    expect(wrapper.text()).not.toContain("缺少 Tag 版本");
  });

  it("blocks invalid metadata versions before sending a batch request", async () => {
    const invalid = track("invalid-version");
    invalid.metadataVersion = 0;
    const wrapper = mountDialog([invalid]);

    await startButton(wrapper).trigger("click");
    await flushPromises();

    expect(scraping.createBatch).not.toHaveBeenCalled();
    expect(wrapper.text()).toContain("Tag 版本无效");
  });

  it("submits more than 5000 tracks without a client-side quantity limit", async () => {
    const tracks = Array.from({ length: 5001 }, (_, index) => track(`track-${index}`));
    scraping.createBatch.mockResolvedValue({ ...completedBatch(), total: tracks.length, processed: tracks.length });
    const wrapper = mountDialog(tracks);

    await startButton(wrapper).trigger("click");
    await flushPromises();

    const request = scraping.createBatch.mock.calls[0]?.[0];
    expect(request.items).toHaveLength(5001);
    expect(wrapper.text()).not.toContain("一次最多刮削");
  });

  it("keeps mixed writeback enabled while applying a missing-field filter", async () => {
    scraping.createBatch.mockResolvedValue(completedBatch());
    const wrapper = mountDialog([track("1", source(true)), track("2", source(false))]);
    const missingLyrics = wrapper.get<HTMLInputElement>('input[value="lyrics"]');
    let writeback = wrapper.get<HTMLInputElement>("[data-testid='batch-writeback']");
    expect(writeback.element.disabled).toBe(false);

    await missingLyrics.setValue(true);
    writeback = wrapper.get<HTMLInputElement>("[data-testid='batch-writeback']");
    expect(writeback.element.disabled).toBe(false);
    expect(wrapper.text()).toContain("仅对可写曲目创建 Tag 写回任务");
    await writeback.setValue(true);

    await missingLyrics.setValue(false);
    writeback = wrapper.get<HTMLInputElement>("[data-testid='batch-writeback']");
    expect(writeback.element.disabled).toBe(false);
    expect(writeback.element.checked).toBe(true);

    await missingLyrics.setValue(true);
    writeback = wrapper.get<HTMLInputElement>("[data-testid='batch-writeback']");
    await writeback.setValue(true);
    await startButton(wrapper).trigger("click");
    await flushPromises();

    expect(scraping.createBatch).toHaveBeenCalledWith(expect.objectContaining({
      options: expect.objectContaining({ missingFields: ["lyrics"], writeBack: true }),
    }));
  });
});
