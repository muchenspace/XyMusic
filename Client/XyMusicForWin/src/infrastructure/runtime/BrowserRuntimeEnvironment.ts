import type { RuntimeEnvironment, RuntimeKind } from "../../application/ports/RuntimeEnvironment";

export class BrowserRuntimeEnvironment implements RuntimeEnvironment {
  kind(): RuntimeKind {
    return typeof window !== "undefined" && "__TAURI_INTERNALS__" in window ? "tauri" : "browser";
  }

  userAgent(): string {
    return typeof navigator === "undefined" ? "unavailable" : navigator.userAgent;
  }
}
