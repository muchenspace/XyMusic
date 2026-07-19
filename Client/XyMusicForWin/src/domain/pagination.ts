export interface CursorPage<T> {
  items: T[];
  nextCursor: string | null;
}

export type TrackSort = "PUBLISHED_DESC" | "TITLE_ASC" | "TITLE_DESC";
export type AlbumSort = "RELEASE_DATE_DESC" | "TITLE_ASC" | "TITLE_DESC";
export type FavoriteSort = "FAVORITED_DESC" | "TITLE_ASC";
export type PlaylistSort = "UPDATED_DESC" | "NAME_ASC" | "NAME_DESC";
