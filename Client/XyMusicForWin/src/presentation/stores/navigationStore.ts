import { computed, ref } from "vue";
import { defineStore } from "pinia";
import type { Album, Artist, Playlist } from "../../domain/music";
import type { LibraryView } from "../../domain/navigation";

export type NavigationEntry =
  | { id: number; kind: "library"; view: LibraryView; scrollTop: number }
  | { id: number; kind: "search"; query: string; scrollTop: number }
  | { id: number; kind: "album"; album: Album; sourceView: LibraryView; scrollTop: number }
  | { id: number; kind: "artist"; artist: Artist; sourceView: LibraryView; scrollTop: number }
  | { id: number; kind: "playlist"; playlist: Playlist; sourceView: LibraryView; scrollTop: number };

type NewEntry = NavigationEntry extends infer Entry
  ? Entry extends NavigationEntry
    ? Omit<Entry, "id" | "scrollTop"> & { scrollTop?: number }
    : never
  : never;

export const useNavigationStore = defineStore("navigation", () => {
  let nextId = 1;
  const entries = ref<NavigationEntry[]>([createEntry({ kind: "library", view: "discover" })]);
  const index = ref(0);

  const current = computed(() => entries.value[index.value]!);
  const canGoBack = computed(() => index.value > 0);
  const canGoForward = computed(() => index.value < entries.value.length - 1);
  const activeView = computed<LibraryView>(() => {
    const entry = current.value;
    if (entry.kind === "library") return entry.view;
    if (entry.kind === "search") return "discover";
    return entry.sourceView;
  });

  function push(entry: NewEntry): NavigationEntry {
    const created = createEntry(entry);
    entries.value.splice(index.value + 1, entries.value.length - index.value - 1, created);
    if (entries.value.length > MAX_NAVIGATION_ENTRIES) {
      entries.value.splice(0, entries.value.length - MAX_NAVIGATION_ENTRIES);
    }
    index.value = entries.value.length - 1;
    return created;
  }

  function replace(entry: NewEntry): NavigationEntry {
    const created = createEntry(entry);
    entries.value[index.value] = created;
    return created;
  }

  function showSearch(query: string): NavigationEntry {
    const trimmed = query.trim();
    if (current.value.kind === "search") {
      current.value.query = query;
      return current.value;
    }
    return push({ kind: "search", query: trimmed });
  }

  function back(): NavigationEntry | null {
    if (!canGoBack.value) return null;
    index.value -= 1;
    return current.value;
  }

  function forward(): NavigationEntry | null {
    if (!canGoForward.value) return null;
    index.value += 1;
    return current.value;
  }

  function rememberScroll(scrollTop: number): void {
    current.value.scrollTop = Math.max(0, scrollTop);
  }

  function reset(): void {
    nextId = 1;
    entries.value = [createEntry({ kind: "library", view: "discover" })];
    index.value = 0;
  }

  function createEntry(entry: NewEntry): NavigationEntry {
    return { ...entry, id: nextId++, scrollTop: entry.scrollTop ?? 0 } as NavigationEntry;
  }

  return { entries, index, current, canGoBack, canGoForward, activeView, push, replace, showSearch, back, forward, rememberScroll, reset };
});

const MAX_NAVIGATION_ENTRIES = 100;
