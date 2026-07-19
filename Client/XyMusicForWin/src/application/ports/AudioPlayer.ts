export interface AudioSnapshot {
  currentTime: number;
  duration: number;
  paused: boolean;
}

export interface AudioPlayer {
  load(url: string, signal?: AbortSignal): Promise<void>;
  preload?(url: string, signal?: AbortSignal): Promise<void>;
  activatePreloaded?(fadeSeconds: number, onActivated?: () => void): Promise<boolean>;
  clearPreloaded?(): void;
  play(): Promise<void>;
  pause(): void;
  stop(): void;
  seek(seconds: number): void;
  setVolume(volume: number): void;
  snapshot(): AudioSnapshot;
  onUpdate(listener: (snapshot: AudioSnapshot) => void): () => void;
  onEnded(listener: () => void): () => void;
  onError(listener: (message: string) => void): () => void;
}
