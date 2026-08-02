import type { Diagnostics } from "../ports/Diagnostics";
import type { DesktopTheme, DesktopWindow } from "../ports/DesktopWindow";
import type {
  DesktopWindowController as DesktopWindowControllerPort,
  DesktopWindowControllerState as DesktopWindowState,
} from "../ports/DesktopWindowController";
import type { TaskScheduler } from "../ports/TaskScheduler";
import { SerialTaskQueue } from "./SerialTaskQueue";

export type { DesktopWindowControllerState as DesktopWindowState } from "../ports/DesktopWindowController";

/**
 * Owns native window state observation and exposes one bounded state stream
 * to presentation. Resize bursts become a single trailing native refresh.
 */
export class DesktopWindowController implements DesktopWindowControllerPort {
  private readonly commandQueue = new SerialTaskQueue();
  private readonly listeners = new Set<(state: DesktopWindowState) => void>();
  private stateValue: DesktopWindowState = { maximized: false, fullscreen: false };
  private initializePromise: Promise<void> | undefined;
  private initialized = false;
  private removeResizeListener: (() => void) | undefined;
  private cancelRefresh: (() => void) | undefined;
  private stateRevision = 0;
  private disposed = false;

  constructor(
    private readonly desktopWindow: DesktopWindow,
    private readonly diagnostics: Diagnostics,
    private readonly scheduler: TaskScheduler,
  ) {}

  state(): DesktopWindowState {
    return this.stateValue;
  }

  subscribe(listener: (state: DesktopWindowState) => void): () => void {
    if (this.disposed) return () => undefined;
    this.listeners.add(listener);
    listener(this.stateValue);
    void this.initialize();
    return () => this.listeners.delete(listener);
  }

  initialize(): Promise<void> {
    if (this.disposed || this.initialized) return Promise.resolve();
    if (this.initializePromise) return this.initializePromise;
    let initialization!: Promise<void>;
    initialization = Promise.all([
      this.refreshState(),
      this.registerResizeListener(),
    ]).then(() => undefined).finally(() => {
      if (this.initializePromise === initialization) this.initializePromise = undefined;
      if (!this.disposed) this.initialized = true;
    });
    this.initializePromise = initialization;
    return initialization;
  }

  minimize(): Promise<void> {
    return this.runCommand("minimize the window", () => this.desktopWindow.minimize());
  }

  toggleMaximize(): Promise<void> {
    return this.runCommand("toggle window maximization", () => this.desktopWindow.toggleMaximize(), true);
  }

  toggleFullscreen(): Promise<void> {
    return this.runCommand("toggle window fullscreen mode", () => this.desktopWindow.toggleFullscreen(), true);
  }

  close(): Promise<void> {
    return this.runCommand("hide the main window", () => this.desktopWindow.close());
  }

  setTheme(theme: DesktopTheme): Promise<void> {
    return this.runCommand("apply the window theme", () => this.desktopWindow.setTheme(theme));
  }

  dispose(): void {
    if (this.disposed) return;
    this.disposed = true;
    this.stateRevision += 1;
    this.cancelRefresh?.();
    this.cancelRefresh = undefined;
    this.removeResizeListener?.();
    this.removeResizeListener = undefined;
    this.listeners.clear();
  }

  private async registerResizeListener(): Promise<void> {
    try {
      const remove = await this.desktopWindow.onResized(() => this.scheduleStateRefresh());
      if (this.disposed) {
        remove();
        return;
      }
      this.removeResizeListener = remove;
    } catch (cause) {
      if (!this.disposed) this.report("observe native window changes", cause);
    }
  }

  private scheduleStateRefresh(): void {
    if (this.disposed) return;
    this.cancelRefresh?.();
    this.cancelRefresh = this.scheduler.delay(() => {
      this.cancelRefresh = undefined;
      void this.refreshState();
    }, WINDOW_STATE_REFRESH_DEBOUNCE_MS);
  }

  private async runCommand(
    action: string,
    command: () => Promise<void>,
    refreshState = false,
  ): Promise<void> {
    return this.commandQueue.run(async () => {
      if (this.disposed) return;
      this.cancelRefresh?.();
      this.cancelRefresh = undefined;
      this.stateRevision += 1;
      try {
        await command();
      } catch (cause) {
        if (!this.disposed) this.report(action, cause);
        throw cause;
      }
      if (refreshState && !this.disposed) await this.refreshState();
    });
  }

  private async refreshState(): Promise<void> {
    if (this.disposed) return;
    const revision = ++this.stateRevision;
    try {
      const [maximized, fullscreen] = await Promise.all([
        this.desktopWindow.isMaximized(),
        this.desktopWindow.isFullscreen(),
      ]);
      if (this.disposed || revision !== this.stateRevision) return;
      this.updateState({ maximized, fullscreen });
    } catch (cause) {
      if (!this.disposed && revision === this.stateRevision) this.report("read native window state", cause);
    }
  }

  private updateState(next: DesktopWindowState): void {
    if (this.stateValue.maximized === next.maximized && this.stateValue.fullscreen === next.fullscreen) return;
    this.stateValue = next;
    for (const listener of this.listeners) listener(next);
  }

  private report(action: string, cause: unknown): void {
    const detail = cause instanceof Error ? cause.message : String(cause);
    this.diagnostics.warn("window", `Could not ${action}: ${detail}`);
  }
}

const WINDOW_STATE_REFRESH_DEBOUNCE_MS = 120;
