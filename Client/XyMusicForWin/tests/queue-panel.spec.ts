import { mount } from "@vue/test-utils";
import { createPinia } from "pinia";
import { nextTick } from "vue";
import { describe, expect, it, vi } from "vitest";
import type { ApplicationServices } from "../src/application/services";
import type { Track } from "../src/domain/music";
import QueuePanel from "../src/presentation/components/QueuePanel.vue";
import { applicationServicesKey } from "../src/presentation/services";
import { usePlayerStore } from "../src/presentation/stores/playerStore";
import { FakePlaybackSession } from "./support/FakePlaybackSession";

describe("queue panel", () => {
  it("updates retained rows when removing a preceding queue entry", async () => {
    const first = track("first");
    const second = track("second");
    const third = track("third");
    const playbackSession = new FakePlaybackSession({ state: { queue: [first, second, third] } });
    const pinia = createPinia();
    const wrapper = mount(QueuePanel, {
      global: {
        plugins: [pinia],
        provide: { [applicationServicesKey as symbol]: { playbackSession } as unknown as ApplicationServices },
      },
    });
    const player = usePlayerStore();

    player.queueOpen = true;
    await settle();
    playbackSession.update({ queue: [second, third] });
    await settle();

    const rows = wrapper.findAll(".queue-item");
    expect(rows[0]?.attributes("aria-posinset")).toBe("1");
    expect(rows[0]?.attributes("aria-setsize")).toBe("2");

    await rows[0]!.get(".queue-item-main").trigger("click");
    await settle();
    expect(playbackSession.state().currentIndex).toBe(0);

    wrapper.unmount();
  });

  it("keeps repeated entries independently playable after a preceding entry is removed", async () => {
    const first = track("first");
    const repeated = track("repeated");
    const last = track("last");
    const playbackSession = new FakePlaybackSession({ state: { queue: [first, repeated, repeated, last] } });
    const pinia = createPinia();
    const wrapper = mount(QueuePanel, {
      global: {
        plugins: [pinia],
        provide: { [applicationServicesKey as symbol]: { playbackSession } as unknown as ApplicationServices },
      },
    });
    const player = usePlayerStore();

    player.queueOpen = true;
    await settle();
    playbackSession.update({ queue: [repeated, repeated, last] });
    await settle();

    const rows = wrapper.findAll(".queue-item");
    await rows[1]!.get(".queue-item-main").trigger("click");
    await settle();

    expect(playbackSession.state().currentIndex).toBe(1);
    wrapper.unmount();
  });

  it("does not rerender the queue shell for audio progress snapshots", async () => {
    const playbackSession = new FakePlaybackSession({ state: { queue: [track("first")], currentIndex: 0, duration: 180 } });
    const pinia = createPinia();
    const updates = vi.fn();
    const wrapper = mount(QueuePanel, {
      global: {
        plugins: [pinia],
        provide: { [applicationServicesKey as symbol]: { playbackSession } as unknown as ApplicationServices },
        mixins: [{
          updated() {
            if ((this as unknown as { $: { type: unknown } }).$.type === QueuePanel) updates();
          },
        }],
      },
    });
    const player = usePlayerStore();

    player.queueOpen = true;
    await settle();
    updates.mockClear();
    playbackSession.update({ currentTime: 60, progress: 100 / 3 });
    await settle();

    expect(updates).not.toHaveBeenCalled();
    wrapper.unmount();
  });
});

async function settle(): Promise<void> {
  await nextTick();
  await nextTick();
  await nextTick();
}

function track(id: string): Track {
  return {
    id,
    title: id,
    artist: "Artist",
    artistIds: ["artist-1"],
    album: "Album",
    coverUrl: "",
    duration: 180,
    liked: false,
    publishedAt: "2024-01-01T00:00:00.000Z",
  };
}
