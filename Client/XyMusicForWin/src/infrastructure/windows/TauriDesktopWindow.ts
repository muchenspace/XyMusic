import { invoke } from "@tauri-apps/api/core";
import { getCurrentWindow } from "@tauri-apps/api/window";
import type { DesktopTheme, DesktopWindow } from "../../application/ports/DesktopWindow";
import { SerialTaskQueue } from "../../application/services/SerialTaskQueue";

export class TauriDesktopWindow implements DesktopWindow {
  private readonly windowModeTransitions = new SerialTaskQueue();

  async minimize(): Promise<void> {
    if (isTauriRuntime()) await getCurrentWindow().minimize();
  }

  toggleMaximize(): Promise<void> {
    return this.windowModeTransitions.run(async () => {
      if (!isTauriRuntime()) return;
      const appWindow = getCurrentWindow();
      if (await appWindow.isFullscreen()) return;
      await appWindow.toggleMaximize();
    });
  }

  toggleFullscreen(): Promise<void> {
    return this.windowModeTransitions.run(async () => {
      if (!isTauriRuntime()) return;
      const appWindow = getCurrentWindow();
      await appWindow.setFullscreen(!(await appWindow.isFullscreen()));
    });
  }

  async isMaximized(): Promise<boolean> {
    return isTauriRuntime() ? getCurrentWindow().isMaximized() : false;
  }

  async close(): Promise<void> {
    if (isTauriRuntime()) await getCurrentWindow().close();
  }

  async onResized(listener: () => void): Promise<() => void> {
    return isTauriRuntime() ? getCurrentWindow().onResized(listener) : () => undefined;
  }

  async setTheme(theme: DesktopTheme): Promise<void> {
    if (!isTauriRuntime()) return;
    const appWindow = getCurrentWindow() as ReturnType<typeof getCurrentWindow> & { setTheme?: (value: DesktopTheme) => Promise<void> };
    await appWindow.setTheme?.(theme);
  }

  setMiniMode(enabled: boolean): Promise<void> {
    return this.windowModeTransitions.run(async () => {
      if (!isTauriRuntime()) return;
      await invoke("set_mini_mode", { enabled });
    });
  }
}

function isTauriRuntime(): boolean {
  return "__TAURI_INTERNALS__" in window;
}
