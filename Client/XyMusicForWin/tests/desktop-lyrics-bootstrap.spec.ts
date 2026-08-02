import type { DesktopLyricsWindowPlacement } from "../src/application/ports/DesktopLyricsWindowPlacement";
import type { DesktopLyricsBridge } from "../src/desktop-lyrics/bridge";
import { describe, expect, it, vi } from "vitest";

const appFixture = vi.hoisted(() => {
  const unmountHandlers: Array<() => void> = [];
  const app = {
    mount: vi.fn(),
    onUnmount: vi.fn((handler: () => void) => unmountHandlers.push(handler)),
    unmount: vi.fn(() => unmountHandlers.splice(0).forEach((handler) => handler())),
  };
  return { app, createApp: vi.fn(() => app), unmountHandlers };
});

vi.mock("vue", () => ({ createApp: appFixture.createApp }));
vi.mock("../src/desktop-lyrics/DesktopLyricsApp.vue", () => ({ default: {} }));

import { bootstrapDesktopLyricsApp } from "../src/desktop-lyrics";

describe("desktop lyrics bootstrap", () => {
  it("releases an observer that resolves after the app has unmounted", async () => {
    const observation = deferred<() => void>();
    const observeStarted = deferred<void>();
    const stopObserving = vi.fn();
    const placement: DesktopLyricsWindowPlacement = {
      restore: vi.fn(async () => undefined),
      observe: vi.fn(async () => {
        observeStarted.resolve();
        return observation.promise;
      }),
    };

    const bootstrapping = bootstrapDesktopLyricsApp(document.createElement("div"), {
      bridge: inertBridge(),
      placement,
    });
    await observeStarted.promise;
    appFixture.app.unmount();
    observation.resolve(stopObserving);

    const app = await bootstrapping;

    expect(appFixture.app.onUnmount).toHaveBeenCalledOnce();
    expect(app).toBe(appFixture.app);
    expect(stopObserving).toHaveBeenCalledOnce();
  });
});

function inertBridge(): DesktopLyricsBridge {
  return {
    async onState() { return () => undefined; },
    async onClock() { return () => undefined; },
    async emitAction() {},
  };
}

function deferred<T>() {
  let resolve!: (value: T | PromiseLike<T>) => void;
  const promise = new Promise<T>((resolvePromise) => { resolve = resolvePromise; });
  return { promise, resolve };
}
