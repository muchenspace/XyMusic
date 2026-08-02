import { createApp, defineComponent, h } from "vue";
import { createPinia } from "pinia";
import { describe, expect, it, vi } from "vitest";
import type {
  DesktopLyricsController,
  DesktopLyricsControllerState,
} from "../src/application/ports/DesktopLyricsController";
import type { ApplicationServices } from "../src/application/services";
import { applicationServicesKey } from "../src/presentation/services";
import { useDesktopLyricsStore } from "../src/presentation/stores/desktopLyricsStore";

describe("desktop lyrics store", () => {
  it("projects controller state and forwards user intent", async () => {
    const controller = new FakeDesktopLyricsController();
    const services = { desktopLyricsController: controller } as unknown as ApplicationServices;
    let store!: ReturnType<typeof useDesktopLyricsStore>;
    const Root = defineComponent({
      setup() {
        store = useDesktopLyricsStore();
        return () => h("div");
      },
    });
    const app = createApp(Root);
    app.use(createPinia());
    app.provide(applicationServicesKey, services);
    const element = document.createElement("div");
    document.body.appendChild(element);
    app.mount(element);

    controller.emit({
      visible: true,
      actuallyVisible: true,
      locked: true,
      hiddenForFullscreen: false,
      fullscreenBehavior: "hide",
      fontScale: 1.2,
      textColor: "#abcdef",
      highlightColor: "#123456",
    });

    expect(store.visible).toBe(true);
    expect(store.actuallyVisible).toBe(true);
    expect(store.locked).toBe(true);
    expect(store.fullscreenBehavior).toBe("hide");
    expect(store.fontScale).toBe(1.2);

    await store.setVisible(false);
    await store.setLocked(false);
    await store.setFullscreenBehavior("show");
    store.setFontScale(1.1);
    store.setTextColor("#fedcba");
    store.setHighlightColor("#654321");

    expect(controller.setVisible).toHaveBeenCalledWith(false);
    expect(controller.setLocked).toHaveBeenCalledWith(false);
    expect(controller.setFullscreenBehavior).toHaveBeenCalledWith("show");
    expect(controller.setFontScale).toHaveBeenCalledWith(1.1);
    expect(controller.setTextColor).toHaveBeenCalledWith("#fedcba");
    expect(controller.setHighlightColor).toHaveBeenCalledWith("#654321");

    store.$dispose();
    expect(controller.unsubscribe).toHaveBeenCalledExactlyOnceWith();
    expect(controller.dispose).not.toHaveBeenCalled();

    app.unmount();
    element.remove();
  });
});

class FakeDesktopLyricsController implements DesktopLyricsController {
  private stateValue: DesktopLyricsControllerState = {
    visible: false,
    actuallyVisible: false,
    locked: false,
    hiddenForFullscreen: false,
    fullscreenBehavior: "show",
    fontScale: 1,
    textColor: "#f4f5f7",
    highlightColor: "#cf9437",
  };
  private readonly listeners = new Set<(state: DesktopLyricsControllerState) => void>();
  readonly initialize = vi.fn(async () => undefined);
  readonly setVisible = vi.fn(async () => undefined);
  readonly toggleVisible = vi.fn(async () => undefined);
  readonly setLocked = vi.fn(async () => undefined);
  readonly setFullscreenBehavior = vi.fn(async () => undefined);
  readonly setFontScale = vi.fn();
  readonly setTextColor = vi.fn();
  readonly setHighlightColor = vi.fn();
  readonly subscribePlaybackRequests = vi.fn(() => () => undefined);
  readonly sendSnapshot = vi.fn(async () => undefined);
  readonly sendClock = vi.fn(async () => undefined);
  readonly dispose = vi.fn();
  readonly unsubscribe = vi.fn();

  state(): DesktopLyricsControllerState {
    return this.stateValue;
  }

  subscribe(listener: (state: DesktopLyricsControllerState) => void): () => void {
    this.listeners.add(listener);
    listener(this.stateValue);
    return () => {
      this.listeners.delete(listener);
      this.unsubscribe();
    };
  }

  emit(state: DesktopLyricsControllerState): void {
    this.stateValue = state;
    for (const listener of this.listeners) listener(state);
  }
}
