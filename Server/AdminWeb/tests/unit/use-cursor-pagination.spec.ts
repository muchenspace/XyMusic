import { describe, expect, it } from "vitest";
import { useCursorPagination } from "@/shared/presentation/use-cursor-pagination";

function createPagination(options: { fetching?: boolean; nextCursor?: string | undefined } = {}) {
  let fetching = options.fetching ?? false;
  let nextCursor = options.nextCursor;
  const pagination = useCursorPagination({
    initialPageSize: 50,
    isFetching: () => fetching,
    nextCursor: () => nextCursor,
  });
  return {
    pagination,
    setFetching: (value: boolean) => { fetching = value; },
    setNextCursor: (value: string | undefined) => { nextCursor = value; },
  };
}

describe("useCursorPagination", () => {
  it("stores the next cursor per page and moves backwards from history", () => {
    const { pagination, setNextCursor } = createPagination();
    setNextCursor("cursor-page-2");

    pagination.changePage(2);
    expect(pagination.page.value).toBe(2);
    expect(pagination.cursor.value).toBe("cursor-page-2");

    setNextCursor("cursor-page-3");
    pagination.changePage(3);
    expect(pagination.cursor.value).toBe("cursor-page-3");

    setNextCursor(undefined);
    pagination.changePage(2);
    expect(pagination.page.value).toBe(2);
    expect(pagination.cursor.value).toBe("cursor-page-2");

    pagination.changePage(1);
    expect(pagination.page.value).toBe(1);
    expect(pagination.cursor.value).toBe("");
  });

  it("ignores navigation while fetching or without a next cursor", () => {
    const { pagination, setFetching, setNextCursor } = createPagination();
    setNextCursor(undefined);
    pagination.changePage(2);
    expect(pagination.page.value).toBe(1);

    setNextCursor("cursor-page-2");
    setFetching(true);
    pagination.changePage(2);
    expect(pagination.page.value).toBe(1);

    setFetching(false);
    pagination.changePage(2);
    expect(pagination.page.value).toBe(2);
  });

  it("resets page, cursor and history, and resets when the page size changes", () => {
    const { pagination, setNextCursor } = createPagination();
    setNextCursor("cursor-page-2");
    pagination.changePage(2);
    pagination.reset();
    expect(pagination.page.value).toBe(1);
    expect(pagination.cursor.value).toBe("");

    setNextCursor("cursor-page-2");
    pagination.changePage(2);
    pagination.changePageSize(100);
    expect(pagination.pageSize.value).toBe(100);
    expect(pagination.page.value).toBe(1);
    expect(pagination.cursor.value).toBe("");
  });

  it("invokes the page-change callback only after a successful change", () => {
    let changes = 0;
    const pagination = useCursorPagination({
      initialPageSize: 50,
      isFetching: () => false,
      nextCursor: () => "cursor",
      onPageChanged: () => { changes += 1; },
    });
    pagination.changePage(2);
    expect(changes).toBe(1);
    pagination.changePage(2);
    expect(changes).toBe(1);
  });
});
