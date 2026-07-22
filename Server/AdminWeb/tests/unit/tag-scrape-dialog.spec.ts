import { flushPromises, mount } from "@vue/test-utils";
import { afterEach, describe, expect, it, vi } from "vitest";
import TagScrapeDialog from "@/components/TagScrapeDialog.vue";
import type { TrackSummary } from "@/features/music/domain/models";
import type { TagCandidate, TagCandidateDetail } from "@/features/scraping/domain/models";

const scraping = vi.hoisted(() => ({
  search: vi.fn(),
  candidateDetail: vi.fn(),
  fingerprint: vi.fn(),
  apply: vi.fn(),
  artworkUrl: vi.fn((url: string) => `/proxy?url=${encodeURIComponent(url)}`),
}));

vi.mock("@/app/services/scraping", () => ({ useTagScraping: () => scraping }));
vi.mock("@/stores/ui", () => ({ useUiStore: () => ({ notify: vi.fn() }) }));

const dialogStub = {
  props: ["modelValue", "title", "description", "width"],
  emits: ["update:modelValue"],
  template: "<section v-if='modelValue' :data-dialog-title='title'><h2>{{ title }}</h2><p>{{ description }}</p><slot /><footer><slot name='footer' /></footer></section>",
};

function track(): TrackSummary {
  return {
    id: "track-1",
    title: "本地歌曲",
    artistCredits: [],
    artists: ["本地歌手"],
    album: { id: "album-local", title: "本地专辑" },
    artwork: null,
    durationMs: 180_000,
    trackNumber: 1,
    discNumber: 1,
    status: "READY",
    audioStatus: "READY",
    metadataStatus: "ORIGINAL",
    metadataVersion: 3,
    source: null,
    mediaProcessing: null,
    variantSummary: [],
    activeWritebackJobId: null,
    publishedAt: null,
    createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-01T00:00:00Z",
    version: 3,
  };
}

function candidate(id: string, name: string): TagCandidate {
  return {
    id,
    name,
    artist: `歌手 ${id}`,
    artistId: `artist-${id}`,
    album: `专辑 ${id}`,
    albumId: `album-${id}`,
    albumImg: `https://img.example/${id}.jpg`,
    year: "2026",
    track: "1/10",
    disc: "1/1",
    genre: "Pop",
    source: "qmusic",
    score: 0.9,
  };
}

function detail(item: TagCandidate): TagCandidateDetail {
  return {
    candidate: item,
    lyrics: { content: `${item.name} 的歌词`, format: "PLAIN", language: "und" },
  };
}

function mountDialog() {
  return mount(TagScrapeDialog, {
    props: { modelValue: true, track: track(), expectedVersion: 3 },
    global: { stubs: { BaseDialog: dialogStub } },
  });
}

function button(wrapper: ReturnType<typeof mountDialog>, label: string) {
  return wrapper.findAll("button").find((item) => item.text() === label)!;
}

function candidateCard(wrapper: ReturnType<typeof mountDialog>, index: number) {
  const card = wrapper.findAll("[data-testid='tag-candidate']")[index];
  if (!card) throw new Error(`Missing candidate card ${index}`);
  return card;
}

function detailRequestSignal(index = 0): AbortSignal {
  const call = scraping.candidateDetail.mock.calls[index];
  if (!call) throw new Error(`Missing candidate detail request ${index}`);
  return call[1] as AbortSignal;
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((resolvePromise) => {
    resolve = resolvePromise;
  });
  return { promise, resolve };
}

afterEach(() => {
  vi.clearAllMocks();
});

describe("TagScrapeDialog candidate details", () => {
  it("opens details without changing selection and only selects from an explicit action", async () => {
    const first = candidate("remote-1", "第一候选");
    const second = candidate("remote-2", "第二候选");
    scraping.search.mockResolvedValue([first, second]);
    scraping.candidateDetail.mockResolvedValue(detail(second));
    const wrapper = mountDialog();

    await wrapper.get("input[placeholder='歌曲、艺术家或专辑']").setValue("候选歌曲");
    await button(wrapper, "搜索").trigger("click");
    await flushPromises();

    let cards = wrapper.findAll("[data-testid='tag-candidate']");
    expect(cards).toHaveLength(2);
    expect(candidateCard(wrapper, 0).text()).toContain("已选用");
    expect(candidateCard(wrapper, 1).text()).not.toContain("已选用");

    await candidateCard(wrapper, 1).findAll("button").find((item) => item.text() === "查看详情")!.trigger("click");
    await flushPromises();

    expect(scraping.candidateDetail).toHaveBeenCalledWith({ candidate: second }, expect.any(AbortSignal));
    cards = wrapper.findAll("[data-testid='tag-candidate']");
    expect(cards).toHaveLength(2);
    expect(candidateCard(wrapper, 0).text()).toContain("已选用");
    expect(candidateCard(wrapper, 1).text()).not.toContain("已选用");
    expect(wrapper.text()).toContain("第二候选 的歌词");

    await button(wrapper, "选用此候选").trigger("click");
    await flushPromises();

    cards = wrapper.findAll("[data-testid='tag-candidate']");
    expect(cards).toHaveLength(2);
    expect(candidateCard(wrapper, 0).text()).not.toContain("已选用");
    expect(candidateCard(wrapper, 1).text()).toContain("已选用");
    expect(wrapper.find("[data-dialog-title='第二候选']").exists()).toBe(false);
  });

  it("closes the detail view and aborts its request when a new search starts", async () => {
    const first = candidate("remote-1", "第一候选");
    const second = candidate("remote-2", "第二候选");
    const detailRequest = deferred<TagCandidateDetail>();
    scraping.search.mockResolvedValueOnce([first, second]).mockResolvedValueOnce([first]);
    scraping.candidateDetail.mockReturnValue(detailRequest.promise);
    const wrapper = mountDialog();

    await wrapper.get("input[placeholder='歌曲、艺术家或专辑']").setValue("候选歌曲");
    await button(wrapper, "搜索").trigger("click");
    await flushPromises();
    const secondCard = candidateCard(wrapper, 1);
    await secondCard.findAll("button").find((item) => item.text() === "查看详情")!.trigger("click");
    const signal = detailRequestSignal();

    await button(wrapper, "搜索").trigger("click");
    await flushPromises();

    expect(signal.aborted).toBe(true);
    expect(wrapper.find("[data-dialog-title='第二候选']").exists()).toBe(false);
    expect(wrapper.findAll("[data-testid='tag-candidate']")).toHaveLength(1);
  });
});
