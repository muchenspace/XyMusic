import type { SessionIdGenerator } from "../../application/ports/SessionIdGenerator";

export class BrowserSessionIdGenerator implements SessionIdGenerator {
  next(): string {
    return crypto.randomUUID();
  }
}
