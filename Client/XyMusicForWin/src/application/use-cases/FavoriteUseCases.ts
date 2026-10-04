import type { LibraryUseCases } from "./LibraryUseCases";

export type FavoriteOutcome =
  | { readonly kind: "success"; readonly favorite: boolean }
  | { readonly kind: "error"; readonly favorite: boolean; readonly cause: unknown }
  | { readonly kind: "superseded" };

/**
 * Serializes favorite writes per track so rapid toggles reach the server in
 * order, and keeps the latest user intent so an outdated response cannot
 * overwrite a newer one. Presentation applies optimistic projections and
 * decides how to surface the outcome.
 */
export class FavoriteUseCases {
  private readonly queues = new Map<string, Promise<void>>();
  private readonly intents = new Map<string, boolean>();

  constructor(private readonly library: LibraryUseCases) {}

  async setFavorite(trackId: string, favorite: boolean): Promise<FavoriteOutcome> {
    this.intents.set(trackId, favorite);
    const previous = this.queues.get(trackId) ?? Promise.resolve();
    const operation = previous.catch(() => undefined).then(() => this.library.favorite(trackId, favorite));
    this.queues.set(trackId, operation);
    try {
      await operation;
      return this.intents.get(trackId) === favorite
        ? { kind: "success", favorite }
        : { kind: "superseded" };
    } catch (cause) {
      return this.intents.get(trackId) === favorite
        ? { kind: "error", favorite, cause }
        : { kind: "superseded" };
    } finally {
      if (this.queues.get(trackId) === operation) this.queues.delete(trackId);
      if (this.intents.get(trackId) === favorite) this.intents.delete(trackId);
    }
  }
}
