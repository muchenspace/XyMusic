import fs from "node:fs";
import path from "node:path";
import process from "node:process";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright-core";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const artifacts = path.join(root, "artifacts");
fs.mkdirSync(artifacts, { recursive: true });

const appUrl = process.env.XYMUSIC_E2E_APP_URL ?? "http://127.0.0.1:1420";
const serverUrl = new URL(process.env.XYMUSIC_E2E_BASE_URL ?? "http://127.0.0.1:3102");
const username = requiredEnvironment("XYMUSIC_E2E_USERNAME");
const password = requiredEnvironment("XYMUSIC_E2E_PASSWORD");
const edgePath = process.env.XYMUSIC_E2E_EDGE_PATH
  ?? "C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe";
const avatarPath = process.env.XYMUSIC_E2E_AVATAR ?? path.join(root, "src", "assets", "brand-mark.png");
const testRegistrationDisabled = process.env.XYMUSIC_E2E_EXPECT_REGISTRATION_DISABLED === "1";
const playlistName = `win-e2e-${Date.now().toString(36)}`;
const mediaPlaylistName = `${playlistName}-media`;
let playedTrackTitle = "";
let playedArtistName = "";
let playedAlbumTitle = "";

const report = {
  completedSteps: [],
  currentStep: "startup",
  apiResponses: [],
  expectedFailures: [],
  unexpectedResponses: [],
  requestFailures: [],
  externalResponses: [],
  externalFinishedRequests: [],
  browserErrors: [],
  metrics: {},
};

const browser = await chromium.launch({ executablePath: edgePath, headless: true });
const context = await browser.newContext({
  viewport: { width: 1440, height: 900 },
  colorScheme: "light",
  locale: "zh-CN",
});
const page = await context.newPage();

page.on("pageerror", (error) => report.browserErrors.push(`pageerror: ${error.message}`));
page.on("console", (message) => {
  if (message.type() === "error") {
    const locationUrl = message.location().url;
    if (locationUrl) {
      const url = new URL(locationUrl);
      if (isExpectedFailure(url, expectedConsoleStatus(message.text()))) return;
    }
    const location = locationUrl ? ` (${safePath(locationUrl)})` : "";
    report.browserErrors.push(`console: ${message.text()}${location}`);
  }
});
page.on("requestfailed", (request) => {
  report.requestFailures.push(`${request.resourceType()} ${request.method()} ${safePath(request.url())}: ${request.failure()?.errorText ?? "failed"}`);
});
page.on("requestfinished", (request) => {
  const url = new URL(request.url());
  if (!isServerUrl(url) && url.origin !== new URL(appUrl).origin) {
    report.externalFinishedRequests.push(`${request.method()} ${url.origin}${url.pathname}`);
  }
});
page.on("response", (response) => {
  const url = new URL(response.url());
  if (isServerUrl(url)) {
    report.apiResponses.push(`${response.request().method()} ${url.pathname}${url.search} ${response.status()}`);
  }
  else if (url.origin !== new URL(appUrl).origin) {
    report.externalResponses.push(`${response.request().method()} ${url.origin}${url.pathname} ${response.status()}`);
  }
  if (response.status() >= 400) {
    const entry = `${response.status()} ${url.origin}${url.pathname}`;
    if (isExpectedFailure(url, response.status())) report.expectedFailures.push(entry);
    else report.unexpectedResponses.push(entry);
  }
});

try {
  await step("open-login", async () => {
    await page.goto(appUrl, { waitUntil: "networkidle" });
    await page.locator(".login-card").waitFor();
    await fillServer(serverUrl.hostname);
  });

  if (testRegistrationDisabled) {
    await step("registration-disabled-error", async () => {
      await page.locator(".auth-mode-switch button").nth(1).click();
      await page.locator('input[autocomplete="username"]').fill(`disabled_${Date.now().toString(36)}`);
      await page.locator('input[autocomplete="new-password"]').nth(0).fill(password);
      await page.locator('input[autocomplete="new-password"]').nth(1).fill(password);
      const response = waitForApi("POST", "/api/v1/auth/register");
      await page.locator("button.login-submit").click();
      assertStatus(await response, 403, "disabled registration");
      await page.locator(".login-error").waitFor();
      await page.locator(".auth-mode-switch button").nth(0).click();
    });
  }

  await step("login-success", () => login(username, password));

  await step("library-empty-and-list-views", async () => {
    const navItems = page.locator(".nav-item");
    for (const index of [1, 2]) {
      await navItems.nth(index).click();
      await page.waitForTimeout(200);
    }
  });

  await step("search-success", async () => {
    const searchTerm = `missing-${Date.now().toString(36)}`;
    const response = page.waitForResponse((candidate) => {
      const url = new URL(candidate.url());
      return isServerUrl(url)
        && url.pathname === "/api/v1/search"
        && url.searchParams.get("q") === searchTerm;
    }, { timeout: 20_000 });
    await page.locator(".search-field input").fill(searchTerm);
    assertStatus(await response, 200, "search");
    await page.waitForTimeout(300);
  });

  await step("playlist-create", async () => {
    await page.locator(".nav-label-row .icon-button").click();
    const editor = page.locator("#playlist-editor-form");
    await editor.waitFor();
    await editor.locator("input").fill(playlistName);
    await editor.locator("textarea").fill("Windows isolated E2E playlist");
    await editor.locator("select").selectOption("UNLISTED");
    const response = waitForApi("POST", "/api/v1/playlists");
    await page.locator('button[type="submit"][form="playlist-editor-form"]').click();
    assertStatus(await response, 201, "playlist create");
    await page.locator(".app-dialog").waitFor({ state: "hidden" });
  });

  await step("playlist-update", async () => {
    await page.locator(".playlist-heading").click();
    const card = playlistCard(playlistName);
    await card.waitFor({ timeout: 10_000 });
    await card.locator(".playlist-card-actions button").nth(1).click();
    const editor = page.locator("#playlist-editor-form");
    await editor.waitFor();
    await editor.locator("input").fill(`${playlistName}-edited`);
    const response = waitForApiPrefix("PATCH", "/api/v1/playlists/");
    await page.locator('button[type="submit"][form="playlist-editor-form"]').click();
    assertStatus(await response, 200, "playlist update");
    await page.locator(".app-dialog").waitFor({ state: "hidden" });
  });

  await step("playlist-delete", async () => {
    const card = playlistCard(`${playlistName}-edited`);
    page.once("dialog", (dialog) => dialog.accept());
    const response = waitForApiPrefix("DELETE", "/api/v1/playlists/");
    await card.locator(".playlist-card-actions button").nth(2).click();
    assertStatus(await response, 204, "playlist delete");
    await card.waitFor({ state: "detached" });
  });

  await step("playback-grant-media-and-history", async () => {
    await page.locator(".nav-item").nth(0).click();
    const cards = page.locator(".random-track-card");
    await cards.first().waitFor();
    if (await cards.count() < 2) throw new Error("media E2E requires at least two random tracks");
    playedTrackTitle = (await cards.first().locator(".random-track-copy strong").textContent())?.trim() ?? "";
    playedArtistName = (await cards.first().locator(".random-track-copy small").textContent())?.trim() ?? "";
    playedAlbumTitle = (await cards.first().locator(".random-track-copy > span").textContent())?.trim() ?? "";
    if (!playedTrackTitle || !playedArtistName || !playedAlbumTitle || playedAlbumTitle === "未知专辑") {
      throw new Error("first random track is missing searchable catalog metadata");
    }
    const grant = waitForApiPattern("POST", /^\/api\/v1\/tracks\/[^/]+\/playback$/);
    const media = waitForMediaResponse();
    const history = waitForApiPattern("PUT", /^\/api\/v1\/library\/history\/[^/]+$/);
    await cards.first().locator(".random-track-main").click();
    assertStatus(await grant, 200, "playback grant");
    assertStatusOneOf(await media, [200, 206], "media stream");
    assertStatusOneOf(await history, [200, 204], "playback history");
    await page.locator(".player-bar").waitFor({ timeout: 20_000 });
  });

  await step("player-pause-resume-and-next", async () => {
    const playButton = page.locator(".transport-buttons .play-button");
    const playingLabel = await playButton.getAttribute("aria-label");
    await playButton.click();
    await page.waitForFunction((label) => document.querySelector(".transport-buttons .play-button")?.getAttribute("aria-label") !== label, playingLabel);
    await playButton.click();
    await page.waitForFunction((label) => document.querySelector(".transport-buttons .play-button")?.getAttribute("aria-label") === label, playingLabel);

    const currentTitle = (await page.locator(".now-playing strong").textContent())?.trim() ?? "";
    const history = waitForApiPattern("PUT", /^\/api\/v1\/library\/history\/[^/]+$/);
    const startedAt = Date.now();
    await page.locator(".transport-buttons > button").nth(3).click();
    await page.waitForFunction((title) => {
      const current = document.querySelector(".now-playing strong")?.textContent?.trim();
      return Boolean(current && current !== title);
    }, currentTitle, { timeout: 10_000 });
    report.metrics.nextTrackUiMs = Date.now() - startedAt;
    assertStatusOneOf(await history, [200, 204], "next-track history");
  });

  await step("queue-and-lyrics", async () => {
    await page.locator(".player-extras > button").nth(2).click();
    const queue = page.locator(".queue-panel");
    await queue.waitFor();
    if (await queue.locator(".queue-item").count() < 2) throw new Error("playback queue did not retain the source list");
    await queue.locator("header .icon-button").nth(1).click();
    await queue.waitFor({ state: "hidden" });

    const lyricsResponse = waitForApiPattern("GET", /^\/api\/v1\/tracks\/[^/]+$/);
    await page.locator(".lyrics-button").click();
    assertStatus(await lyricsResponse, 200, "lyrics track detail");
    const lyrics = page.locator(".lyrics-view");
    await lyrics.waitFor();
    await lyrics.locator(".lyric-line").first().waitFor({ timeout: 10_000 });
    await lyrics.locator(".lyrics-now-playing").click();
    await lyrics.waitFor({ state: "hidden" });
  });

  await step("favorite-and-recent-history", async () => {
    const currentTitle = (await page.locator(".now-playing strong").textContent())?.trim() ?? "";
    const favorite = waitForApiPattern("PUT", /^\/api\/v1\/library\/favorites\/[^/]+$/);
    await page.locator(".player-extras > button").nth(0).click();
    assertStatusOneOf(await favorite, [200, 204], "favorite track");

    const favoritesPage = waitForApi("GET", "/api/v1/library/favorites");
    await page.locator(".nav-item").nth(2).click();
    assertStatus(await favoritesPage, 200, "favorites list");
    const favoriteRow = page.locator(".track-row-group .track-row").filter({ hasText: currentTitle });
    await favoriteRow.waitFor();
    const unfavorite = waitForApiPattern("DELETE", /^\/api\/v1\/library\/favorites\/[^/]+$/);
    await favoriteRow.locator(".track-actions button").nth(0).click();
    assertStatusOneOf(await unfavorite, [200, 204], "unfavorite track");
    await favoriteRow.waitFor({ state: "detached" });

    const historyPage = waitForApi("GET", "/api/v1/library/history");
    await page.locator(".nav-item").nth(1).click();
    assertStatus(await historyPage, 200, "history list");
    await page.locator(".track-row-group .track-row").first().waitFor();
  });

  await step("positive-search-album-and-artist-detail", async () => {
    const searchResponse = page.waitForResponse((candidate) => {
      const url = new URL(candidate.url());
      return isServerUrl(url) && url.pathname === "/api/v1/search" && url.searchParams.get("q") === playedTrackTitle;
    }, { timeout: 20_000 });
    await page.locator(".search-field input").fill(playedTrackTitle);
    assertStatus(await searchResponse, 200, "positive search");
    await page.locator(".track-row-group .track-row").first().waitFor();
    await page.locator(".search-field input").fill("");

    const albumSearchResponse = page.waitForResponse((candidate) => {
      const url = new URL(candidate.url());
      return isServerUrl(url) && url.pathname === "/api/v1/search" && url.searchParams.get("q") === playedAlbumTitle;
    }, { timeout: 20_000 });
    await page.locator(".search-field input").fill(playedAlbumTitle);
    assertStatus(await albumSearchResponse, 200, "album search");
    const albumCard = page.locator(".album-card").first();
    await albumCard.waitFor();
    const albumTracks = page.waitForResponse((candidate) => {
      const url = new URL(candidate.url());
      return isServerUrl(url) && url.pathname === "/api/v1/tracks" && url.searchParams.has("albumId");
    }, { timeout: 20_000 });
    await albumCard.locator(".album-card-main").click();
    assertStatus(await albumTracks, 200, "album tracks");
    await page.locator(".collection-header").waitFor();
    await page.locator(".collection-header .icon-button").click();
    await page.locator(".search-field input").fill("");

    const artistSearchResponse = page.waitForResponse((candidate) => {
      const url = new URL(candidate.url());
      return isServerUrl(url) && url.pathname === "/api/v1/search" && url.searchParams.get("q") === playedArtistName;
    }, { timeout: 20_000 });
    await page.locator(".search-field input").fill(playedArtistName);
    assertStatus(await artistSearchResponse, 200, "artist search");
    const artistCard = page.locator(".artist-card").first();
    await artistCard.waitFor();
    const artistTracks = page.waitForResponse((candidate) => {
      const url = new URL(candidate.url());
      return isServerUrl(url) && url.pathname === "/api/v1/tracks" && url.searchParams.has("artistId");
    }, { timeout: 20_000 });
    await artistCard.click();
    assertStatus(await artistTracks, 200, "artist tracks");
    await page.locator(".collection-header").waitFor();
    await page.locator(".collection-header .icon-button").click();
    await page.locator(".search-field input").fill("");
  });

  await step("playlist-add-reorder-remove", async () => {
    await page.locator(".nav-item").nth(0).click();
    await page.locator(".random-track-card").first().waitFor();
    await page.locator(".nav-label-row .icon-button").click();
    const editor = page.locator("#playlist-editor-form");
    await editor.waitFor();
    await editor.locator("input").fill(mediaPlaylistName);
    const create = waitForApi("POST", "/api/v1/playlists");
    await page.locator('button[type="submit"][form="playlist-editor-form"]').click();
    assertStatus(await create, 201, "media playlist create");
    await page.locator(".app-dialog").waitFor({ state: "hidden" });

    const cards = page.locator(".random-track-card");
    for (const index of [0, 1]) {
      await cards.nth(index).locator(".random-track-actions button").nth(1).click();
      const dialog = page.locator(".app-dialog");
      await dialog.waitFor();
      const add = waitForApiPattern("POST", /^\/api\/v1\/playlists\/[^/]+\/tracks$/);
      await dialog.locator(".dialog-list button").filter({ hasText: mediaPlaylistName }).click();
      assertStatusOneOf(await add, [200, 201], "add track to playlist");
      await dialog.waitFor({ state: "hidden" });
    }

    const detail = waitForApiPattern("GET", /^\/api\/v1\/playlists\/[^/]+$/);
    await page.locator(".playlist-link").filter({ hasText: mediaPlaylistName }).click();
    assertStatus(await detail, 200, "playlist detail");
    const rows = page.locator(".track-row-group .track-row");
    await rows.nth(1).waitFor();
    const reorder = waitForApiPattern("PATCH", /^\/api\/v1\/playlists\/[^/]+\/tracks\/order$/);
    await rows.nth(0).locator(".track-actions button").nth(1).click();
    assertStatus(await reorder, 200, "playlist reorder");

    const remove = waitForApiPattern("DELETE", /^\/api\/v1\/playlists\/[^/]+\/tracks\/[^/]+$/);
    await rows.nth(0).locator(".track-actions .danger-action").click();
    assertStatusOneOf(await remove, [200, 204], "remove playlist track");

    await page.locator(".playlist-heading").click();
    const card = playlistCard(mediaPlaylistName);
    await card.waitFor();
    page.once("dialog", (dialog) => dialog.accept());
    const deletion = waitForApiPrefix("DELETE", "/api/v1/playlists/");
    await card.locator(".playlist-card-actions button").nth(2).click();
    assertStatus(await deletion, 204, "media playlist delete");
  });

  await step("profile-update", async () => {
    await openSettings();
    const form = page.locator(".settings-card--profile");
    await form.locator("input[required]").fill("Windows E2E Updated");
    await form.locator("textarea").fill("Isolated end-to-end account");
    const response = waitForApi("PATCH", "/api/v1/users/me");
    await form.locator('button[type="submit"]').click();
    assertStatus(await response, 200, "profile update");
  });

  await step("avatar-upload", async () => {
    const form = page.locator(".settings-card--profile");
    const reservation = waitForApi("POST", "/api/v1/users/me/avatar/uploads");
    const completion = page.waitForResponse((candidate) => {
      const url = new URL(candidate.url());
      return candidate.request().method() === "POST"
        && url.origin === serverUrl.origin
        && /^\/api\/v1\/users\/me\/avatar\/uploads\/[^/]+\/complete$/.test(url.pathname);
    }, { timeout: 30_000 });
    await form.locator('input[type="file"]').setInputFiles(avatarPath);
    assertStatus(await reservation, 201, "avatar reservation");
    assertStatus(await completion, 200, "avatar completion");
  });

  await step("client-preferences", async () => {
    await selectSettingsCategory("playback");
    const playback = page.locator("#settings-category-panel");
    await playback.locator("select").nth(0).selectOption("LOSSLESS");
    await playback.locator("select").nth(1).selectOption("2");
    await playback.locator("select").nth(2).selectOption("true");

    await selectSettingsCategory("lyrics");
    await page.locator("#playback-lyrics-font-scale").fill("1.15");

    await selectSettingsCategory("system");
    await page.locator("#settings-category-panel select").first().selectOption("dark");
    await page.waitForTimeout(200);
    if (await page.locator("html").getAttribute("data-theme") !== "dark") {
      throw new Error("theme preference did not update the document");
    }
    await page.screenshot({ path: path.join(artifacts, "win-e2e-settings-dark.png"), fullPage: true });
  });

  await step("diagnostics-view", async () => {
    await page.locator(".sidebar-footer > .nav-item").nth(0).click();
    await page.locator(".diagnostics-intro").waitFor();
  });

  await step("server-switch", async () => {
    await openSettings("system");
    const form = page.locator("#settings-category-panel form.settings-card");
    await form.locator(".server-host input").fill("localhost");
    const response = waitForApi("POST", "/api/v1/auth/logout");
    page.once("dialog", (dialog) => dialog.accept());
    await form.locator('button[type="submit"]').click();
    assertStatus(await response, 204, "server switch revocation");
    await page.locator(".login-card").waitFor();
    if (await page.locator(".server-host input").inputValue() !== "localhost") {
      throw new Error("server switch was not persisted");
    }
  });

  await step("logout-current-device", async () => {
    await login(username, password);
    const response = waitForApi("POST", "/api/v1/auth/logout");
    await page.locator(".profile-logout").click();
    assertStatus(await response, 204, "logout");
    await page.locator(".login-card").waitFor();
  });

  await step("login-error", async () => {
    await page.locator('input[autocomplete="username"]').fill(`invalid_${Date.now().toString(36)}`);
    await page.locator('input[autocomplete="current-password"]').fill("wrong-password");
    const response = waitForApi("POST", "/api/v1/auth/login");
    await page.locator("button.login-submit").click();
    const result = await response;
    if (![401, 429].includes(result.status())) {
      throw new Error(`invalid login returned ${result.status()}, expected 401 or 429`);
    }
    await page.locator(".login-error").waitFor();
    if (result.status() === 429) {
      const retryAfter = Number(await result.headerValue("Retry-After"));
      await page.waitForTimeout((Number.isFinite(retryAfter) ? retryAfter : 1) * 1_000 + 100);
    }
  });

  await step("logout-all-devices", async () => {
    await login(username, password);
    await openSettings();
    const response = waitForApi("POST", "/api/v1/auth/logout-all");
    page.once("dialog", (dialog) => dialog.accept());
    await page.locator(".danger-zone .danger-button").click();
    assertStatus(await response, 204, "logout all");
    await page.locator(".login-card").waitFor();
  });

  await page.screenshot({ path: path.join(artifacts, "win-e2e-final-login.png"), fullPage: true });
  if (report.unexpectedResponses.length || report.requestFailures.length || report.browserErrors.length) {
    throw new Error("browser or network diagnostics contain unexpected failures");
  }
  writeReport(true);
} catch (error) {
  await page.screenshot({ path: path.join(artifacts, "win-e2e-failure.png"), fullPage: true }).catch(() => undefined);
  report.error = error instanceof Error ? error.message : String(error);
  writeReport(false);
  process.exitCode = 1;
} finally {
  await browser.close();
}

async function step(name, operation) {
  report.currentStep = name;
  await operation();
  report.completedSteps.push(name);
}

async function fillServer(host) {
  await page.locator(".server-host input").fill(host);
  await page.locator(".server-port input").fill(serverUrl.port || (serverUrl.protocol === "https:" ? "443" : "80"));
  await page.locator(".server-protocol select").selectOption(serverUrl.protocol.slice(0, -1));
}

async function login(name, secret) {
  const currentHost = await page.locator(".server-host input").inputValue();
  await fillServer(currentHost || serverUrl.hostname);
  await page.locator('input[autocomplete="username"]').fill(name);
  await page.locator('input[autocomplete="current-password"]').fill(secret);
  const response = waitForApi("POST", "/api/v1/auth/login");
  await page.locator("button.login-submit").click();
  assertStatus(await response, 200, "login");
  await page.locator(".sidebar").waitFor({ timeout: 20_000 });
  await page.locator(".page-content").waitFor({ state: "visible", timeout: 20_000 });
}

async function openSettings(category = "account") {
  await page.locator(".sidebar-footer > .nav-item").nth(1).click();
  await page.locator(".settings-view").waitFor();
  await selectSettingsCategory(category);
}

async function selectSettingsCategory(category) {
  const button = page.locator(`#settings-category-${category}`);
  await button.click();
  await page.waitForFunction((id) => document.getElementById(id)?.getAttribute("aria-current") === "page", `settings-category-${category}`);
}

function playlistCard(name) {
  return page.locator(".playlist-library-card").filter({ hasText: name });
}

function waitForApi(method, pathname) {
  return page.waitForResponse((candidate) => {
    const url = new URL(candidate.url());
    return candidate.request().method() === method
      && isServerUrl(url)
      && url.pathname === pathname;
  }, { timeout: 20_000 });
}

function waitForApiPrefix(method, pathnamePrefix) {
  return page.waitForResponse((candidate) => {
    const url = new URL(candidate.url());
    return candidate.request().method() === method
      && isServerUrl(url)
      && url.pathname.startsWith(pathnamePrefix);
  }, { timeout: 20_000 });
}

function waitForApiPattern(method, pathnamePattern) {
  return page.waitForResponse((candidate) => {
    const url = new URL(candidate.url());
    return candidate.request().method() === method
      && isServerUrl(url)
      && pathnamePattern.test(url.pathname);
  }, { timeout: 20_000 });
}

function waitForMediaResponse() {
  return page.waitForResponse((candidate) => {
    const url = new URL(candidate.url());
    return candidate.request().resourceType() === "media"
      && !isServerUrl(url)
      && [200, 206].includes(candidate.status());
  }, { timeout: 30_000 });
}

function assertStatus(response, expected, label) {
  if (response.status() !== expected) {
    throw new Error(`${label} returned ${response.status()}, expected ${expected}`);
  }
}

function assertStatusOneOf(response, expected, label) {
  if (!expected.includes(response.status())) {
    throw new Error(`${label} returned ${response.status()}, expected ${expected.join(" or ")}`);
  }
}

function isExpectedFailure(url, status) {
  return (testRegistrationDisabled && isServerUrl(url)
      && url.pathname === "/api/v1/auth/register" && status === 403)
    || (isServerUrl(url) && url.pathname === "/api/v1/auth/login" && [401, 429].includes(status));
}

function isServerUrl(url) {
  return url.protocol === serverUrl.protocol
    && effectivePort(url) === effectivePort(serverUrl)
    && (url.pathname.startsWith("/api/") || url.pathname.startsWith("/health/"));
}

function effectivePort(url) {
  return url.port || (url.protocol === "https:" ? "443" : "80");
}

function expectedConsoleStatus(message) {
  const match = /status of (\d+)/i.exec(message);
  return match ? Number(match[1]) : 0;
}

function safePath(value) {
  try {
    const url = new URL(value);
    return `${url.origin}${url.pathname}`;
  } catch {
    return "invalid-url";
  }
}

function requiredEnvironment(name) {
  const value = process.env[name]?.trim();
  if (!value) throw new Error(`${name} is required`);
  return value;
}

function writeReport(passed) {
  process.stdout.write(`${JSON.stringify({ passed, ...report }, null, 2)}\n`);
}
