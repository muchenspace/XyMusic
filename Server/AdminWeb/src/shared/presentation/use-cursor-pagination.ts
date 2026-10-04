import { ref, type Ref } from "vue";

/**
 * Shared cursor-pagination state machine used by every list page.
 *
 * Callers provide the current fetching flag and next cursor for their own
 * query; the composable keeps the page number, page size, cursor and the
 * history map used to move backwards without refetching. Navigation is
 * ignored while the query is fetching or while no next cursor is available,
 * matching the previous per-page implementations.
 */
export interface CursorPaginationOptions {
  initialPageSize: number;
  /** Whether the owning query is currently fetching. */
  isFetching: () => boolean;
  /** The current page's next cursor, when the backend returned one. */
  nextCursor: () => string | undefined;
  /** Extra side effects after a successful page change (for example resetting dependent pagination). */
  onPageChanged?: () => void;
}

export interface CursorPagination {
  page: Ref<number>;
  pageSize: Ref<number>;
  cursor: Ref<string>;
  reset: () => void;
  changePage: (nextPage: number) => void;
  changePageSize: (value: number) => void;
}

export function useCursorPagination(options: CursorPaginationOptions): CursorPagination {
  const page = ref(1);
  const pageSize = ref(options.initialPageSize);
  const cursor = ref("");
  const cursorHistory = ref(new Map<number, string>());

  function reset(): void {
    page.value = 1;
    cursor.value = "";
    cursorHistory.value = new Map([[1, ""]]);
  }

  function changePage(nextPage: number): void {
    if (options.isFetching() || !Number.isSafeInteger(nextPage) || nextPage < 1 || nextPage === page.value) return;
    const next = new Map(cursorHistory.value);
    if (nextPage < page.value) {
      cursor.value = next.get(nextPage) ?? "";
    } else {
      const nextCursor = options.nextCursor();
      if (!nextCursor) return;
      next.set(nextPage, nextCursor);
      cursor.value = nextCursor;
    }
    cursorHistory.value = next;
    page.value = nextPage;
    options.onPageChanged?.();
  }

  function changePageSize(value: number): void {
    pageSize.value = value;
    reset();
  }

  return { page, pageSize, cursor, reset, changePage, changePageSize };
}
