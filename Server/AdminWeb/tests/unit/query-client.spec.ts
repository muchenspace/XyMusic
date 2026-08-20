import { afterEach, describe, expect, it, vi } from "vitest";
import { invalidateAdminMusicQueries, queryClient, shouldNotifyQueryError } from "@/app/query-client";

afterEach(() => {
  vi.restoreAllMocks();
  queryClient.clear();
});

describe("query error notifications", () => {
  it("keeps initial page errors in the page instead of duplicating them as toasts", () => {
    expect(shouldNotifyQueryError(undefined)).toBe(false);
  });

  it("notifies when a background refresh fails over cached data", () => {
    expect(shouldNotifyQueryError({ items: [] })).toBe(true);
    expect(shouldNotifyQueryError(null)).toBe(true);
  });

  it("refreshes cached catalog lists while keeping inactive detail queries stale", async () => {
    const invalidate = vi.spyOn(queryClient, "invalidateQueries")
      .mockImplementation(async () => undefined);

    await invalidateAdminMusicQueries();

    const calls = invalidate.mock.calls.map(([filters]) => {
      const resolved = typeof filters === "function" ? filters() : filters;
      return [resolved?.queryKey, resolved?.refetchType];
    });
    expect(calls).toEqual(expect.arrayContaining([
      [["admin", "tracks"], "all"],
      [["admin", "albums"], "all"],
      [["admin", "artists"], "all"],
      [["admin", "track"], "active"],
      [["admin", "album"], "active"],
      [["admin", "dashboard"], "active"],
      [["admin", "audit"], "active"],
    ]));
    expect(calls).toHaveLength(7);
  });
});
