import { createApp, defineComponent, h, nextTick, ref } from "vue";
import { createPinia } from "pinia";
import { describe, expect, it, vi } from "vitest";
import type { ApplicationServices } from "../src/application/services";
import type { Album, Track } from "../src/domain/music";
import { useNavigationActions, type ScrollContainer } from "../src/presentation/composables/useNavigationActions";
import { applicationServicesKey } from "../src/presentation/services";

describe("navigation actions", () => {
  it("does not restore a stale detail page scroll position after a newer navigation", async () => {
    const requests: Array<ReturnType<typeof deferred<{ items: Track[]; nextCursor: string | null }>>> = [];
    const albumTracksPage = vi.fn(() => {
      const request = deferred<{ items: Track[]; nextCursor: string | null }>();
      requests.push(request);
      return request.promise;
    });
    const scrollElement = document.createElement("div");
    const shell = ref<ScrollContainer | null>({ scrollElement: () => scrollElement });
    let actions!: ReturnType<typeof useNavigationActions>;
    const app = createApp(defineComponent({
      setup() {
        actions = useNavigationActions(shell);
        return () => h("div");
      },
    }));
    app.use(createPinia());
    app.provide(applicationServicesKey, {
      catalog: { albumTracksPage },
      library: {},
      playlists: {},
    } as unknown as ApplicationServices);
    const element = document.createElement("div");
    document.body.appendChild(element);
    app.mount(element);

    const first = actions.openAlbum(album("album-one"));
    expect(requests).toHaveLength(1);
    scrollElement.scrollTop = 75;

    const second = actions.openAlbum(album("album-two"));
    expect(requests).toHaveLength(2);
    requests[1]!.resolve({ items: [], nextCursor: null });
    await second;
    expect(scrollElement.scrollTop).toBe(0);

    requests[0]!.resolve({ items: [], nextCursor: null });
    await first;
    await nextTick();
    expect(scrollElement.scrollTop).toBe(0);

    app.unmount();
    element.remove();
  });
});

function album(id: string): Album {
  return {
    id,
    title: id,
    artist: "Artist",
    artistIds: ["artist-1"],
    coverUrl: "",
    trackCount: 1,
    accent: "#000000",
  };
}

function deferred<T>() {
  let resolve!: (value: T | PromiseLike<T>) => void;
  const promise = new Promise<T>((resolvePromise) => { resolve = resolvePromise; });
  return { promise, resolve };
}
