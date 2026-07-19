// Package admintagscraping implements the administrator Tag scraping API.
//
// Production composition creates one Repository, ProductionMusicPlatform,
// configured Fingerprinter, and AdminMediaArtworkApplier. The resulting
// Service is used by both Routes and BatchService. The application lifecycle
// must call BatchService.Start after dependencies are ready and Close before
// closing the database pool.
//
// EnqueueWriteback persists metadata_writeback_jobs. Processing those jobs is
// intentionally owned by the metadata writeback worker, not by this package.
package admintagscraping
