import { describe, expect, it, vi } from "vitest";
import { ApiError, type ApiClient } from "../src/infrastructure/http/ApiClient";
import {
  collectContinuation,
  mapCursorPage,
  normalizePageLimit,
  withCursor,
} from "../src/infrastructure/http/cursorPagination";
import { HttpLibraryRepository } from "../src/infrastructure/repositories/HttpLibraryRepository";

describe("cursor pagination guards", () => {
  it("stops repeated cursors instead of looping forever", async () => {
    const loadPage = vi.fn().mockResolvedValue({ items: [2], nextCursor: "repeat" });

    const error = await collectContinuation({ items: [1], nextCursor: "repeat" }, loadPage).catch((cause: unknown) => cause);

    expect(error).toBeInstanceOf(ApiError);
    expect((error as ApiError).code).toBe("INVALID_PAGINATION");
    expect(loadPage).toHaveBeenCalledTimes(1);
  });

  it("preserves existing filters when adding a cursor", () => {
    expect(withCursor("api/v1/tracks?sort=TITLE_ASC&limit=50", "next/value"))
      .toBe("api/v1/tracks?sort=TITLE_ASC&limit=50&cursor=next%2Fvalue");
  });

  it("validates cursors and clamps page limits", () => {
    expect(() => mapCursorPage({ items: [], nextCursor: 3 as unknown as string }, String)).toThrowError(ApiError);
    expect(normalizePageLimit(0)).toBe(1);
    expect(normalizePageLimit(500)).toBe(100);
    expect(normalizePageLimit(Number.NaN)).toBe(50);
  });

  it("returns a Chinese pagination error for malformed library pages", async () => {
    const request = vi.fn().mockResolvedValue({ nextCursor: null });
    const repository = new HttpLibraryRepository({ request } as unknown as ApiClient);

    const error = await repository.getHistoryTracks(undefined, 500).catch((cause: unknown) => cause);

    expect(request.mock.calls[0]?.[0]).toContain("limit=100");
    expect(error).toBeInstanceOf(ApiError);
    expect((error as ApiError).message).toContain("播放历史分页响应");
  });
});
