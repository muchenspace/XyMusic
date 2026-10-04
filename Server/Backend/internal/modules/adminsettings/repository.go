package adminsettings

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"xymusic/server/internal/shared/apperror"
)

// Repository owns the SQL executed by the administrator settings module. SQL
// statements and parameter order are kept identical to the previous inline
// implementation. It is bound to the active runtime database; operations on a
// candidate database receive it explicitly.
type Repository struct {
	database Database
}

func NewRepository(database Database) *Repository { return &Repository{database: database} }

func (repository *Repository) ServerVersion(ctx context.Context) (string, error) {
	var databaseVersion string
	if err := repository.database.QueryRow(ctx, "select current_setting('server_version')").Scan(&databaseVersion); err != nil {
		return "", err
	}
	return databaseVersion, nil
}

func (repository *Repository) MigrationInformation(ctx context.Context) string {
	var count int
	var latest *string
	err := repository.database.QueryRow(ctx, `
		select count(*)::int, max(created_at)::text from drizzle.__drizzle_migrations
	`).Scan(&count, &latest)
	if err != nil {
		return "not initialized"
	}
	if latest == nil {
		return fmt.Sprintf("%d applied", count)
	}
	return fmt.Sprintf("%d applied, latest %s", count, *latest)
}

func (repository *Repository) QueueInformation(ctx context.Context) (QueueDTO, error) {
	result := QueueDTO{}
	err := repository.database.QueryRow(ctx, `
		select
			(select count(*)::int from library_scan_runs where status in ('PENDING','RUNNING')),
			(select count(*)::int from metadata_writeback_jobs where status in ('PENDING','PROCESSING')),
			(
				(select count(*)::int from tag_scraping_jobs where status in ('PENDING','RUNNING')) +
				(select count(*)::int from artist_artwork_scraping_jobs where status in ('PENDING','RUNNING'))
			)
	`).Scan(&result.Scans, &result.Writeback, &result.Scraping)
	if err != nil {
		return QueueDTO{}, err
	}
	result.Total = result.Media + result.Scans + result.Cleanup + result.Writeback + result.Scraping
	return result, nil
}

func (repository *Repository) RequireAdministrator(ctx context.Context, database Database, actorID string) error {
	var id string
	err := database.QueryRow(ctx, `
		select id from users where id=$1 and role='ADMIN' and status='ACTIVE' limit 1
	`, actorID).Scan(&id)
	if err == nil {
		return nil
	}
	var databaseError *pgconn.PgError
	if errors.As(err, &databaseError) && (databaseError.Code == "42P01" || databaseError.Code == "42703") {
		return apperror.Conflict(apperror.CodeResourceConflict, "The target database is not an initialized compatible XyMusic database", nil)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return apperror.Conflict(apperror.CodeResourceConflict, "The target database does not contain the current active administrator", nil)
	}
	return err
}
