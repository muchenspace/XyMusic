export type DesktopTheme = "dark" | "light";

export interface DesktopWindow {
  minimize(): Promise<void>;
  toggleMaximize(): Promise<void>;
  toggleFullscreen(): Promise<void>;
  isMaximized(): Promise<boolean>;
  close(): Promise<void>;
  onResized(listener: () => void): Promise<() => void>;
  setTheme(theme: DesktopTheme): Promise<void>;
  setMiniMode(enabled: boolean): Promise<void>;
}
