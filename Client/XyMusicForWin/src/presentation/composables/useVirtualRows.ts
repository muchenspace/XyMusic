import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch, type Ref } from "vue";

interface VirtualRowsOptions {
  rowHeight: number;
  threshold?: number;
  overscan?: number;
}

export function useVirtualRows(count: Readonly<Ref<number>>, listElement: Ref<HTMLElement | null>, options: VirtualRowsOptions) {
  const threshold = options.threshold ?? 80;
  const overscan = options.overscan ?? 8;
  const initialWindowSize = Math.max(1, overscan * 2 + 20);
  const rangeBuffer = Math.max(1, Math.floor(overscan / 2));
  const start = ref(0);
  const end = ref(count.value > threshold ? Math.min(count.value, initialWindowSize) : count.value);
  let scrollParent: HTMLElement | Window | null = null;
  let resizeObserver: ResizeObserver | null = null;
  let animationFrame: number | null = null;
  let mounted = false;
  let connectQueued = false;
  let scrollAnchorTop: number | null = null;
  let scrollViewportHeight = 0;

  const enabled = computed(() => count.value > threshold);
  const topSpacer = computed(() => enabled.value ? start.value * options.rowHeight : 0);
  const bottomSpacer = computed(() => enabled.value ? Math.max(0, (count.value - end.value) * options.rowHeight) : 0);

  function refresh(): void {
    if (!enabled.value) {
      cancelRefresh();
      measure(true);
      return;
    }
    if (animationFrame !== null) return;
    animationFrame = window.requestAnimationFrame(() => {
      animationFrame = null;
      measure();
    });
  }

  function measure(force = false): void {
    const element = listElement.value;
    if (!element) {
      updateRange(0, enabled.value ? Math.min(count.value, initialWindowSize) : count.value);
      return;
    }
    if (!enabled.value) {
      updateRange(0, count.value);
      return;
    }

    const totalHeight = count.value * options.rowHeight;
    const { visibleTop, visibleBottom } = visibleRange(element, totalHeight);
    const visibleStart = Math.max(0, Math.min(count.value - 1, Math.floor(visibleTop / options.rowHeight)));
    const visibleEnd = Math.min(count.value, Math.max(visibleStart + 1, Math.ceil(visibleBottom / options.rowHeight)));
    if (!force && !rangeNeedsUpdate(visibleStart, visibleEnd)) return;

    const nextStart = Math.max(0, Math.min(count.value - 1, Math.floor(visibleTop / options.rowHeight) - overscan));
    const nextEnd = Math.min(count.value, Math.max(nextStart + 1, Math.ceil(visibleBottom / options.rowHeight) + overscan));
    updateRange(nextStart, nextEnd);
  }

  function rangeNeedsUpdate(visibleStart: number, visibleEnd: number): boolean {
    const currentStart = start.value;
    const currentEnd = end.value;
    if (currentStart < 0 || currentEnd > count.value || currentStart >= currentEnd) return true;
    if (visibleStart < currentStart || visibleEnd > currentEnd) return true;
    return (currentStart > 0 && visibleStart < currentStart + rangeBuffer)
      || (currentEnd < count.value && visibleEnd > currentEnd - rangeBuffer);
  }

  function updateRange(nextStart: number, nextEnd: number): void {
    if (start.value !== nextStart) start.value = nextStart;
    if (end.value !== nextEnd) end.value = nextEnd;
  }

  function connect(): void {
    connectQueued = false;
    disconnect();
    if (!mounted) return;
    const element = listElement.value;
    if (!element || !enabled.value) {
      measure(true);
      return;
    }
    scrollParent = findScrollParent(element);
    scrollParent.addEventListener("scroll", refresh, { passive: true });
    window.addEventListener("resize", refreshAfterLayout, { passive: true });
    if (typeof ResizeObserver !== "undefined") {
      resizeObserver = new ResizeObserver(refreshAfterLayout);
      resizeObserver.observe(element);
      if (scrollParent instanceof HTMLElement) resizeObserver.observe(scrollParent);
    }
    measure(true);
  }

  function scheduleConnect(): void {
    if (connectQueued) return;
    connectQueued = true;
    void nextTick(() => {
      connectQueued = false;
      if (mounted) connect();
    });
  }

  function disconnect(): void {
    scrollParent?.removeEventListener("scroll", refresh);
    window.removeEventListener("resize", refreshAfterLayout);
    resizeObserver?.disconnect();
    resizeObserver = null;
    scrollParent = null;
    invalidateScrollMetrics();
    cancelRefresh();
  }

  function refreshAfterLayout(): void {
    invalidateScrollMetrics();
    refresh();
  }

  function invalidateScrollMetrics(): void {
    scrollAnchorTop = null;
    scrollViewportHeight = 0;
  }

  function visibleRange(element: HTMLElement, totalHeight: number): { visibleTop: number; visibleBottom: number } {
    if (scrollParent instanceof HTMLElement) {
      if (scrollAnchorTop === null) {
        const listRect = element.getBoundingClientRect();
        const viewport = scrollParent.getBoundingClientRect();
        scrollAnchorTop = scrollParent.scrollTop + listRect.top - viewport.top;
        scrollViewportHeight = scrollParent.clientHeight || Math.max(0, viewport.bottom - viewport.top);
      }
      const visibleTop = Math.max(0, scrollParent.scrollTop - scrollAnchorTop);
      return {
        visibleTop,
        visibleBottom: Math.max(0, Math.min(totalHeight, visibleTop + scrollViewportHeight)),
      };
    }

    const listRect = element.getBoundingClientRect();
    const viewport = viewportRect(scrollParent);
    return {
      visibleTop: Math.max(0, viewport.top - listRect.top),
      visibleBottom: Math.max(0, Math.min(totalHeight, viewport.bottom - listRect.top)),
    };
  }

  function cancelRefresh(): void {
    if (animationFrame !== null) window.cancelAnimationFrame(animationFrame);
    animationFrame = null;
  }

  onMounted(() => {
    mounted = true;
    scheduleConnect();
  });
  onBeforeUnmount(() => {
    mounted = false;
    disconnect();
  });
  watch(count, (nextCount, previousCount) => {
    const wasEnabled = previousCount > threshold;
    const isEnabled = nextCount > threshold;
    if (wasEnabled !== isEnabled) {
      if (!isEnabled) disconnect();
      scheduleConnect();
      return;
    }
    if (!isEnabled) {
      cancelRefresh();
      invalidateScrollMetrics();
      updateRange(0, nextCount);
      return;
    }
    if (!listElement.value) {
      cancelRefresh();
      invalidateScrollMetrics();
      updateRange(0, Math.min(nextCount, initialWindowSize));
      return;
    }
    invalidateScrollMetrics();
    void nextTick(refresh);
  });
  watch(listElement, scheduleConnect);

  return { enabled, start, end, topSpacer, bottomSpacer, refresh };
}

function findScrollParent(element: HTMLElement): HTMLElement | Window {
  let current = element.parentElement;
  while (current) {
    const overflowY = window.getComputedStyle(current).overflowY;
    if (overflowY === "auto" || overflowY === "scroll") return current;
    current = current.parentElement;
  }
  return window;
}

function viewportRect(scrollParent: HTMLElement | Window | null): { top: number; bottom: number } {
  if (scrollParent instanceof HTMLElement) {
    const rect = scrollParent.getBoundingClientRect();
    return { top: rect.top, bottom: rect.bottom };
  }
  return { top: 0, bottom: window.innerHeight };
}
