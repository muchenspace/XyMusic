import type { PageLifecycle } from "../../application/ports/PageLifecycle";

export class BrowserPageLifecycle implements PageLifecycle {
  onPageHide(listener: () => void): () => void {
    window.addEventListener("pagehide", listener);
    return () => window.removeEventListener("pagehide", listener);
  }
}
