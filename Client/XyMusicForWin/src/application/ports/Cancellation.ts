/**
 * Application-level cancellation primitive. It centralizes AbortController
 * creation and abort subscription inside one application-owned type while the
 * AbortSignal boundary expected by repository and audio ports stays intact.
 * AbortController is a standard runtime global (available in browsers and
 * Node/vitest), so inner layers do not reference host objects such as window.
 */
export interface CancellationToken {
  readonly signal: AbortSignal;
  readonly aborted: boolean;
  abort(reason?: unknown): void;
  onAbort(listener: () => void): () => void;
}

export class CancellationSource implements CancellationToken {
  private readonly controller = new AbortController();

  get signal(): AbortSignal {
    return this.controller.signal;
  }

  get aborted(): boolean {
    return this.controller.signal.aborted;
  }

  get reason(): unknown {
    return this.controller.signal.reason;
  }

  abort(reason?: unknown): void {
    if (!this.controller.signal.aborted) this.controller.abort(reason);
  }

  onAbort(listener: () => void): () => void {
    if (this.controller.signal.aborted) {
      listener();
      return () => undefined;
    }
    this.controller.signal.addEventListener("abort", listener, { once: true });
    return () => this.controller.signal.removeEventListener("abort", listener);
  }
}

export function cancellationReason(token: CancellationToken): unknown {
  return token.signal.reason ?? createAbortError("Request cancelled");
}

export function createAbortError(message: string): Error {
  const error = new Error(message);
  error.name = "AbortError";
  return error;
}
