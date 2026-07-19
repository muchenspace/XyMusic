import { describe, expect, it } from "vitest";
import { shouldNotifyQueryError } from "@/app/query-client";

describe("query error notifications", () => {
  it("keeps initial page errors in the page instead of duplicating them as toasts", () => {
    expect(shouldNotifyQueryError(undefined)).toBe(false);
  });

  it("notifies when a background refresh fails over cached data", () => {
    expect(shouldNotifyQueryError({ items: [] })).toBe(true);
    expect(shouldNotifyQueryError(null)).toBe(true);
  });
});
