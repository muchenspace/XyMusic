export type LibraryView = "discover" | "recent" | "favorites" | "playlists" | "settings" | "diagnostics";

export function libraryViewRequiresHomeFeed(view: LibraryView): boolean {
  return view === "discover";
}
