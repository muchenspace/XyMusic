/** 库导航视图标识：由展示层消费的导航概念。 */
export type LibraryView = "discover" | "recent" | "favorites" | "playlists" | "settings";

export function libraryViewRequiresHomeFeed(view: LibraryView): boolean {
  return view === "discover";
}
