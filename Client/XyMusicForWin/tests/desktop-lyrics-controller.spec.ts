import { describe, expect, it, vi } from "vitest";
import type {
  DesktopLyrics,
  DesktopLyricsAction,
  DesktopLyricsWindowState,
} from "../src/application/ports/DesktopLyrics";
import type { PageLifecycle } from "../src/application/ports/PageLifecycle";
import type { TaskScheduler } from "../src/application/ports/TaskScheduler";
import type { UserInterfacePreferences } from "../src/application/ports/UserInterfacePreferences";
import { DesktopLyricsController } from "../src/application/services/DesktopLyricsController";

describe("desktop lyrics controller", () => {
  it("applies persisted window choices before refreshing native state", async () => {
    let nativeState = state();
    let stateListener: ((next: DesktopLyricsWindowState) => void) | undefined;
    const integration = createIntegration({
      getWindowState: vi.fn(async () => nativeState),
      setVisible: vi.fn(async (visible: boolean) => {
        nativeState = { ...nativeState, requestedVisible: visible, visible };
        return nativeState;
      }),
      setLocked: vi.fn(async (locked: boolean) => {
        nativeState = { ...nativeState, locked };
        return nativeState;
      }),
      setFullscreenBehavior: vi.fn(async (fullscreenBehavior: "show" | "hide") => {
        nativeState = { ...nativeState, fullscreenBehavior };
        return nativeState;
      }),
      onWindowState: vi.fn(async (listener) => {
        stateListener = listener;
        return () => undefined;
      }),
    });
    const preferences = createPreferences({
      readDesktopLyrics: vi.fn(() => ({
        visible: true,
        locked: true,
        fullscreenBehavior: "hide" as const,
        fontScale: 1.2,
        textColor: "#abcdef",
        highlightColor: "#123456",
      })),
    });
    const controller = new DesktopLyricsController(integration, preferences, new ManualTaskScheduler(), new FakePageLifecycle());

    await controller.initialize();

    expect(integration.setFullscreenBehavior).toHaveBeenCalledWith("hide");
    expect(integration.setLocked).toHaveBeenCalledWith(true);
    expect(integration.setVisible).toHaveBeenCalledWith(true);
    expect(controller.state()).toMatchObject({
      visible: true,
      actuallyVisible: true,
      locked: true,
      fullscreenBehavior: "hide",
      fontScale: 1.2,
    });

    stateListener?.({ ...nativeState, revision: 1, requestedVisible: false, visible: false, locked: false });
    expect(controller.state()).toMatchObject({ visible: false, actuallyVisible: false, locked: false });
    expect(preferences.writeDesktopLyricsVisible).toHaveBeenLastCalledWith(false);
    expect(preferences.writeDesktopLyricsLocked).toHaveBeenLastCalledWith(false);

    controller.dispose();
  });

  it("serializes startup with a newer visibility request and rolls back a failed lock", async () => {
    const startup = deferred<void>();
    const fullscreenStarted = deferred<void>();
    let nativeState = state();
    const integration = createIntegration({
      getWindowState: vi.fn(async () => nativeState),
      setVisible: vi.fn(async (visible: boolean) => {
        nativeState = { ...nativeState, requestedVisible: visible, visible };
        return nativeState;
      }),
      setLocked: vi.fn(async () => { throw new Error("native lock failed"); }),
      setFullscreenBehavior: vi.fn(async (fullscreenBehavior: "show" | "hide") => {
        fullscreenStarted.resolve();
        await startup.promise;
        nativeState = { ...nativeState, fullscreenBehavior };
        return nativeState;
      }),
    });
    const preferences = createPreferences();
    const controller = new DesktopLyricsController(integration, preferences, new ManualTaskScheduler(), new FakePageLifecycle());

    const initializing = controller.initialize();
    await fullscreenStarted.promise;
    const newerVisibility = controller.setVisible(true);
    startup.resolve();
    await Promise.all([initializing, newerVisibility]);

    expect(integration.setVisible.mock.calls.map(([value]) => value)).toEqual([false, true]);
    expect(controller.state().visible).toBe(true);

    await controller.setLocked(true);
    expect(controller.state().locked).toBe(false);
    expect(preferences.writeDesktopLyricsLocked).toHaveBeenLastCalledWith(false);

    controller.dispose();
  });

  it("does not roll back a newer visibility intent when an older native command fails", async () => {
    const firstVisibleCommand = deferred<DesktopLyricsWindowState>();
    let visibleCalls = 0;
    const integration = createIntegration({
      setVisible: vi.fn((visible: boolean) => {
        visibleCalls += 1;
        return visibleCalls === 1
          ? firstVisibleCommand.promise
          : Promise.resolve({ ...state(), requestedVisible: visible, visible });
      }),
    });
    const controller = new DesktopLyricsController(integration, createPreferences(), new ManualTaskScheduler(), new FakePageLifecycle());

    const older = controller.setVisible(true);
    const newer = controller.setVisible(false);
    firstVisibleCommand.reject(new Error("native visibility failed"));
    await Promise.all([older, newer]);

    expect(controller.state().visible).toBe(false);
    expect(integration.setVisible.mock.calls.map(([value]) => value)).toEqual([true, false]);

    controller.dispose();
  });

  it("ignores a delayed native state event after a newer visibility command completes", async () => {
    let stateListener: ((next: DesktopLyricsWindowState) => void) | undefined;
    let nativeRevision = 0;
    const integration = createIntegration({
      setVisible: vi.fn(async (visible: boolean) => ({
        ...state(),
        revision: ++nativeRevision,
        requestedVisible: visible,
        visible,
      })),
      onWindowState: vi.fn(async (listener) => {
        stateListener = listener;
        return () => undefined;
      }),
    });
    const preferences = createPreferences();
    const controller = new DesktopLyricsController(integration, preferences, new ManualTaskScheduler(), new FakePageLifecycle());

    await controller.setVisible(true);
    await controller.setVisible(false);
    preferences.writeDesktopLyricsVisible.mockClear();

    stateListener?.({ ...state(), revision: 1, requestedVisible: true, visible: true });

    expect(controller.state()).toMatchObject({ visible: false, actuallyVisible: false });
    expect(preferences.writeDesktopLyricsVisible).not.toHaveBeenCalled();
    controller.dispose();
  });

  it("does not rewrite preferences for an unchanged native window-state event", async () => {
    let stateListener: ((next: DesktopLyricsWindowState) => void) | undefined;
    const integration = createIntegration({
      onWindowState: vi.fn(async (listener) => {
        stateListener = listener;
        return () => undefined;
      }),
    });
    const preferences = createPreferences();
    const controller = new DesktopLyricsController(integration, preferences, new ManualTaskScheduler(), new FakePageLifecycle());
    await controller.initialize();
    preferences.writeDesktopLyricsVisible.mockClear();
    preferences.writeDesktopLyricsLocked.mockClear();
    preferences.writeDesktopLyricsFullscreenBehavior.mockClear();

    stateListener?.(state());

    expect(preferences.writeDesktopLyricsVisible).not.toHaveBeenCalled();
    expect(preferences.writeDesktopLyricsLocked).not.toHaveBeenCalled();
    expect(preferences.writeDesktopLyricsFullscreenBehavior).not.toHaveBeenCalled();

    controller.dispose();
  });

  it("removes listeners that resolve after disposal", async () => {
    const windowStateRegistration = deferred<() => void>();
    const actionRegistration = deferred<() => void>();
    const removeWindowState = vi.fn();
    const removeActions = vi.fn();
    const integration = createIntegration({
      onWindowState: vi.fn(() => windowStateRegistration.promise),
      onAction: vi.fn(() => actionRegistration.promise),
    });
    const controller = new DesktopLyricsController(integration, createPreferences(), new ManualTaskScheduler(), new FakePageLifecycle());
    const removeRequests = controller.subscribePlaybackRequests(() => undefined);
    const initializing = controller.initialize();

    await waitFor(() => integration.onWindowState.mock.calls.length === 1 && integration.onAction.mock.calls.length === 1);
    controller.dispose();
    removeRequests();
    windowStateRegistration.resolve(removeWindowState);
    actionRegistration.resolve(removeActions);
    await initializing;
    await settle();

    expect(removeWindowState).toHaveBeenCalledOnce();
    expect(removeActions).toHaveBeenCalledOnce();
  });

  it("handles native configuration actions locally and only forwards playback requests", async () => {
    let actionListener: ((action: DesktopLyricsAction) => void) | undefined;
    const integration = createIntegration({
      onAction: vi.fn(async (listener) => {
        actionListener = listener;
        return () => undefined;
      }),
    });
    const controller = new DesktopLyricsController(integration, createPreferences(), new ManualTaskScheduler(), new FakePageLifecycle());
    const requests: string[] = [];
    controller.subscribePlaybackRequests((request) => requests.push(request));
    await waitFor(() => Boolean(actionListener));

    actionListener?.({ version: 4, issuedAtMs: 1, action: "set-font-scale", value: 1.18 });
    actionListener?.({ version: 4, issuedAtMs: 2, action: "previous" });

    expect(controller.state().fontScale).toBe(1.18);
    expect(requests).toEqual(["previous"]);

    controller.dispose();
  });

  it("defers repeated font-scale persistence and flushes the latest value on page hide", () => {
    const scheduler = new ManualTaskScheduler();
    const lifecycle = new FakePageLifecycle();
    const preferences = createPreferences();
    const controller = new DesktopLyricsController(createIntegration(), preferences, scheduler, lifecycle);

    controller.setFontScale(1.1);
    controller.setFontScale(1.24);

    expect(controller.state().fontScale).toBe(1.24);
    expect(preferences.writeDesktopLyricsFontScale).not.toHaveBeenCalled();
    expect(scheduler.delays).toEqual([180, 180]);

    lifecycle.emitPageHide();
    scheduler.runAll();
    expect(preferences.writeDesktopLyricsFontScale).toHaveBeenCalledExactlyOnceWith(1.24);

    controller.dispose();
  });
});

function createIntegration(overrides: Partial<DesktopLyrics> = {}): DesktopLyrics & Record<string, ReturnType<typeof vi.fn>> {
  return {
    getWindowState: vi.fn(async () => state()),
    setVisible: vi.fn(async (visible: boolean) => ({ ...state(), requestedVisible: visible, visible })),
    toggleVisible: vi.fn(async () => state()),
    setLocked: vi.fn(async (locked: boolean) => ({ ...state(), locked })),
    setFullscreenBehavior: vi.fn(async (fullscreenBehavior: "show" | "hide") => ({ ...state(), fullscreenBehavior })),
    sendSnapshot: vi.fn(async () => undefined),
    sendClock: vi.fn(async () => undefined),
    onAction: vi.fn(async () => () => undefined),
    onWindowState: vi.fn(async () => () => undefined),
    ...overrides,
  } as DesktopLyrics & Record<string, ReturnType<typeof vi.fn>>;
}

function createPreferences(overrides: Partial<ReturnType<typeof desktopLyricsPreferences>> = {}) {
  return {
    ...desktopLyricsPreferences(),
    ...overrides,
  } as UserInterfacePreferences & {
    writeDesktopLyricsVisible: ReturnType<typeof vi.fn>;
    writeDesktopLyricsLocked: ReturnType<typeof vi.fn>;
    writeDesktopLyricsFullscreenBehavior: ReturnType<typeof vi.fn>;
    writeDesktopLyricsFontScale: ReturnType<typeof vi.fn>;
  };
}

function desktopLyricsPreferences() {
  return {
    readDesktopLyrics: vi.fn(() => ({
      visible: false,
      locked: false,
      fullscreenBehavior: "show" as const,
      fontScale: 1,
      textColor: "#f4f5f7",
      highlightColor: "#cf9437",
    })),
    writeDesktopLyricsVisible: vi.fn(),
    writeDesktopLyricsLocked: vi.fn(),
    writeDesktopLyricsFullscreenBehavior: vi.fn(),
    writeDesktopLyricsFontScale: vi.fn(),
    writeDesktopLyricsTextColor: vi.fn(),
    writeDesktopLyricsHighlightColor: vi.fn(),
  };
}

class ManualTaskScheduler implements TaskScheduler {
  readonly delays: number[] = [];
  private readonly tasks = new Set<{ callback: () => void; cancelled: boolean }>();

  delay(callback: () => void, milliseconds: number): () => void {
    this.delays.push(milliseconds);
    const task = { callback, cancelled: false };
    this.tasks.add(task);
    return () => {
      task.cancelled = true;
      this.tasks.delete(task);
    };
  }

  whenIdle(callback: () => void): () => void {
    return this.delay(callback, 0);
  }

  runAll(): void {
    for (const task of [...this.tasks]) {
      this.tasks.delete(task);
      if (!task.cancelled) task.callback();
    }
  }
}

class FakePageLifecycle implements PageLifecycle {
  private listener: (() => void) | undefined;

  onPageHide(listener: () => void): () => void {
    this.listener = listener;
    return () => {
      if (this.listener === listener) this.listener = undefined;
    };
  }

  emitPageHide(): void {
    this.listener?.();
  }
}

function state(): DesktopLyricsWindowState {
  return {
    revision: 0,
    requestedVisible: false,
    visible: false,
    locked: false,
    hiddenForFullscreen: false,
    fullscreenBehavior: "show",
  };
}

function deferred<T>() {
  let resolve!: (value: T | PromiseLike<T>) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise;
    reject = rejectPromise;
  });
  return { promise, resolve, reject };
}

async function waitFor(condition: () => boolean): Promise<void> {
  for (let attempt = 0; attempt < 10; attempt += 1) {
    if (condition()) return;
    await Promise.resolve();
  }
  throw new Error("Expected condition to become true");
}

async function settle(): Promise<void> {
  await Promise.resolve();
  await Promise.resolve();
}
