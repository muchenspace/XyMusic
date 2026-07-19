import { mount } from "@vue/test-utils";
import { nextTick, ref } from "vue";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { TrackMetadataRecord, TrackSummary } from "@/features/music/domain/models";
import {
  assertWritebackAllowed,
  batchWritebackCapability,
  batchWritebackHint,
  sourceWritebackCapability,
  useWritebackSelection,
} from "@/features/music/presentation/writeback-capability";

const scraping = vi.hoisted(() => ({
  search: vi.fn(),
  fingerprint: vi.fn(),
  apply: vi.fn(),
  createBatch: vi.fn(),
  batch: vi.fn(),
  cancelBatch: vi.fn(),
  retryBatch: vi.fn(),
  artworkUrl: vi.fn((url: string) => url),
}));

vi.mock("@/app/services/scraping", () => ({ useTagScraping: () => scraping }));
vi.mock("@/stores/ui", () => ({ useUiStore: () => ({ notify: vi.fn() }) }));

import BatchTagScrapeDialog from "@/components/BatchTagScrapeDialog.vue";
import TagScrapeDialog from "@/components/TagScrapeDialog.vue";

const dialogStub = {
  props: ["modelValue", "title", "description"],
  emits: ["update:modelValue"],
  template: "<section><h2>{{ title }}</h2><slot /><footer><slot name='footer' /></footer></section>",
};

function summarySource(canWriteBack: boolean, writebackBlockReason: string | null, mode: "READ_ONLY" | "READ_WRITE" = "READ_WRITE"): NonNullable<TrackSummary["source"]> {
  return {
    id: "asset-1",
    rootId: "root-1",
    rootName: "Music",
    relativePath: "Artist/Track.flac",
    format: "FLAC",
    status: "READY",
    checksumSha256: null,
    mode,
    canWriteBack,
    writebackBlockReason,
  };
}

function metadataSource(canWriteBack: boolean, writebackBlockReason: string | null): NonNullable<TrackMetadataRecord["source"]> {
  return {
    id: "asset-1",
    rootId: "root-1",
    relativePath: "Artist/Track.flac",
    status: "READY",
    checksumSha256: null,
    mode: canWriteBack ? "READ_WRITE" : "READ_ONLY",
    canWriteBack,
    writebackBlockReason,
  };
}

function track(id: string, source: TrackSummary["source"], status: TrackSummary["status"] = "READY"): TrackSummary {
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
    metadataStatus: "ORIGINAL",
    metadataVersion: 1,
    source,
    mediaProcessing: null,
    variantSummary: [],
    activeWritebackJobId: null,
    publishedAt: null,
    createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-01T00:00:00Z",
    version: 1,
  };
}

afterEach(() => vi.clearAllMocks());

describe("writeback capability", () => {
  it("treats READ_ONLY and missing sources as non-writable", () => {
    const readOnly = sourceWritebackCapability(summarySource(false, "Library root is read-only", "READ_ONLY"));
    expect(readOnly).toEqual({ canWriteBack: false, blockReason: "Library root is read-only" });
    expect(sourceWritebackCapability(null)).toEqual({ canWriteBack: false, blockReason: "无可写本地源" });
    expect(() => assertWritebackAllowed(true, readOnly)).toThrow("Library root is read-only");
  });

  it("blocks a mixed batch and groups the unavailable reasons", () => {
    const capability = batchWritebackCapability([
      track("writable", summarySource(true, null)),
      track("readonly", summarySource(false, "Library root is read-only", "READ_ONLY")),
      track("missing", null),
    ]);

    expect(capability.canWriteBack).toBe(false);
    expect(capability.blockedCount).toBe(2);
    expect(batchWritebackHint(capability)).toContain("其中 2 首不能写回");
    expect(batchWritebackHint(capability)).toContain("Library root is read-only（1 首）");
    expect(batchWritebackHint(capability)).toContain("无可写本地源（1 首）");
  });

  it("clears a selected writeback option when capability changes", async () => {
    const capability = ref(sourceWritebackCapability(summarySource(true, null)));
    const selected = useWritebackSelection(capability);
    selected.value = true;

    capability.value = sourceWritebackCapability(summarySource(false, "Library root is read-only", "READ_ONLY"));
    await nextTick();

    expect(selected.value).toBe(false);
  });
});

describe("writeback controls", () => {
  it("disables single-track scraping controls for an archived track", () => {
    const wrapper = mount(TagScrapeDialog, {
      props: {
        modelValue: true,
        track: track("archived", summarySource(false, "曲目已归档"), "ARCHIVED"),
        expectedVersion: 1,
        writebackSource: metadataSource(false, "曲目已归档"),
      },
      global: { stubs: { BaseDialog: dialogStub } },
    });

    const buttons = wrapper.findAll("button");
    expect(buttons.find((button) => button.text() === "搜索")?.element.disabled).toBe(true);
    expect(buttons.find((button) => button.text() === "音频指纹")?.element.disabled).toBe(true);
    expect(buttons.find((button) => button.text() === "应用候选")?.element.disabled).toBe(true);
    expect(scraping.search).not.toHaveBeenCalled();
    expect(scraping.fingerprint).not.toHaveBeenCalled();
    expect(scraping.apply).not.toHaveBeenCalled();
  });

  it("disables and clears batch writeback for a mixed selection", async () => {
    const writable = track("writable", summarySource(true, null));
    const wrapper = mount(BatchTagScrapeDialog, {
      props: { modelValue: true, tracks: [writable] },
      global: { stubs: { BaseDialog: dialogStub } },
    });
    const checkbox = wrapper.get<HTMLInputElement>("[data-testid='batch-writeback']");
    await checkbox.setValue(true);
    expect(checkbox.element.checked).toBe(true);

    await wrapper.setProps({ tracks: [writable, track("readonly", summarySource(false, "Library root is read-only", "READ_ONLY"))] });

    const updated = wrapper.get<HTMLInputElement>("[data-testid='batch-writeback']");
    expect(updated.element.disabled).toBe(true);
    expect(updated.element.checked).toBe(false);
    expect(wrapper.text()).toContain("其中 1 首不能写回");
    expect(wrapper.text()).toContain("Library root is read-only");
  });

  it("disables and clears single-track scraping writeback when capability changes", async () => {
    const wrapper = mount(TagScrapeDialog, {
      props: {
        modelValue: true,
        track: track("track-1", summarySource(true, null)),
        expectedVersion: 1,
        writebackSource: metadataSource(true, null),
      },
      global: { stubs: { BaseDialog: dialogStub } },
    });
    const checkbox = wrapper.get<HTMLInputElement>("[data-testid='tag-writeback']");
    await checkbox.setValue(true);
    expect(checkbox.element.checked).toBe(true);

    await wrapper.setProps({ writebackSource: metadataSource(false, "Library root is read-only") });

    const updated = wrapper.get<HTMLInputElement>("[data-testid='tag-writeback']");
    expect(updated.element.disabled).toBe(true);
    expect(updated.element.checked).toBe(false);
    expect(wrapper.text()).toContain("Library root is read-only");
  });
});
