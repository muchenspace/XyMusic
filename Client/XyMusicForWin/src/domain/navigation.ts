export type LibraryView = "discover" | "recent" | "favorites" | "playlists" | "settings";

export function libraryViewRequiresHomeFeed(view: LibraryView): boolean {
  return view === "discover";
}
