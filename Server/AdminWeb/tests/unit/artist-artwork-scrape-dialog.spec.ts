import { flushPromises, mount } from "@vue/test-utils";
import { afterEach, describe, expect, it, vi } from "vitest";
import ArtistArtworkScrapeDialog from "@/components/ArtistArtworkScrapeDialog.vue";
import type { ArtistSummary } from "@/features/music/domain/models";
import type { ArtistCandidate } from "@/features/scraping/domain/models";

const scraping = vi.hoisted(() => ({
  searchArtists: vi.fn(),
  applyArtistArtwork: vi.fn(),
  artworkUrl: vi.fn((url: string) => `/proxy?url=${encodeURIComponent(url)}`),
}));
const notify = vi.hoisted(() => vi.fn());

vi.mock("@/app/services/scraping", () => ({ useTagScraping: () => scraping }));
vi.mock("@/stores/ui", () => ({ useUiStore: () => ({ notify }) }));

const dialogStub = {
  props: ["modelValue", "title", "description", "preventClose"],
  emits: ["update:modelValue"],
  template: "<section><h2>{{ title }}</h2><p>{{ description }}</p><slot /><footer><slot name='footer' /></footer></section>",
};

function artist(withArtwork = false): ArtistSummary {
  return {
    id: "artist-1",
    name: "测试歌手",
    description: null,
    artwork: withArtwork ? { assetId: "asset-old", url: "/old.jpg" } : null,
    albumCount: 2,
    trackCount: 12,
    createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-01T00:00:00Z",
    version: 3,
  };
}

function candidate(): ArtistCandidate {
  return {
    id: "remote-1",
    name: "测试歌手",
    imageUrl: "https://img.example/artist.jpg",
    aliases: ["歌手别名"],
    source: "qmusic",
    score: 1.5,
  };
}

function mountDialog(withArtwork = false) {
  return mount(ArtistArtworkScrapeDialog, {
    props: { modelValue: true, artist: artist(withArtwork) },
    global: { stubs: { BaseDialog: dialogStub } },
  });
}

function button(wrapper: ReturnType<typeof mountDialog>, label: string) {
  return wrapper.findAll("button").find((item) => item.text().includes(label))!;
}

afterEach(() => {
  vi.restoreAllMocks();
  vi.clearAllMocks();
});

describe("ArtistArtworkScrapeDialog", () => {
  it("searches supported sources and applies a selected avatar", async () => {
    scraping.searchArtists.mockResolvedValue([candidate()]);
    scraping.applyArtistArtwork.mockResolvedValue({ applied: true, version: 4 });
    const confirm = vi.spyOn(window, "confirm").mockReturnValue(true);
    const wrapper = mountDialog(true);

    expect(wrapper.text()).toContain("智能多源");
    expect(wrapper.text()).toContain("QQ 音乐");
    expect(wrapper.text()).toContain("网易云");
    expect(wrapper.text()).not.toContain("咪咕");

    await button(wrapper, "搜索头像").trigger("click");
    await flushPromises();

    expect(scraping.searchArtists).toHaveBeenCalledWith({ source: "smart", query: "测试歌手", sources: ["qmusic", "netease"] }, expect.any(AbortSignal));
    expect(wrapper.text()).toContain("歌手别名");
    expect(wrapper.text()).toContain("匹配分 1.50");

    await button(wrapper, "应用所选头像").trigger("click");
    await flushPromises();

    expect(confirm).toHaveBeenCalledWith("“测试歌手”已有头像，确定使用所选候选覆盖吗？");
    expect(scraping.applyArtistArtwork).toHaveBeenCalledWith("artist-1", {
      expectedVersion: 3,
      candidate: candidate(),
      overwrite: true,
      reason: "在线刮削艺术家头像",
    });
    expect(wrapper.emitted("applied")).toEqual([[4]]);
    expect(notify).toHaveBeenCalledWith("success", "艺术家头像已更新");
  });

  it("presents an empty search as a neutral skip without applying", async () => {
    scraping.searchArtists.mockResolvedValue([]);
    const wrapper = mountDialog();

    await button(wrapper, "搜索头像").trigger("click");
    await flushPromises();

    expect(wrapper.text()).toContain("未找到可靠候选，已跳过");
    expect(wrapper.text()).toContain("当前头像未作任何修改");
    expect(wrapper.html()).not.toContain("bg-rose-500/10");
    expect(scraping.applyArtistArtwork).not.toHaveBeenCalled();
  });

  it("does not submit an overwrite when the operator rejects confirmation", async () => {
    scraping.searchArtists.mockResolvedValue([candidate()]);
    vi.spyOn(window, "confirm").mockReturnValue(false);
    const wrapper = mountDialog(true);

    await button(wrapper, "搜索头像").trigger("click");
    await flushPromises();
    await button(wrapper, "应用所选头像").trigger("click");

    expect(scraping.applyArtistArtwork).not.toHaveBeenCalled();
  });
});
