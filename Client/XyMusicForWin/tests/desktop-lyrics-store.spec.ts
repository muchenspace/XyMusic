import { createApp, defineComponent, h } from "vue";
import { createPinia } from "pinia";
import { describe, expect, it, vi } from "vitest";
import type { ApplicationServices } from "../src/application/services";
import type { DesktopLyricsWindowState } from "../src/application/ports/DesktopLyrics";
import { applicationServicesKey } from "../src/presentation/services";
import { useDesktopLyricsStore } from "../src/presentation/stores/desktopLyricsStore";

describe("desktop lyrics store", () => {
  it("applies persisted visibility and lock choices without overwriting them with native defaults", async () => {
    let nativeState: DesktopLyricsWindowState = {
      requestedVisible: false,
      visible: false,
      locked: false,
      hiddenForFullscreen: false,
      fullscreenBehavior: "show",
    };
    let stateListener: ((state: DesktopLyricsWindowState) => void) | undefined;
    const setVisible = vi.fn(async (visible: boolean) => {
      nativeState = { ...nativeState, requestedVisible: visible, visible };
      return nativeState;
    });
    const setLocked = vi.fn(async (locked: boolean) => {
      nativeState = { ...nativeState, locked };
      return nativeState;
    });
    const setFullscreenBehavior = vi.fn(async (fullscreenBehavior: "show" | "hide") => {
      nativeState = { ...nativeState, fullscreenBehavior };
      return nativeState;
    });
    const writeVisible = vi.fn();
    const writeLocked = vi.fn();
    const writeFullscreenBehavior = vi.fn();
    const services = {
      desktopLyrics: {
        getWindowState: vi.fn(async () => nativeState),
        setVisible,
        toggleVisible: vi.fn(async () => nativeState),
        setLocked,
        setFullscreenBehavior,
        sendSnapshot: vi.fn(async () => undefined),
        sendClock: vi.fn(async () => undefined),
        onAction: vi.fn(async () => () => undefined),
        onWindowState: vi.fn(async (listener: (state: DesktopLyricsWindowState) => void) => {
          stateListener = listener;
          return () => undefined;
        }),
      },
      uiPreferences: {
        readDesktopLyrics: () => ({
          visible: true,
          locked: true,
          fullscreenBehavior: "hide" as const,
          fontScale: 1.2,
          textColor: "#abcdef",
          highlightColor: "#123456",
          wordLyricsEnabled: true,
        }),
        writeDesktopLyricsVisible: writeVisible,
        writeDesktopLyricsLocked: writeLocked,
        writeDesktopLyricsFullscreenBehavior: writeFullscreenBehavior,
        writeDesktopLyricsWordLyricsEnabled() {},
      },
    } as unknown as ApplicationServices;
    let store!: ReturnType<typeof useDesktopLyricsStore>;
    let initialized!: Promise<void>;
    const Root = defineComponent({
      setup() {
        store = useDesktopLyricsStore();
        initialized = store.initialize();
        return () => h("div");
      },
    });
    const app = createApp(Root);
    app.use(createPinia());
    app.provide(applicationServicesKey, services);
    const element = document.createElement("div");
    document.body.appendChild(element);
    app.mount(element);

    await initialized;

    expect(setFullscreenBehavior).toHaveBeenCalledWith("hide");
    expect(setLocked).toHaveBeenCalledWith(true);
    expect(setVisible).toHaveBeenCalledWith(true);
    expect(store.visible).toBe(true);
    expect(store.locked).toBe(true);
    expect(store.fullscreenBehavior).toBe("hide");
    expect(store.fontScale).toBe(1.2);

    stateListener?.({ ...nativeState, requestedVisible: false, visible: false, locked: false });
    expect(store.visible).toBe(false);
    expect(store.locked).toBe(false);
    expect(writeVisible).toHaveBeenLastCalledWith(false);
    expect(writeLocked).toHaveBeenLastCalledWith(false);

    app.unmount();
    element.remove();
  });

  it("serializes startup with a newer visibility request and rolls back a failed native lock", async () => {
    let releaseStartup!: () => void;
    const startupGate = new Promise<void>((resolve) => { releaseStartup = resolve; });
    let nativeState: DesktopLyricsWindowState = {
      requestedVisible: false,
      visible: false,
      locked: false,
      hiddenForFullscreen: false,
      fullscreenBehavior: "show",
    };
    const setVisible = vi.fn(async (visible: boolean) => {
      nativeState = { ...nativeState, requestedVisible: visible, visible };
      return nativeState;
    });
    const services = {
      desktopLyrics: {
        async getWindowState() { return nativeState; },
        setVisible,
        async toggleVisible() { return nativeState; },
        async setLocked() { throw new Error("native lock failed"); },
        async setFullscreenBehavior(fullscreenBehavior: "show" | "hide") {
          await startupGate;
          nativeState = { ...nativeState, fullscreenBehavior };
          return nativeState;
        },
        async sendSnapshot() {},
        async sendClock() {},
        async onAction() { return () => undefined; },
        async onWindowState() { return () => undefined; },
      },
      uiPreferences: {
        readDesktopLyrics: () => ({
          visible: false,
          locked: false,
          fullscreenBehavior: "show" as const,
          fontScale: 1,
          textColor: "#f4f5f7",
          highlightColor: "#cf9437",
          wordLyricsEnabled: true,
        }),
        writeDesktopLyricsVisible() {},
        writeDesktopLyricsLocked() {},
        writeDesktopLyricsFullscreenBehavior() {},
        writeDesktopLyricsWordLyricsEnabled() {},
      },
    } as unknown as ApplicationServices;
    let store!: ReturnType<typeof useDesktopLyricsStore>;
    let initialized!: Promise<void>;
    const Root = defineComponent({
      setup() {
        store = useDesktopLyricsStore();
        initialized = store.initialize();
        return () => h("div");
      },
    });
    const app = createApp(Root);
    app.use(createPinia());
    app.provide(applicationServicesKey, services);
    const element = document.createElement("div");
    document.body.appendChild(element);
    app.mount(element);

    await Promise.resolve();
    const newerVisibility = store.setVisible(true);
    releaseStartup();
    await Promise.all([initialized, newerVisibility]);

    expect(setVisible.mock.calls.map(([value]) => value)).toEqual([false, true]);
    expect(store.visible).toBe(true);

    await store.setLocked(true);
    expect(store.locked).toBe(false);

    app.unmount();
    element.remove();
  });
});
