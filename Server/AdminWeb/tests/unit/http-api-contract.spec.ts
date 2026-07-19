import { beforeEach, describe, expect, it, vi } from "vitest";
import { apiRequest, openEventStream, uploadBinary } from "@/api/client";
import { adminApi } from "@/api/admin";
import { HttpAuthGateway } from "@/features/auth/infrastructure/http-auth-gateway";
import { HttpMediaUploadGateway } from "@/features/music/infrastructure/http-media-upload-gateway";
import { HttpSetupGateway, SETUP_COMPLETION_TIMEOUT_MS } from "@/features/setup/infrastructure/http-setup-gateway";
import { HttpTagScrapingGateway } from "@/features/scraping/infrastructure/http-tag-scraping-gateway";
import type { SetupCompleteInput } from "@/features/setup/domain/models";
import type {
  ApplyArtistArtworkInput,
  ApplyTagInput,
  ArtistCandidate,
  ArtistSearchInput,
  CreateArtistArtworkBatchInput,
  CreateBatchInput,
  TagSearchInput,
} from "@/features/scraping/domain/models";

vi.mock("@/api/client", () => ({
  apiRequest: vi.fn(() => Promise.resolve({})),
  openEventStream: vi.fn(() => ({ close: vi.fn() })),
  uploadBinary: vi.fn(() => Promise.resolve()),
}));

const requestMock = vi.mocked(apiRequest);
const eventStreamMock = vi.mocked(openEventStream);
const uploadMock = vi.mocked(uploadBinary);
const signal = new AbortController().signal;

async function expectRequest(
  invoke: () => unknown,
  path: string,
  options: Record<string, unknown>,
): Promise<void> {
  requestMock.mockClear();
  await invoke();
  expect(requestMock).toHaveBeenCalledTimes(1);
  expect(requestMock).toHaveBeenCalledWith(path, options);
}

describe("administrator HTTP API contract", () => {
  beforeEach(() => {
    requestMock.mockClear();
    eventStreamMock.mockClear();
    uploadMock.mockClear();
    localStorage.clear();
  });

  it("keeps the exported API surface explicit so new methods require contract coverage", () => {
    expect(Object.keys(adminApi).sort()).toEqual([
      "album", "albumDuplicates", "albums", "archiveTrack", "artists", "audit",
      "batchRestoreTracks", "browseSourceDirectories", "bulkUpdateTracks", "cancelJob", "cancelScan",
      "cancelWritebackJob", "createPermanentDeleteTracksJob", "createSource", "createUser", "dashboard", "deleteSource",
      "deleteTrackPermanently", "deleteUser", "job", "jobEvents", "jobs", "mergeAlbums",
      "permanentDeleteTracksJob", "publishTrack", "resetUserPassword", "restoreTagRevision", "restoreTrack", "restoreUser", "retryJob",
      "retryWritebackJob", "revokeUserSession", "scanEvents", "scanSource", "scans",
      "settings", "sourceProcessing", "sources", "systemInformation", "tagHistory",
      "testDatabase", "testLocalLibrary", "testMediaTools", "testStorage", "track",
      "trackMetadata", "tracks", "updateAlbum", "updateArtist", "updateSettings",
      "updateSource", "updateTrackMetadata", "updateUser", "user", "users",
      "writeTrackMetadata", "writebackJobs",
    ]);
    expect(publicMethods(HttpAuthGateway)).toEqual(["login", "logout", "session"]);
    expect(publicMethods(HttpSetupGateway)).toEqual(["complete", "status", "testAdministrator", "testDatabase", "testHttp", "testMedia", "testPaths", "testSource", "testStorage"]);
    expect(publicMethods(HttpTagScrapingGateway)).toEqual([
      "apply", "applyArtistArtwork", "artistArtworkBatch", "artworkUrl", "batch",
      "cancelArtistArtworkBatch", "cancelBatch", "createArtistArtworkBatch", "createBatch",
      "fingerprint", "retryArtistArtworkBatch", "retryBatch", "search", "searchArtists",
    ]);
    expect(publicMethods(HttpMediaUploadGateway)).toEqual(["complete", "reserve", "upload"]);
  });

  it("maps every management and source API method", async () => {
    await expectRequest(() => adminApi.dashboard(signal), "/api/v1/admin/dashboard", { signal });
    await expectRequest(() => adminApi.users({ page: 2, pageSize: 25, search: "mu", status: "ACTIVE", role: "ADMIN" }, signal), "/api/v1/admin/users", { query: { page: 2, pageSize: 25, query: "mu", status: "ACTIVE", role: "ADMIN" }, signal });
    await expectRequest(() => adminApi.user("user-1", { page: 2, pageSize: 10 }, signal), "/api/v1/admin/users/user-1", { query: { page: 2, pageSize: 10 }, signal });
    await expectRequest(() => adminApi.createUser({ username: "tester", displayName: "Tester", role: "USER", password: "secret1" }), "/api/v1/admin/users", { method: "POST", body: { username: "tester", displayName: "Tester", role: "USER", password: "secret1" } });
    await expectRequest(() => adminApi.updateUser("user-1", { expectedVersion: 2, displayName: "Updated", reason: "test" }), "/api/v1/admin/users/user-1", { method: "PATCH", body: { expectedVersion: 2, displayName: "Updated", reason: "test" } });
    await expectRequest(() => adminApi.resetUserPassword("user-1", 2, "secret2", "test"), "/api/v1/admin/users/user-1/password", { method: "POST", body: { expectedVersion: 2, password: "secret2", reason: "test" } });
    await expectRequest(() => adminApi.revokeUserSession("user-1", "session-1", "lost"), "/api/v1/admin/users/user-1/sessions/session-1/revoke", { method: "POST", body: { reason: "lost" } });
    await expectRequest(() => adminApi.deleteUser("user-1", 2, "cleanup"), "/api/v1/admin/users/user-1", { method: "DELETE", body: { expectedVersion: 2, reason: "cleanup" } });
    await expectRequest(() => adminApi.restoreUser("user-1", 3, "restore"), "/api/v1/admin/users/user-1/restore", { method: "POST", body: { expectedVersion: 3, reason: "restore" } });

    await expectRequest(() => adminApi.sources({ page: 2, pageSize: 12 }, signal), "/api/v1/admin/sources", { query: { page: 2, pageSize: 12 }, signal });
    const source = { name: "Music", path: "music", mode: "READ_ONLY" as const, enabled: true, scanOnStartup: false, scanIntervalMinutes: null, includePatterns: [], excludePatterns: [] };
    await expectRequest(() => adminApi.createSource(source), "/api/v1/admin/sources", { method: "POST", body: source });
    await expectRequest(() => adminApi.updateSource("source-1", { expectedVersion: 2, name: "Music 2" }), "/api/v1/admin/sources/source-1", { method: "PATCH", body: { expectedVersion: 2, name: "Music 2" } });
    await expectRequest(() => adminApi.deleteSource("source-1", 2, true), "/api/v1/admin/sources/source-1", { method: "DELETE", body: { expectedVersion: 2, archiveCatalog: true } });
    await expectRequest(() => adminApi.browseSourceDirectories("D:/Music", { page: 3, pageSize: 50 }, signal), "/api/v1/admin/sources/browse", { query: { path: "D:/Music", page: 3, pageSize: 50 }, signal });
    await expectRequest(() => adminApi.scanSource("source-1"), "/api/v1/admin/sources/source-1/scans", { method: "POST" });
    await expectRequest(() => adminApi.sourceProcessing("source-1", signal), "/api/v1/admin/sources/source-1/processing", { signal });
    await expectRequest(() => adminApi.scans("source-1", { page: 2, pageSize: 10 }, signal), "/api/v1/admin/sources/source-1/scans", { query: { page: 2, pageSize: 10 }, signal });
    await expectRequest(() => adminApi.cancelScan("source-1", "scan-1"), "/api/v1/admin/sources/source-1/scans/scan-1/cancel", { method: "POST" });
    adminApi.scanEvents("source-1", "scan-1");
    expect(eventStreamMock).toHaveBeenLastCalledWith("/api/v1/admin/sources/source-1/scans/scan-1/events");
  });

  it("maps every catalog and metadata API method", async () => {
    const listQuery = { page: 1, pageSize: 25, search: "song", sort: "updatedAt", order: "desc" as const };
    await expectRequest(() => adminApi.tracks({ ...listQuery, status: "READY", metadataStatus: "ORIGINAL", sourceId: "source-1" }, signal), "/api/v1/admin/tracks", { query: { ...listQuery, status: "READY", metadataStatus: "ORIGINAL", sourceId: "source-1" }, signal });
    await expectRequest(() => adminApi.track("track-1", signal), "/api/v1/admin/tracks/track-1", { signal });
    await expectRequest(() => adminApi.trackMetadata("track-1", signal), "/api/v1/admin/tracks/track-1/metadata", { signal });
    const metadataUpdate = { expectedVersion: 3, patch: { title: "Updated" }, resetFields: ["comment"], reason: "test" };
    await expectRequest(() => adminApi.updateTrackMetadata("track-1", metadataUpdate), "/api/v1/admin/tracks/track-1/metadata", { method: "PATCH", body: metadataUpdate });
    await expectRequest(() => adminApi.publishTrack("track-1", 3), "/api/v1/admin/tracks/track-1/publish", { method: "POST", body: { expectedVersion: 3 } });
    await expectRequest(() => adminApi.archiveTrack("track-1", 3), "/api/v1/admin/tracks/track-1/archive", { method: "POST", body: { expectedVersion: 3 } });
    await expectRequest(() => adminApi.restoreTrack("track-1", 3), "/api/v1/admin/tracks/track-1/restore", { method: "POST", body: { expectedVersion: 3 } });
    const mutationItems = [{ trackId: "track-1", expectedVersion: 3 }];
    await expectRequest(() => adminApi.batchRestoreTracks(mutationItems), "/api/v1/admin/tracks/batch/restore", { method: "POST", body: { items: mutationItems } });
    await expectRequest(() => adminApi.createPermanentDeleteTracksJob(mutationItems), "/api/v1/admin/tracks/batch/delete-permanently", { method: "POST", body: { items: mutationItems } });
    await expectRequest(() => adminApi.permanentDeleteTracksJob("delete-job-1", signal), "/api/v1/admin/tracks/batch/delete-permanently/delete-job-1", { signal });
    await expectRequest(() => adminApi.deleteTrackPermanently("track-1", 3), "/api/v1/admin/tracks/track-1", { method: "DELETE", body: { expectedVersion: 3 } });
    await expectRequest(() => adminApi.writeTrackMetadata("track-1", 3, "write"), "/api/v1/admin/tracks/track-1/metadata/writeback", { method: "POST", body: { expectedVersion: 3, reason: "write" } });
    await expectRequest(() => adminApi.tagHistory("track-1", { page: 2, pageSize: 10 }, signal), "/api/v1/admin/tracks/track-1/metadata/revisions", { query: { page: 2, pageSize: 10 }, signal });
    await expectRequest(() => adminApi.restoreTagRevision("track-1", "revision-1", 3, "restore"), "/api/v1/admin/tracks/track-1/metadata/revisions/revision-1/restore", { method: "POST", body: { expectedVersion: 3, reason: "restore" } });
    await expectRequest(() => adminApi.bulkUpdateTracks([{ trackId: "track-1", expectedVersion: 3 }], { genres: ["Rock"] }, "batch"), "/api/v1/admin/metadata/batch", { method: "POST", body: { items: [{ trackId: "track-1", expectedVersion: 3 }], patch: { genres: ["Rock"] }, reason: "batch" } });

    await expectRequest(() => adminApi.albums(listQuery, signal), "/api/v1/admin/albums", { query: listQuery, signal });
    await expectRequest(() => adminApi.albumDuplicates({ page: 2, pageSize: 10, albumId: "album-1", albumPage: 3, albumPageSize: 50 }, signal), "/api/v1/admin/albums/duplicates", { query: { page: 2, pageSize: 10, albumId: "album-1", albumPage: 3, albumPageSize: 50 }, signal });
    await expectRequest(() => adminApi.album("album-1", { page: 3, pageSize: 25 }, signal), "/api/v1/admin/albums/album-1", { query: { page: 3, pageSize: 25 }, signal });
    const albumUpdate = { expectedVersion: 2, title: "Album", releaseDate: null, description: null };
    await expectRequest(() => adminApi.updateAlbum("album-1", albumUpdate), "/api/v1/admin/albums/album-1", { method: "PATCH", body: albumUpdate });
    const merge = { target: { albumId: "album-1", expectedVersion: 2 }, sources: [{ albumId: "album-2", expectedVersion: 1 }], fieldSources: { title: "album-1", cover: null, artistCredits: "album-1", releaseDate: null, description: null } };
    await expectRequest(() => adminApi.mergeAlbums(merge), "/api/v1/admin/albums/merge", { method: "POST", body: merge });
    await expectRequest(() => adminApi.artists(listQuery, signal), "/api/v1/admin/artists", { query: listQuery, signal });
    const artistUpdate = { expectedVersion: 2, name: "Artist", description: null };
    await expectRequest(() => adminApi.updateArtist("artist-1", artistUpdate), "/api/v1/admin/artists/artist-1", { method: "PATCH", body: artistUpdate });
  });

  it("maps every jobs, audit, settings, and system API method", async () => {
    const jobsQuery = { page: 2, pageSize: 20, search: "scan", status: "FAILED", type: "SOURCE_SCAN", sort: "createdAt", order: "desc" as const };
    await expectRequest(() => adminApi.jobs(jobsQuery, signal), "/api/v1/admin/jobs", { query: jobsQuery, signal });
    await expectRequest(() => adminApi.job("job-1", signal), "/api/v1/admin/jobs/job-1", { signal });
    await expectRequest(() => adminApi.retryJob("job-1"), "/api/v1/admin/jobs/job-1/retry", { method: "POST" });
    await expectRequest(() => adminApi.cancelJob("job-1"), "/api/v1/admin/jobs/job-1/cancel", { method: "POST" });
    adminApi.jobEvents();
    expect(eventStreamMock).toHaveBeenLastCalledWith("/api/v1/admin/jobs/events");
    await expectRequest(() => adminApi.writebackJobs({ page: 1, pageSize: 10, status: "FAILED", trackId: "track-1" }, signal), "/api/v1/admin/metadata/writeback-jobs", { query: { page: 1, pageSize: 10, status: "FAILED", trackId: "track-1" }, signal });
    await expectRequest(() => adminApi.retryWritebackJob("write-1", 2, "retry"), "/api/v1/admin/metadata/writeback-jobs/write-1/retry", { method: "POST", body: { expectedVersion: 2, reason: "retry" } });
    await expectRequest(() => adminApi.cancelWritebackJob("write-1", 2, "cancel"), "/api/v1/admin/metadata/writeback-jobs/write-1/cancel", { method: "POST", body: { expectedVersion: 2, reason: "cancel" } });

    const auditQuery = { page: 1, pageSize: 25, search: "user", action: "USER_UPDATE", result: "SUCCESS", actorId: "actor-1", from: "2026-01-01", to: "2026-12-31", sort: "createdAt", order: "desc" as const };
    await expectRequest(() => adminApi.audit(auditQuery, signal), "/api/v1/admin/audit", { query: auditQuery, signal });
    await expectRequest(() => adminApi.settings(signal), "/api/v1/admin/settings", { signal });
    const settingsUpdate = { expectedVersion: 2, registration: { enabled: false } };
    await expectRequest(() => adminApi.updateSettings(settingsUpdate), "/api/v1/admin/settings", { method: "PATCH", body: settingsUpdate });
    const database = { host: "db", port: 5432, database: "xymusic", username: "admin", sslMode: "prefer" as const, maximumConnections: 10 };
    await expectRequest(() => adminApi.testDatabase(database), "/api/v1/admin/settings/test/database", { method: "POST", body: database });
    const storage = { endpoint: "http://minio:9000", publicBaseUrl: null, region: "us-east-1", bucket: "xymusic", accessKeyId: "key", forcePathStyle: true, signedUrlTtlSeconds: 300, maxUploadBytes: 1024 };
    await expectRequest(() => adminApi.testStorage(storage), "/api/v1/admin/settings/test/storage", { method: "POST", body: storage });
    await expectRequest(() => adminApi.testMediaTools({ directory: "tools" }), "/api/v1/admin/settings/test/media-tools", { method: "POST", body: { directory: "tools" } });
    await expectRequest(() => adminApi.testLocalLibrary("music"), "/api/v1/admin/settings/test/local-library", { method: "POST", body: { directory: "music" } });
    await expectRequest(() => adminApi.systemInformation(signal), "/api/v1/admin/system", { signal });
  });

  it("maps auth, setup, scraping, and binary media gateways", async () => {
    const auth = new HttpAuthGateway();
    await expectRequest(() => auth.session(signal), "/api/v1/admin/auth/session", { signal });
    requestMock.mockClear();
    await auth.login("admin", "secret1");
    expect(requestMock).toHaveBeenCalledWith("/api/v1/admin/auth/login", {
      method: "POST",
      body: {
        username: "admin",
        password: "secret1",
        installationId: expect.stringMatching(/^[0-9a-f-]{36}$/i),
        deviceName: "Web administration console",
      },
    });
    await expectRequest(() => auth.logout(), "/api/v1/admin/auth/logout", { method: "POST" });

    const setup = new HttpSetupGateway();
    const completeInput = setupInput();
    await expectRequest(() => setup.status(signal), "/api/setup/status", { signal });
    await expectRequest(() => setup.testHttp(completeInput.http), "/api/setup/http/test", { method: "POST", body: completeInput.http });
    await expectRequest(() => setup.testPaths({ migrationsDirectory: " migrations ", adminWebDirectory: " admin " }), "/api/setup/paths/test", { method: "POST", body: { migrationsDirectory: "migrations", adminWebDirectory: "admin" } });
    await expectRequest(() => setup.testDatabase({ database: completeInput.database, migrationsDirectory: "migrations" }), "/api/setup/database/test", { method: "POST", body: { database: completeInput.database, migrationsDirectory: "migrations" } });
    await expectRequest(() => setup.testStorage({ ...completeInput.storage, publicBaseUrl: " " }), "/api/setup/storage/test", { method: "POST", body: completeInput.storage });
    await expectRequest(() => setup.testMedia(completeInput.media), "/api/setup/media/test", { method: "POST", body: { ffmpegPath: "ffmpeg", ffprobePath: "ffprobe" } });
    await expectRequest(() => setup.testSource({ ...completeInput.source, scanIntervalMinutes: null }), "/api/setup/source/test", { method: "POST", body: { ...completeInput.source, scanIntervalMinutes: null } });
    await expectRequest(() => setup.testAdministrator(completeInput.administrator), "/api/setup/administrator/test", { method: "POST", body: completeInput.administrator });
    await expectRequest(() => setup.complete(completeInput), "/api/setup/complete", { method: "POST", timeoutMs: SETUP_COMPLETION_TIMEOUT_MS, body: { ...completeInput, media: { ffmpegPath: "ffmpeg", ffprobePath: "ffprobe" } } });

    const scraping = new HttpTagScrapingGateway();
    const search: TagSearchInput = { source: "smart", query: "song" };
    const candidate = { id: "remote-1", name: "Song", artist: "Artist", artistId: "a", album: "Album", albumId: "b", albumImg: "https://img.example/1.jpg", year: "2026", track: "1", disc: "1", genre: "Pop", source: "qmusic" as const };
    const fields = { title: true, artist: true, album: true, year: true, genre: true, lyrics: true, cover: true, overwrite: false };
    const apply: ApplyTagInput = { expectedVersion: 2, candidate, fields, writeBack: false, reason: "test" };
    const batch: CreateBatchInput = { items: [{ trackId: "track-1", expectedVersion: 2 }], options: { sources: ["qmusic"], matchMode: "strict", missingFields: [], fields, writeBack: false, reason: "batch" } };
    await expectRequest(() => scraping.search(search, signal), "/api/v1/admin/tag-scraping/search", { method: "POST", body: search, signal });
    await expectRequest(() => scraping.fingerprint("track-1", signal), "/api/v1/admin/tag-scraping/tracks/track-1/fingerprint", { method: "POST", signal });
    await expectRequest(() => scraping.apply("track-1", apply), "/api/v1/admin/tag-scraping/tracks/track-1/apply", { method: "POST", body: apply });
    await expectRequest(() => scraping.createBatch(batch), "/api/v1/admin/tag-scraping/batches", { method: "POST", body: batch });
    await expectRequest(() => scraping.batch("batch-1", "2026-01-01T00:00:00Z", signal), "/api/v1/admin/tag-scraping/batches/batch-1", { query: { updatedAfter: "2026-01-01T00:00:00Z" }, signal });
    await expectRequest(() => scraping.cancelBatch("batch-1"), "/api/v1/admin/tag-scraping/batches/batch-1/cancel", { method: "POST" });
    await expectRequest(() => scraping.retryBatch("batch-1"), "/api/v1/admin/tag-scraping/batches/batch-1/retry", { method: "POST" });
    const artistSearch: ArtistSearchInput = { source: "smart", query: "Artist", sources: ["qmusic", "netease"] };
    const artistCandidate: ArtistCandidate = { id: "artist-remote-1", name: "Artist", imageUrl: "https://img.example/artist.jpg", aliases: ["Alias"], source: "qmusic", score: 1.5 };
    const artistApply: ApplyArtistArtworkInput = { expectedVersion: 3, candidate: artistCandidate, overwrite: true, reason: "test artist artwork" };
    const artistBatch: CreateArtistArtworkBatchInput = { items: [{ artistId: "artist-1", expectedVersion: 3 }], options: { sources: ["qmusic", "netease"], overwrite: false, reason: "batch artist artwork" } };
    await expectRequest(() => scraping.searchArtists(artistSearch, signal), "/api/v1/admin/tag-scraping/artists/search", { method: "POST", body: artistSearch, signal });
    await expectRequest(() => scraping.applyArtistArtwork("artist-1", artistApply), "/api/v1/admin/tag-scraping/artists/artist-1/apply", { method: "POST", body: artistApply });
    await expectRequest(() => scraping.createArtistArtworkBatch(artistBatch), "/api/v1/admin/tag-scraping/artists/batches", { method: "POST", body: artistBatch });
    await expectRequest(
      () => scraping.artistArtworkBatch("artist-batch-1", "2026-01-01T00:00:00Z", signal),
      "/api/v1/admin/tag-scraping/artists/batches/artist-batch-1",
      { query: { updatedAfter: "2026-01-01T00:00:00Z" }, signal },
    );
    await expectRequest(() => scraping.cancelArtistArtworkBatch("artist-batch-1"), "/api/v1/admin/tag-scraping/artists/batches/artist-batch-1/cancel", { method: "POST" });
    await expectRequest(() => scraping.retryArtistArtworkBatch("artist-batch-1"), "/api/v1/admin/tag-scraping/artists/batches/artist-batch-1/retry", { method: "POST" });
    expect(scraping.artworkUrl("https://img.example/a b.jpg")).toBe("/api/v1/admin/tag-scraping/artwork?url=https%3A%2F%2Fimg.example%2Fa%20b.jpg");

    const media = new HttpMediaUploadGateway();
    const reservation = { purpose: "ALBUM_ARTWORK" as const, targetId: "album-1", fileName: "cover.png", contentType: "image/png", sizeBytes: 4, checksumSha256: "a".repeat(64) };
    await expectRequest(() => media.reserve(reservation), "/api/v1/admin/media/uploads", { method: "POST", body: reservation });
    const file = new File(["test"], "cover.png", { type: "image/png" });
    const progress = vi.fn();
    await media.upload("upload-1", file, "image/png", progress, signal);
    expect(uploadMock).toHaveBeenCalledWith("/api/v1/admin/media/uploads/upload-1/content", file, { contentType: "image/png", onProgress: progress, signal });
    await expectRequest(() => media.complete("upload-1"), "/api/v1/admin/media/uploads/upload-1/complete", { method: "POST", body: {} });
  });
});

function setupInput(): SetupCompleteInput {
  return {
    http: { ipv4Host: "127.0.0.1", ipv4Port: 3000, ipv6Host: "::1", ipv6Port: 3000, trustedProxyAddresses: [] },
    paths: { migrationsDirectory: "migrations", adminWebDirectory: "admin" },
    database: { host: "db", port: 5432, database: "xymusic", username: "admin", password: "secret", sslMode: "prefer", maxConnections: 10 },
    storage: { endpoint: "http://minio:9000", region: "us-east-1", bucket: "xymusic", accessKeyId: "key", secretAccessKey: "secret", forcePathStyle: true, signedUrlTtlSeconds: 300, maxUploadBytes: 1024 },
    media: { mode: "ADVANCED", directory: "", ffmpegPath: "ffmpeg", ffprobePath: "ffprobe", fpcalcPath: "", acoustIdClient: "" },
    source: { name: "Music", directory: "music", mode: "READ_ONLY", enabled: true, syncOnStartup: true, scanIntervalMinutes: null, includePatterns: [], excludePatterns: [] },
    registration: { enabled: false },
    administrator: { username: "admin", displayName: "Admin", password: "secret1" },
  };
}

function publicMethods(value: abstract new (...args: never[]) => unknown): string[] {
  return Object.getOwnPropertyNames(value.prototype).filter((name) => name !== "constructor").sort();
}
