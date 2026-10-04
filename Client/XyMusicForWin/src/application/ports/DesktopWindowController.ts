import type { DesktopTheme } from "./DesktopWindow";

export interface DesktopWindowControllerState {
  readonly maximized: boolean;
  readonly fullscreen: boolean;
}

/** Application-facing control boundary for main-window state and commands.
 *
 * Platform constraint mirrored from the native shell: true fullscreen is
 * unavailable while mini mode is active (and mini mode cannot change while
 * true fullscreen is active). The native adapter rejects those transitions
 * with an error; callers should not present them as available actions.
 */
export interface DesktopWindowController {
  state(): DesktopWindowControllerState;
  subscribe(listener: (state: DesktopWindowControllerState) => void): () => void;
  initialize(): Promise<void>;
  minimize(): Promise<void>;
  toggleMaximize(): Promise<void>;
  toggleFullscreen(): Promise<void>;
  close(): Promise<void>;
  setTheme(theme: DesktopTheme): Promise<void>;
  dispose(): void;
}
