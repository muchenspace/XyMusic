export interface DesktopLyricsWindowPlacement {
  restore(): Promise<void>;
  observe(): Promise<() => void>;
}
