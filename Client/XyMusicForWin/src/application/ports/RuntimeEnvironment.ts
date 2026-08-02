export type RuntimeKind = "tauri" | "browser";

/** Platform-neutral runtime details used by diagnostic presentation. */
export interface RuntimeEnvironment {
  kind(): RuntimeKind;
  userAgent(): string;
}
