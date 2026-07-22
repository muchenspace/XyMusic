import { flushPromises, mount } from "@vue/test-utils";
import { afterEach, describe, expect, it, vi } from "vitest";
import TagCandidateDetailDialog from "@/components/TagCandidateDetailDialog.vue";
import type { TagCandidate, TagCandidateDetail } from "@/features/scraping/domain/models";

const scraping = vi.hoisted(() => ({
  candidateDetail: vi.fn(),
  artworkUrl: vi.fn((url: string) => `/proxy?url=${encodeURIComponent(url)}`),
}));

vi.mock("@/app/services/scraping", () => ({ useTagScraping: () => scraping }));

const dialogStub = {
  props: ["modelValue", "title", "description", "width"],
  emits: ["update:modelValue"],
  template: "<section v-if='modelValue'><h2>{{ title }}</h2><p>{{ description }}</p><slot /><footer><slot name='footer' /></footer></section>",
};

function candidate(id = "remote-1", name = "候选歌曲"): TagCandidate {
  return {
    id,
    name,
    artist: "候选歌手",
    artistId: `artist-${id}`,
    album: "候选专辑",
    albumId: `album-${id}`,
    albumImg: `https://img.example/${id}.jpg`,
    year: "2026",
    track: "2/10",
    disc: "1/2",
    genre: "Pop",
    source: "qmusic",
    titleScore: 0.98,
    artistScore: 0.97,
    albumScore: 0.96,
    score: 0.95,
  };
}

function detail(item: TagCandidate, content = "[00:01.00]第一行\n[00:04.00]第二行"): TagCandidateDetail {
  return {
    candidate: item,
    lyrics: { content, format: "LRC", language: "und" },
  };
}

function mountDialog(item: TagCandidate, selected = false) {
  return mount(TagCandidateDetailDialog, {
    props: { modelValue: true, candidate: item, selected },
    global: { stubs: { BaseDialog: dialogStub } },
  });
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise;
    reject = rejectPromise;
  });
  return { promise, resolve, reject };
}

function button(wrapper: ReturnType<typeof mountDialog>, label: string) {
  return wrapper.findAll("button").find((item) => item.text().includes(label))!;
}

function requestSignal(index = 0): AbortSignal {
  const call = scraping.candidateDetail.mock.calls[index];
  if (!call) throw new Error(`Missing candidate detail request ${index}`);
  return call[1] as AbortSignal;
}

afterEach(() => {
  vi.clearAllMocks();
});

describe("TagCandidateDetailDialog", () => {
  it("loads lyrics on demand and renders the complete candidate metadata", async () => {
    const item = candidate();
    scraping.candidateDetail.mockResolvedValue(detail(item));
    const wrapper = mountDialog(item);

    await flushPromises();

    expect(scraping.candidateDetail).toHaveBeenCalledWith({ candidate: item }, expect.any(AbortSignal));
    expect(scraping.artworkUrl).toHaveBeenCalledWith(item.albumImg);
    expect(wrapper.text()).toContain("候选歌曲");
    expect(wrapper.text()).toContain("候选歌手");
    expect(wrapper.text()).toContain("候选专辑");
    expect(wrapper.text()).toContain("2/10");
    expect(wrapper.text()).toContain("1/2");
    expect(wrapper.text()).toContain("artist-remote-1");
    expect(wrapper.text()).toContain("album-remote-1");
    expect(wrapper.text()).toContain("0.98");
    expect(wrapper.get("[data-testid='candidate-lyrics']").element.textContent).toBe("[00:01.00]第一行\n[00:04.00]第二行");

    await button(wrapper, "选用此候选").trigger("click");

    expect(wrapper.emitted("select")).toEqual([[item]]);
    expect(wrapper.emitted("update:modelValue")).toEqual([[false]]);
  });

  it("shows a retryable error and then a neutral empty lyrics state", async () => {
    const item = candidate();
    scraping.candidateDetail
      .mockRejectedValueOnce(new Error("上游歌词服务超时"))
      .mockResolvedValueOnce({ candidate: item, lyrics: null });
    const wrapper = mountDialog(item);

    await flushPromises();
    expect(wrapper.text()).toContain("歌词加载失败");
    expect(wrapper.text()).toContain("上游歌词服务超时");

    await button(wrapper, "重新加载").trigger("click");
    await flushPromises();

    expect(scraping.candidateDetail).toHaveBeenCalledTimes(2);
    expect(wrapper.text()).toContain("未找到歌词");
    expect(wrapper.text()).toContain("该来源未返回可用于预览的歌词");
  });

  it("aborts the previous request and ignores its result when the candidate changes", async () => {
    const first = candidate("remote-1", "第一候选");
    const second = candidate("remote-2", "第二候选");
    const firstRequest = deferred<TagCandidateDetail>();
    const secondRequest = deferred<TagCandidateDetail>();
    scraping.candidateDetail.mockImplementation(({ candidate: requested }: { candidate: TagCandidate }) => (
      requested.id === first.id ? firstRequest.promise : secondRequest.promise
    ));
    const wrapper = mountDialog(first);
    const firstSignal = requestSignal();

    expect(wrapper.text()).toContain("正在获取歌词");
    await wrapper.setProps({ candidate: second });

    expect(firstSignal.aborted).toBe(true);
    firstRequest.resolve(detail(first, "第一候选歌词"));
    secondRequest.resolve(detail(second, "第二候选歌词"));
    await flushPromises();

    expect(wrapper.text()).toContain("第二候选歌词");
    expect(wrapper.text()).not.toContain("第一候选歌词");
  });

  it("aborts an in-flight request when the dialog closes", async () => {
    const request = deferred<TagCandidateDetail>();
    scraping.candidateDetail.mockReturnValue(request.promise);
    const wrapper = mountDialog(candidate());
    const signal = requestSignal();

    await wrapper.setProps({ modelValue: false });

    expect(signal.aborted).toBe(true);
  });
});
