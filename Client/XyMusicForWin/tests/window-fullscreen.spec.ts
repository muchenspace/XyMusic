import { createApp, defineComponent, h, nextTick, type Ref } from "vue";
import { createPinia } from "pinia";
import { describe, expect, it, vi } from "vitest";
import type { ApplicationServices } from "../src/application/services";
import { isFullscreenShortcut } from "../src/presentation/composables/useWindowFullscreen";
import { useWindowFullscreen } from "../src/presentation/composables/useWindowFullscreen";
import { applicationServicesKey } from "../src/presentation/services";

function keyboardEvent(key: string, modifiers: Partial<KeyboardEvent> = {}): KeyboardEvent {
  return {
    key,
    altKey: false,
    ctrlKey: false,
    metaKey: false,
    shiftKey: false,
    ...modifiers,
  } as KeyboardEvent;
}

describe("window fullscreen shortcuts", () => {
  it("accepts F11 and Alt+Enter", () => {
    expect(isFullscreenShortcut(keyboardEvent("F11"))).toBe(true);
    expect(isFullscreenShortcut(keyboardEvent("Enter", { altKey: true }))).toBe(true);
  });

  it("rejects modified or unrelated shortcuts", () => {
    expect(isFullscreenShortcut(keyboardEvent("F11", { ctrlKey: true }))).toBe(false);
    expect(isFullscreenShortcut(keyboardEvent("Enter"))).toBe(false);
    expect(isFullscreenShortcut(keyboardEvent("Enter", { altKey: true, shiftKey: true }))).toBe(false);
  });

  it("routes fullscreen shortcuts through the window controller projection", async () => {
    const toggleFullscreen = vi.fn(async () => undefined);
    let stateListener!: (state: { maximized: boolean; fullscreen: boolean }) => void;
    let fullscreen!: Ref<boolean>;
    const app = createApp(defineComponent({
      setup() {
        fullscreen = useWindowFullscreen();
        return () => h("div", fullscreen.value ? "fullscreen" : "windowed");
      },
    }));
    app.use(createPinia());
    app.provide(applicationServicesKey, {
      desktopWindowController: {
        state: () => ({ maximized: false, fullscreen: false }),
        subscribe: (listener: (state: { maximized: boolean; fullscreen: boolean }) => void) => {
          stateListener = listener;
          listener({ maximized: false, fullscreen: false });
          return () => undefined;
        },
        toggleFullscreen,
      },
    } as unknown as ApplicationServices);
    const element = document.createElement("div");
    document.body.appendChild(element);
    app.mount(element);
    await nextTick();

    const event = new KeyboardEvent("keydown", { key: "F11", bubbles: true, cancelable: true });
    window.dispatchEvent(event);
    await Promise.resolve();

    expect(event.defaultPrevented).toBe(true);
    expect(toggleFullscreen).toHaveBeenCalledOnce();
    stateListener({ maximized: true, fullscreen: true });
    await nextTick();
    expect(element.textContent).toBe("fullscreen");

    app.unmount();
    element.remove();
  });
});
