import type { DesktopTheme } from "./DesktopWindow";

export interface DesktopWindowControllerState {
  readonly maximized: boolean;
  readonly fullscreen: boolean;
}

/** Application-facing control boundary for main-window state and commands. */
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
