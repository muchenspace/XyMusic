export interface TaskScheduler {
  delay(callback: () => void, milliseconds: number): () => void;
  whenIdle(callback: () => void, timeoutMilliseconds: number): () => void;
}
