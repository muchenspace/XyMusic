import { describe, expect, it } from "vitest";
import { libraryViewRequiresHomeFeed, type LibraryView } from "../src/domain/navigation";

describe("library view availability", () => {
  it("only gates discovery on the home feed", () => {
    const independentViews: LibraryView[] = [
      "recent",
      "favorites",
      "playlists",
      "settings",
      "diagnostics",
    ];

    expect(libraryViewRequiresHomeFeed("discover")).toBe(true);
    expect(independentViews.every((view) => !libraryViewRequiresHomeFeed(view))).toBe(true);
  });
});
