/** Browser lifecycle events required by an application session. */
export interface PageLifecycle {
  onPageHide(listener: () => void): () => void;
}
