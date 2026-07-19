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
  const start = ref(0);
  const end = ref(count.value > threshold ? Math.min(count.value, initialWindowSize) : count.value);
  let scrollParent: HTMLElement | Window | null = null;
  let resizeObserver: ResizeObserver | null = null;
  let animationFrame: number | null = null;

  const enabled = computed(() => count.value > threshold);
  const topSpacer = computed(() => enabled.value ? start.value * options.rowHeight : 0);
  const bottomSpacer = computed(() => enabled.value ? Math.max(0, (count.value - end.value) * options.rowHeight) : 0);

  function refresh(): void {
    if (animationFrame !== null) return;
    animationFrame = window.requestAnimationFrame(() => {
      animationFrame = null;
      measure();
    });
  }

  function measure(): void {
    const element = listElement.value;
    if (!element) {
      updateRange(0, enabled.value ? Math.min(count.value, initialWindowSize) : count.value);
      return;
    }
    if (!enabled.value) {
      updateRange(0, count.value);
      return;
    }

    const listRect = element.getBoundingClientRect();
    const viewport = viewportRect(scrollParent);
    const totalHeight = count.value * options.rowHeight;
    const visibleTop = Math.max(0, viewport.top - listRect.top);
    const visibleBottom = Math.max(0, Math.min(totalHeight, viewport.bottom - listRect.top));
    const nextStart = Math.max(0, Math.min(count.value - 1, Math.floor(visibleTop / options.rowHeight) - overscan));
    const nextEnd = Math.min(count.value, Math.max(nextStart + 1, Math.ceil(visibleBottom / options.rowHeight) + overscan));
    updateRange(nextStart, nextEnd);
  }

  function updateRange(nextStart: number, nextEnd: number): void {
    if (start.value !== nextStart) start.value = nextStart;
    if (end.value !== nextEnd) end.value = nextEnd;
  }

  function connect(): void {
    disconnect();
    const element = listElement.value;
    if (!element) return;
    scrollParent = findScrollParent(element);
    scrollParent.addEventListener("scroll", refresh, { passive: true });
    window.addEventListener("resize", refresh, { passive: true });
    if (typeof ResizeObserver !== "undefined") {
      resizeObserver = new ResizeObserver(refresh);
      resizeObserver.observe(element);
      if (scrollParent instanceof HTMLElement) resizeObserver.observe(scrollParent);
    }
    measure();
  }

  function disconnect(): void {
    scrollParent?.removeEventListener("scroll", refresh);
    window.removeEventListener("resize", refresh);
    resizeObserver?.disconnect();
    resizeObserver = null;
    scrollParent = null;
    if (animationFrame !== null) window.cancelAnimationFrame(animationFrame);
    animationFrame = null;
  }

  onMounted(() => void nextTick(connect));
  onBeforeUnmount(disconnect);
  watch(count, () => void nextTick(refresh));
  watch(listElement, () => void nextTick(connect));

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
