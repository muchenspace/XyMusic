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

  it("does not intercept Space on text/form inputs, menus, or modal dialogs", async () => {
    const session = new FakePlaybackSession({ state: { queue: [track()], currentIndex: 0 } });
    const toggle = vi.spyOn(session, "toggle");
    mountShortcuts(session);

    const searchInput = document.createElement("input");
    searchInput.type = "search";
    const textInput = document.createElement("input");
    const checkbox = document.createElement("input");
    checkbox.type = "checkbox";
    const textarea = document.createElement("textarea");
    const select = document.createElement("select");
    const contentEditable = document.createElement("div");
    contentEditable.setAttribute("contenteditable", "true");
    const roleTextbox = document.createElement("div");
    roleTextbox.setAttribute("role", "textbox");
    const menuItem = document.createElement("button");
    menuItem.setAttribute("role", "menuitem");
    const modal = document.createElement("section");
    modal.className = "app-dialog";
    const modalButton = document.createElement("button");
    modal.appendChild(modalButton);

    for (const node of [searchInput, textInput, checkbox, textarea, select, contentEditable, roleTextbox, menuItem, modal]) {
      element!.appendChild(node);
    }

    for (const target of [searchInput, textInput, checkbox, textarea, select, contentEditable, roleTextbox, menuItem, modalButton]) {
      const event = new KeyboardEvent("keydown", { code: "Space", key: " ", bubbles: true, cancelable: true });
      target.dispatchEvent(event);
      expect(event.defaultPrevented).toBe(false);
      await Promise.resolve();
    }

    expect(toggle).not.toHaveBeenCalled();
  });

  it("toggles playback on Space when focus rests on a mouse-clicked button, range slider, or page surface", async () => {
    const session = new FakePlaybackSession({ state: { queue: [track()], currentIndex: 0 } });
    const toggle = vi.spyOn(session, "toggle");
    mountShortcuts(session);

    const button = document.createElement("button");
    const range = document.createElement("input");
    range.type = "range";
    const lyricsOverlay = document.createElement("section");
    lyricsOverlay.className = "lyrics-view";
    lyricsOverlay.setAttribute("role", "dialog");
    element!.append(button, range, lyricsOverlay);

    button.dispatchEvent(new MouseEvent("mousedown", { bubbles: true }));
    button.focus();

    for (const target of [button, range, lyricsOverlay, document.body]) {
      const event = new KeyboardEvent("keydown", { code: "Space", key: " ", bubbles: true, cancelable: true });
      target.dispatchEvent(event);
      expect(event.defaultPrevented).toBe(true);
      await Promise.resolve();
    }

    expect(toggle).toHaveBeenCalledTimes(4);
  });

  it("preserves Space button activation when focus arrived via keyboard Tab navigation", async () => {
    const session = new FakePlaybackSession({ state: { queue: [track()], currentIndex: 0 } });
    const toggle = vi.spyOn(session, "toggle");
    mountShortcuts(session);

    const button = document.createElement("button");
    element!.appendChild(button);

    document.body.dispatchEvent(new KeyboardEvent("keydown", { key: "Tab", code: "Tab", bubbles: true }));
    button.focus();

    const tabSpace = new KeyboardEvent("keydown", { code: "Space", key: " ", bubbles: true, cancelable: true });
    button.dispatchEvent(tabSpace);
    expect(tabSpace.defaultPrevented).toBe(false);
    expect(toggle).not.toHaveBeenCalled();

    // Subsequent mouse click clears keyboard focus mode so Space toggles playback again.
    button.dispatchEvent(new MouseEvent("mousedown", { bubbles: true }));
    button.focus();
    const pointerSpace = new KeyboardEvent("keydown", { code: "Space", key: " ", bubbles: true, cancelable: true });
    button.dispatchEvent(pointerSpace);
    expect(pointerSpace.defaultPrevented).toBe(true);
    await Promise.resolve();
    expect(toggle).toHaveBeenCalledTimes(1);
  });

  it("prevents repeat Space keydowns from activating pointer-focused buttons without re-toggling playback", async () => {
    const session = new FakePlaybackSession({ state: { queue: [track()], currentIndex: 0 } });
    const toggle = vi.spyOn(session, "toggle");
    mountShortcuts(session);

    const button = document.createElement("button");
    element!.appendChild(button);
    button.dispatchEvent(new MouseEvent("mousedown", { bubbles: true }));
    button.focus();

    const repeatEvent = new KeyboardEvent("keydown", {
      code: "Space",
      key: " ",
      repeat: true,
      bubbles: true,
      cancelable: true,
    });
    button.dispatchEvent(repeatEvent);
    expect(repeatEvent.defaultPrevented).toBe(true);

    session.reset();
    const emptyQueueEvent = new KeyboardEvent("keydown", {
      code: "Space",
      key: " ",
      bubbles: true,
      cancelable: true,
    });
    button.dispatchEvent(emptyQueueEvent);
    expect(emptyQueueEvent.defaultPrevented).toBe(false);

    await Promise.resolve();
    expect(toggle).not.toHaveBeenCalled();
  });

  function mountShortcuts(session: FakePlaybackSession): void {
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
  }
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
