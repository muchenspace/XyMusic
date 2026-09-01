import type { StreamProtocol } from "../../domain/music";

export interface AudioSnapshot {
  currentTime: number;
  duration: number;
  paused: boolean;
}

export interface AudioSourceMetadata {
  bitrate?: number;
  duration?: number;
  startOffset?: number;
  streamProtocol?: StreamProtocol;
}

export interface AudioBandwidthSample {
  bitsPerSecond: number;
  durationMs: number;
}

export interface AudioPlayer {
  load(url: string, signal?: AbortSignal, metadata?: AudioSourceMetadata): Promise<void>;
  preload?(url: string, signal?: AbortSignal, metadata?: AudioSourceMetadata): Promise<void>;
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
  onBandwidthSample?(listener: (sample: AudioBandwidthSample) => void): () => void;
  onBuffering?(listener: () => void): () => void;
  onNetworkChange?(listener: () => void): () => void;
}
