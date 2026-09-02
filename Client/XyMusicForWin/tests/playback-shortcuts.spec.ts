import { createApp, defineComponent, h } from "vue";
import { createPinia } from "pinia";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { ApplicationServices } from "../src/application/services";
import { usePlaybackShortcuts } from "../src/presentation/composables/usePlaybackShortcuts";
import { applicationServicesKey } from "../src/presentation/services";
import { FakePlaybackSession } from "./support/FakePlaybackSession";

describe("playback shortcuts", () => {
  let app: ReturnType<typeof createApp> | undefined;
  let element: HTMLDivElement | undefined;

  afterEach(() => {
    app?.unmount();
    element?.remove();
    app = undefined;
    element = undefined;
  });

  it("toggles playback for Space regardless of the focused element", async () => {
    const session = new FakePlaybackSession({ state: { queue: [track()], currentIndex: 0 } });
    const toggle = vi.spyOn(session, "toggle");
    const targets = [document.createElement("input"), document.createElement("button"), document.createElement("div")];
    element = document.createElement("div");
    document.body.appendChild(element);

    app = createApp(defineComponent({
      setup() {
        usePlaybackShortcuts();
        return () => h("div");
      },
    }));
    app.use(createPinia());
    app.provide(applicationServicesKey, { playbackSession: session } as unknown as ApplicationServices);
    app.mount(element);
    targets.forEach((target) => element!.appendChild(target));

    for (const target of targets) {
      const event = new KeyboardEvent("keydown", { code: "Space", key: " ", bubbles: true, cancelable: true });
      target.dispatchEvent(event);
      expect(event.defaultPrevented).toBe(true);
      await Promise.resolve();
    }

    expect(toggle).toHaveBeenCalledTimes(targets.length);
  });
});

function track() {
  return {
    id: "track-1",
    title: "Track",
    artist: "Artist",
    artistIds: ["artist-1"],
    album: "Album",
    albumId: "album-1",
    coverUrl: "",
    duration: 180,
    liked: false,
    publishedAt: "2026-08-01T00:00:00.000Z",
  };
}
