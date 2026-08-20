import { describe, expect, it, vi } from "vitest";
import type { Diagnostics } from "../src/application/ports/Diagnostics";
import type { DesktopWindow } from "../src/application/ports/DesktopWindow";
import type { TaskScheduler } from "../src/application/ports/TaskScheduler";
import { DesktopWindowController } from "../src/application/services/DesktopWindowController";

describe("desktop window controller", () => {
  it("coalesces a resize burst into one trailing native state refresh", async () => {
    let resized!: () => void;
    const desktop = createDesktop({
      onResized: async (listener) => {
        resized = listener;
        return () => undefined;
      },
    });
    const scheduler = new ManualTaskScheduler();
    const controller = new DesktopWindowController(desktop, diagnostics(), scheduler);

    await controller.initialize();
    expect(desktop.isMaximized).toHaveBeenCalledOnce();
    expect(desktop.isFullscreen).toHaveBeenCalledOnce();

    resized();
    resized();
    resized();
    expect(scheduler.pendingCount).toBe(1);

    scheduler.flush();
    await settle();

    expect(desktop.isMaximized).toHaveBeenCalledTimes(2);
    expect(desktop.isFullscreen).toHaveBeenCalledTimes(2);
  });

  it("does not let an older state query overwrite a newer fullscreen command", async () => {
    let resized!: () => void;
    const staleMaximized = deferred<boolean>();
    const staleFullscreen = deferred<boolean>();
    const desktop = createDesktop({
      onResized: async (listener) => {
        resized = listener;
        return () => undefined;
      },
      isMaximized: vi.fn()
        .mockResolvedValueOnce(false)
        .mockReturnValueOnce(staleMaximized.promise)
        .mockResolvedValueOnce(true),
      isFullscreen: vi.fn()
        .mockResolvedValueOnce(false)
        .mockReturnValueOnce(staleFullscreen.promise)
        .mockResolvedValueOnce(true),
    });
    const scheduler = new ManualTaskScheduler();
    const controller = new DesktopWindowController(desktop, diagnostics(), scheduler);

    await controller.initialize();
    resized();
    scheduler.flush();
    await settle();

    await controller.toggleFullscreen();
    expect(controller.state()).toEqual({ maximized: false, fullscreen: true });

    staleMaximized.resolve(false);
    staleFullscreen.resolve(false);
    await settle();

    expect(controller.state()).toEqual({ maximized: false, fullscreen: true });
  });

  it("removes a resize listener that resolves after disposal", async () => {
    const registration = deferred<() => void>();
    const remove = vi.fn();
    const desktop = createDesktop({ onResized: () => registration.promise });
    const controller = new DesktopWindowController(desktop, diagnostics(), new ManualTaskScheduler());

    const initialization = controller.initialize();
    controller.dispose();
    registration.resolve(remove);
    await initialization;

    expect(remove).toHaveBeenCalledOnce();
  });

  it("preserves the last observed state when a command fails", async () => {
    const warn = vi.fn();
    const desktop = createDesktop({
      toggleFullscreen: vi.fn(async () => { throw new Error("native failure"); }),
    });
    const controller = new DesktopWindowController(desktop, diagnostics(warn), new ManualTaskScheduler());

    await controller.initialize();
    await expect(controller.toggleFullscreen()).rejects.toThrow("native failure");

    expect(controller.state()).toEqual({ maximized: false, fullscreen: false });
    expect(warn).toHaveBeenCalledWith("window", "Could not toggle window fullscreen mode: native failure");
  });

  it("exposes true fullscreen as its own state instead of a maximized fullscreen hybrid", async () => {
    const desktop = createDesktop({
      isMaximized: vi.fn(async () => true),
      isFullscreen: vi.fn(async () => true),
    });
    const controller = new DesktopWindowController(desktop, diagnostics(), new ManualTaskScheduler());

    await controller.initialize();

    expect(controller.state()).toEqual({ maximized: false, fullscreen: true });
  });
});

function createDesktop(overrides: Partial<DesktopWindow> = {}): DesktopWindow & {
  isMaximized: ReturnType<typeof vi.fn>;
  isFullscreen: ReturnType<typeof vi.fn>;
} {
  return {
    minimize: vi.fn(async () => undefined),
    toggleMaximize: vi.fn(async () => undefined),
    toggleFullscreen: vi.fn(async () => undefined),
    isMaximized: vi.fn(async () => false),
    isFullscreen: vi.fn(async () => false),
    close: vi.fn(async () => undefined),
    onResized: vi.fn(async () => () => undefined),
    setTheme: vi.fn(async () => undefined),
    setMiniMode: vi.fn(async () => undefined),
    ...overrides,
  } as DesktopWindow & {
    isMaximized: ReturnType<typeof vi.fn>;
    isFullscreen: ReturnType<typeof vi.fn>;
  };
}

function diagnostics(warn = vi.fn()): Diagnostics {
  return { info() {}, warn, error() {}, entries: () => [], clear() {} };
}

class ManualTaskScheduler implements TaskScheduler {
  private tasks: Array<{ callback: () => void; cancelled: boolean }> = [];

  get pendingCount(): number {
    return this.tasks.filter((task) => !task.cancelled).length;
  }

  delay(callback: () => void): () => void {
    const task = { callback, cancelled: false };
    this.tasks.push(task);
    return () => { task.cancelled = true; };
  }

  whenIdle(callback: () => void): () => void {
    return this.delay(callback);
  }

  flush(): void {
    const tasks = this.tasks;
    this.tasks = [];
    for (const task of tasks) if (!task.cancelled) task.callback();
  }
}

function deferred<T>() {
  let resolve!: (value: T | PromiseLike<T>) => void;
  const promise = new Promise<T>((resolvePromise) => { resolve = resolvePromise; });
  return { promise, resolve };
}

async function settle(): Promise<void> {
  await Promise.resolve();
  await Promise.resolve();
  await Promise.resolve();
}
