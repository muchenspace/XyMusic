export type DesktopMediaAction = "play" | "pause" | "toggle" | "previous" | "next" | "stop" | "seek";

export interface DesktopMediaMetadata {
  title: string;
  artist?: string;
  album?: string;
  artworkUrl?: string;
}

export interface DesktopPlaybackState {
  status: "playing" | "paused" | "stopped";
  position?: number;
  duration?: number;
}

export interface DesktopIntegration {
  onMediaAction(listener: (action: DesktopMediaAction, position?: number) => void): Promise<() => void>;
  updateMediaMetadata(metadata: DesktopMediaMetadata): Promise<void>;
  updateMediaPlayback(state: DesktopPlaybackState): Promise<void>;
  clearMediaSession(): Promise<void>;
}
