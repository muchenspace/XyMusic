import { describe, expect, it } from "vitest";
import { SerialTaskQueue } from "../src/application/services/SerialTaskQueue";

describe("serial task queue", () => {
  it("runs later window transitions after an earlier transition fails", async () => {
    const queue = new SerialTaskQueue();
    const gate = deferred<void>();
    const calls: string[] = [];
    const first = queue.run(async () => {
      calls.push("first:start");
      await gate.promise;
      calls.push("first:end");
      throw new Error("window operation failed");
    });
    const second = queue.run(async () => { calls.push("second"); });

    await Promise.resolve();
    expect(calls).toEqual(["first:start"]);
    gate.resolve();
    await expect(first).rejects.toThrow("window operation failed");
    await second;
    expect(calls).toEqual(["first:start", "first:end", "second"]);
  });
});

function deferred<T>() {
  let resolve!: (value: T | PromiseLike<T>) => void;
  const promise = new Promise<T>((resolvePromise) => { resolve = resolvePromise; });
  return { promise, resolve };
}
