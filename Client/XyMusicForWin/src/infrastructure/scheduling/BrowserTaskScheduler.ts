import type { TaskScheduler } from "../../application/ports/TaskScheduler";

export class BrowserTaskScheduler implements TaskScheduler {
  delay(callback: () => void, milliseconds: number): () => void {
    const handle = window.setTimeout(callback, milliseconds);
    return () => window.clearTimeout(handle);
  }

  whenIdle(callback: () => void, timeoutMilliseconds: number): () => void {
    if (typeof window.requestIdleCallback === "function") {
      const handle = window.requestIdleCallback(callback, { timeout: timeoutMilliseconds });
      return () => window.cancelIdleCallback?.(handle);
    }
    const handle = window.setTimeout(callback, 0);
    return () => window.clearTimeout(handle);
  }
}
