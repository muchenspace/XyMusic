import { flushPromises, mount } from "@vue/test-utils";
import { afterEach, describe, expect, it, vi } from "vitest";
import BatchArtistArtworkScrapeDialog from "@/components/BatchArtistArtworkScrapeDialog.vue";
import type { ArtistSummary } from "@/features/music/domain/models";
import type { ArtistArtworkBatch } from "@/features/scraping/domain/models";

const scraping = vi.hoisted(() => ({
  createArtistArtworkBatch: vi.fn(),
  artistArtworkBatch: vi.fn(),
  cancelArtistArtworkBatch: vi.fn(),
  retryArtistArtworkBatch: vi.fn(),
}));

vi.mock("@/app/services/scraping", () => ({ useTagScraping: () => scraping }));

const dialogStub = {
  props: ["modelValue", "title", "description"],
  emits: ["update:modelValue"],
  template: "<section><h2>{{ title }}</h2><p>{{ description }}</p><slot /><footer><slot name='footer' /></footer></section>",
};

function artist(id: string, withArtwork = false, version = 1): ArtistSummary {
  return {
    id,
    name: `Artist ${id}`,
    description: null,
    artwork: withArtwork ? { assetId: `asset-${id}`, url: `/${id}.jpg` } : null,
    albumCount: 1,
    trackCount: 1,
    createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-01T00:00:00Z",
    version,
  };
}

function completedBatch(): ArtistArtworkBatch {
  return {
    id: "artist-batch-1",
    requestedBy: "admin-1",
    options: { sources: ["qmusic", "netease"], overwrite: false, reason: "批量在线刮削艺术家头像" },
    status: "COMPLETED",
    total: 1,
    processed: 1,
    succeeded: 0,
    failed: 0,
    skipped: 1,
    cancelRequested: false,
    startedAt: "2026-01-01T00:00:00Z",
    completedAt: "2026-01-01T00:00:01Z",
    createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-01T00:00:01Z",
    items: [{
      id: "item-1",
      jobId: "artist-batch-1",
      artistId: "missing-1",
      expectedVersion: 2,
      position: 0,
      status: "SKIPPED",
      candidate: null,
      source: null,
      message: "No reliable artist artwork match was found",
      attempts: 1,
      nextAttemptAt: null,
      startedAt: "2026-01-01T00:00:00Z",
      completedAt: "2026-01-01T00:00:01Z",
      createdAt: "2026-01-01T00:00:00Z",
      updatedAt: "2026-01-01T00:00:01Z",
    }],
  };
}

function mountDialog(artists: ArtistSummary[]) {
  return mount(BatchArtistArtworkScrapeDialog, {
    props: { modelValue: true, artists },
    global: { stubs: { BaseDialog: dialogStub } },
  });
}

function startButton(wrapper: ReturnType<typeof mountDialog>) {
  return wrapper.findAll("button").find((item) => item.text() === "开始刮削")!;
}

afterEach(() => vi.clearAllMocks());

describe("BatchArtistArtworkScrapeDialog", () => {
  it("excludes existing artwork before creating the batch and keeps skips neutral", async () => {
    scraping.createArtistArtworkBatch.mockResolvedValue({
      job: completedBatch(),
      selected: 2,
      conditionExcluded: 1,
    });
    const artists = [artist("existing", true), artist("missing-1", false, 2), artist("missing-2", false, 4)];
    const wrapper = mountDialog(artists);

    expect(wrapper.text()).toContain("QQ 音乐");
    expect(wrapper.text()).toContain("网易云");
    expect(wrapper.text()).not.toContain("咪咕");
    await startButton(wrapper).trigger("click");
    await flushPromises();

    expect(scraping.createArtistArtworkBatch).toHaveBeenCalledWith({
      items: [
        { artistId: "missing-1", expectedVersion: 2 },
        { artistId: "missing-2", expectedVersion: 4 },
      ],
      options: { sources: ["qmusic", "netease"], overwrite: false, reason: "批量在线刮削艺术家头像" },
    });
    expect(wrapper.text()).toContain("条件排除 2");
    expect(wrapper.text()).toContain("成功 0 · 已跳过 1 · 失败 0");
    expect(wrapper.text()).toContain("未找到可靠头像");
    expect(wrapper.get("[data-testid='artist-batch-item-status']").classes()).not.toContain("text-rose-700");
    expect(wrapper.emitted("completed")).toEqual([[]]);
  });

  it("does not create a task when every selected artist already has artwork", async () => {
    const wrapper = mountDialog([artist("one", true), artist("two", true)]);

    await startButton(wrapper).trigger("click");
    await flushPromises();

    expect(scraping.createArtistArtworkBatch).not.toHaveBeenCalled();
    expect(wrapper.text()).toContain("所选艺术家均已有头像，无需创建刮削任务");
    expect(wrapper.text()).toContain("条件排除 2 位");
    expect(wrapper.html()).not.toContain("bg-rose-500/10");
  });

  it("keeps backend all-excluded results as a neutral no-op", async () => {
    scraping.createArtistArtworkBatch.mockResolvedValue({ job: null, selected: 1, conditionExcluded: 1 });
    const wrapper = mountDialog([artist("missing")]);

    await startButton(wrapper).trigger("click");
    await flushPromises();

    expect(wrapper.text()).toContain("符合条件的艺术家在任务创建前已被排除，无需刮削");
    expect(wrapper.text()).toContain("条件排除 1 位");
    expect(wrapper.html()).not.toContain("bg-rose-500/10");
  });
});
