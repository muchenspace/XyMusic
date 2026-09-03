package setup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"golang.org/x/text/unicode/norm"

	"xymusic/server/internal/config"
	"xymusic/server/internal/platform/database"
	"xymusic/server/internal/platform/security"
	"xymusic/server/internal/shared/apperror"
)

var ErrInvalidConfiguration = errors.New("existing setup configuration is invalid")

type FileConfigurationRepository struct {
	store *config.Store
}

func NewFileConfigurationRepository(path string) *FileConfigurationRepository {
	return &FileConfigurationRepository{store: config.NewStore(path)}
}

func (repository *FileConfigurationRepository) Load(_ context.Context) (config.Config, bool, error) {
	configured, err := repository.store.Load()
	if errors.Is(err, config.ErrNotConfigured) {
		return config.Config{}, false, nil
	}
	if err != nil {
		return config.Config{}, false, fmt.Errorf("%w: %v", ErrInvalidConfiguration, err)
	}
	return configured, true, nil
}

func (repository *FileConfigurationRepository) Save(_ context.Context, candidate config.Config) error {
	return repository.store.Save(candidate)
}

func (repository *FileConfigurationRepository) Clear(_ context.Context) error {
	return repository.store.Clear()
}

type ProductionDatabaseFactory struct{}

func (ProductionDatabaseFactory) Open(ctx context.Context, cfg config.Database) (InstallationDatabase, error) {
	pool, err := database.Open(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return &productionInstallationDatabase{Pool: pool}, nil
}

type productionInstallationDatabase struct {
	*database.Pool
}

func (connection *productionInstallationDatabase) CanCreateInCurrentSchema(ctx context.Context) (bool, error) {
	var allowed bool
	err := connection.QueryRow(ctx,
		"select has_schema_privilege(current_user, current_schema(), 'CREATE')",
	).Scan(&allowed)
	return allowed, err
}

func (connection *productionInstallationDatabase) CheckMigrationCompatibility(ctx context.Context, directory string) error {
	return database.CheckMigrationCompatibility(ctx, connection.Pool.Pool, directory)
}

func (connection *productionInstallationDatabase) RunMigrations(ctx context.Context, directory string) error {
	return database.RunMigrations(ctx, connection.Pool.Pool, directory)
}

func (connection *productionInstallationDatabase) Inspect(ctx context.Context, migrationsDirectory string) (InstallationInspection, error) {
	inspection := InstallationInspection{State: DatabaseStateEmpty, Reusable: []string{}, Missing: []string{}}
	available, err := database.ReadMigrations(migrationsDirectory)
	if err != nil {
		return InstallationInspection{}, fmt.Errorf("read migrations for inspection: %w", err)
	}
	var migrationRelation *string
	if err := connection.QueryRow(ctx, "select to_regclass('drizzle.__drizzle_migrations')::text").Scan(&migrationRelation); err != nil {
		return InstallationInspection{}, fmt.Errorf("inspect migration journal: %w", err)
	}
	appliedCount := 0
	if migrationRelation != nil {
		if err := connection.QueryRow(ctx, "select count(*)::int from drizzle.__drizzle_migrations").Scan(&appliedCount); err != nil {
			return InstallationInspection{}, fmt.Errorf("count applied migrations: %w", err)
		}
		inspection.State = DatabaseStatePartial
	}
	inspection.MigrationRequired = appliedCount < len(available)
	usersExists, err := connection.tableExists(ctx, "users")
	if err != nil {
		return InstallationInspection{}, fmt.Errorf("inspect users table: %w", err)
	}
	if !usersExists {
		inspection.Missing = append(inspection.Missing, "administrator", "librarySource")
		return inspection, nil
	}
	inspection.State = DatabaseStatePartial
	if err := connection.QueryRow(ctx, `
		select
			exists(select 1 from users where role = 'ADMIN'),
			exists(select 1 from users where role = 'ADMIN' and status = 'ACTIVE'),
			exists(select 1 from users)
	`).Scan(&inspection.HasAdministrator, &inspection.HasActiveAdministrator, &inspection.HasData); err != nil {
		return InstallationInspection{}, fmt.Errorf("inspect existing users: %w", err)
	}
	if inspection.HasActiveAdministrator {
		inspection.Reusable = append(inspection.Reusable, "administrator")
	} else {
		inspection.Missing = append(inspection.Missing, "administrator")
	}

	hasLibrarySource := false
	for _, item := range []struct {
		table string
		code  string
	}{
		{table: "library_roots", code: "librarySource"},
		{table: "tracks", code: "catalog"},
		{table: "playlists", code: "playlists"},
	} {
		exists, inspectErr := connection.tableHasRows(ctx, item.table)
		if inspectErr != nil {
			return InstallationInspection{}, fmt.Errorf("inspect %s data: %w", item.table, inspectErr)
		}
		if exists {
			inspection.HasData = true
			inspection.Reusable = append(inspection.Reusable, item.code)
			if item.code == "librarySource" {
				hasLibrarySource = true
			}
		} else if item.code == "librarySource" {
			inspection.Missing = append(inspection.Missing, item.code)
		}
	}
	if inspection.HasActiveAdministrator && hasLibrarySource {
		inspection.State = DatabaseStateComplete
	}
	return inspection, nil
}

func (connection *productionInstallationDatabase) tableExists(ctx context.Context, table string) (bool, error) {
	var relation *string
	if err := connection.QueryRow(ctx, "select to_regclass($1)::text", "public."+table).Scan(&relation); err != nil {
		return false, err
	}
	return relation != nil, nil
}

func (connection *productionInstallationDatabase) tableHasRows(ctx context.Context, table string) (bool, error) {
	exists, err := connection.tableExists(ctx, table)
	if err != nil || !exists {
		return false, err
	}
	var hasRows bool
	query := "select exists(select 1 from " + pgx.Identifier{table}.Sanitize() + ")"
	if err := connection.QueryRow(ctx, query).Scan(&hasRows); err != nil {
		return false, err
	}
	return hasRows, nil
}

func (connection *productionInstallationDatabase) Reset(ctx context.Context) error {
	const statement = `truncate table
		tag_scraping_job_items, tag_scraping_jobs,
		metadata_writeback_jobs, track_metadata,
		library_scan_runs, local_music_source_tracks, library_roots, local_music_sources,
		media_uploads, play_history, favorite_tracks,
		playlist_tracks, playlists, lyrics, track_artists, tracks,
		album_artists, albums, artists, user_profiles, media_assets,
		idempotency_records, rate_limit_buckets, refresh_tokens, auth_sessions, users
		restart identity cascade`
	if _, err := connection.Exec(ctx, statement); err != nil {
		return fmt.Errorf("clear XyMusic database data: %w", err)
	}
	return nil
}

func (connection *productionInstallationDatabase) Provision(
	ctx context.Context,
	input ProvisionInput,
) (ProvisionedInstallation, error) {
	tx, err := connection.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ProvisionedInstallation{}, fmt.Errorf("begin installation transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	var administratorID string
	if input.ReuseExisting {
		err = tx.QueryRow(ctx, `
			select id::text from users
			where role = 'ADMIN' and status = 'ACTIVE'
			order by created_at asc, id asc limit 1
		`).Scan(&administratorID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return ProvisionedInstallation{}, fmt.Errorf("select reusable administrator: %w", err)
		}
		if errors.Is(err, pgx.ErrNoRows) {
			administratorID = ""
		}
	}
	var exists bool
	if administratorID == "" && !input.ReuseExisting {
		if err := tx.QueryRow(ctx, "select exists(select 1 from users where role = 'ADMIN')").Scan(&exists); err != nil {
			return ProvisionedInstallation{}, fmt.Errorf("inspect existing administrators: %w", err)
		}
	}
	if administratorID == "" && exists {
		return ProvisionedInstallation{}, apperror.Conflict(
			apperror.CodeResourceConflict,
			"The selected database already contains an administrator",
			nil,
		)
	}
	if administratorID == "" {
		if err := tx.QueryRow(ctx,
			"select exists(select 1 from users where normalized_username = $1)",
			input.Administrator.NormalizedUsername,
		).Scan(&exists); err != nil {
			return ProvisionedInstallation{}, fmt.Errorf("inspect administrator username: %w", err)
		}
	}
	if administratorID == "" && exists {
		return ProvisionedInstallation{}, apperror.Conflict(
			apperror.CodeResourceConflict,
			"The administrator username is already used by an existing account",
			nil,
		)
	}
	var libraryRootID string
	if input.ReuseExisting {
		err = tx.QueryRow(ctx,
			"select id::text from library_roots where normalized_path = $1 order by created_at asc, id asc limit 1",
			input.Source.NormalizedPath,
		).Scan(&libraryRootID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return ProvisionedInstallation{}, fmt.Errorf("select reusable music source: %w", err)
		}
		if errors.Is(err, pgx.ErrNoRows) {
			libraryRootID = ""
		}
	}
	if libraryRootID == "" && !input.ReuseExisting {
		if err := tx.QueryRow(ctx,
			"select exists(select 1 from library_roots where normalized_path = $1)",
			input.Source.NormalizedPath,
		).Scan(&exists); err != nil {
			return ProvisionedInstallation{}, fmt.Errorf("inspect existing music source: %w", err)
		}
	}
	if libraryRootID == "" && exists {
		return ProvisionedInstallation{}, apperror.Conflict(
			apperror.CodeResourceConflict,
			"The selected database already contains the configured music source",
			nil,
		)
	}

	createdAdministrator := false
	if administratorID == "" {
		if err := tx.QueryRow(ctx, `
		insert into users (username, normalized_username, password_hash, role, status)
		values ($1, $2, $3, 'ADMIN', 'ACTIVE')
		returning id::text`,
			input.Administrator.Username,
			input.Administrator.NormalizedUsername,
			input.Administrator.PasswordHash,
		).Scan(&administratorID); err != nil {
			return ProvisionedInstallation{}, fmt.Errorf("create administrator: %w", err)
		}
		createdAdministrator = true
		if _, err := tx.Exec(ctx,
			"insert into user_profiles (user_id, display_name) values ($1::uuid, $2)",
			administratorID,
			input.Administrator.DisplayName,
		); err != nil {
			return ProvisionedInstallation{}, fmt.Errorf("create administrator profile: %w", err)
		}
	}
	includePatterns, err := json.Marshal(input.Source.IncludePatterns)
	if err != nil {
		return ProvisionedInstallation{}, fmt.Errorf("encode music source include patterns: %w", err)
	}
	excludePatterns, err := json.Marshal(input.Source.ExcludePatterns)
	if err != nil {
		return ProvisionedInstallation{}, fmt.Errorf("encode music source exclude patterns: %w", err)
	}
	createdLibraryRoot := false
	if libraryRootID == "" {
		if err := tx.QueryRow(ctx, `
		insert into library_roots (
			name, path, normalized_path, mode, configuration_managed, enabled,
			scan_on_startup, scan_interval_minutes, include_patterns, exclude_patterns, status
		) values ($1, $2, $3, $4::library_root_mode, true, $5, $6, $7, $8::jsonb, $9::jsonb, $10::library_root_status)
		returning id::text`,
			input.Source.Name,
			input.Source.Path,
			input.Source.NormalizedPath,
			input.Source.Mode,
			input.Source.Enabled,
			input.Source.ScanOnStartup,
			input.Source.ScanIntervalMinutes,
			string(includePatterns),
			string(excludePatterns),
			input.Source.Status,
		).Scan(&libraryRootID); err != nil {
			return ProvisionedInstallation{}, fmt.Errorf("create music source: %w", err)
		}
		createdLibraryRoot = true
	}
	if err := tx.Commit(ctx); err != nil {
		return ProvisionedInstallation{}, fmt.Errorf("commit installation transaction: %w", err)
	}
	return ProvisionedInstallation{
		AdministratorID:      administratorID,
		LibraryRootID:        libraryRootID,
		CreatedAdministrator: createdAdministrator,
		CreatedLibraryRoot:   createdLibraryRoot,
	}, nil
}

func (connection *productionInstallationDatabase) Compensate(
	ctx context.Context,
	installation ProvisionedInstallation,
) error {
	tx, err := connection.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin installation compensation: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if installation.CreatedLibraryRoot {
		if _, err := tx.Exec(ctx, "delete from library_roots where id = $1::uuid", installation.LibraryRootID); err != nil {
			return fmt.Errorf("remove setup music source: %w", err)
		}
	}
	if installation.CreatedAdministrator {
		if _, err := tx.Exec(ctx, "delete from users where id = $1::uuid", installation.AdministratorID); err != nil {
			return fmt.Errorf("remove setup administrator: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit installation compensation: %w", err)
	}
	return nil
}

type ProductionMediaStorageFactory struct{}

func (ProductionMediaStorageFactory) Open(cfg config.MediaStorage) (SetupMediaStorage, error) {
	return &productionSetupMediaStorage{
		assetDir:     cfg.AssetDirectory,
		transcodeDir: cfg.TranscodeDirectory,
	}, nil
}

type productionSetupMediaStorage struct {
	assetDir     string
	transcodeDir string
}

func (storage *productionSetupMediaStorage) Probe(ctx context.Context) error {
	if storage.assetDir == "" || storage.transcodeDir == "" {
		return errors.New("media asset and transcode directories are required")
	}
	return nil
}

func (storage *productionSetupMediaStorage) Inspect(ctx context.Context) (StorageInspection, error) {
	assetExists := false
	if fi, err := os.Stat(storage.assetDir); err == nil && fi.IsDir() {
		assetExists = true
	}
	transcodeExists := false
	if fi, err := os.Stat(storage.transcodeDir); err == nil && fi.IsDir() {
		transcodeExists = true
	}
	var assetCount int64
	hasAssets := false
	if assetExists {
		entries, err := os.ReadDir(storage.assetDir)
		if err == nil {
			assetCount = int64(len(entries))
			hasAssets = assetCount > 0
		}
	}
	var transcodeCount int64
	hasTranscode := false
	if transcodeExists {
		entries, err := os.ReadDir(storage.transcodeDir)
		if err == nil {
			transcodeCount = int64(len(entries))
			hasTranscode = transcodeCount > 0
		}
	}
	return StorageInspection{
		AssetDirectoryExists:     assetExists,
		TranscodeDirectoryExists: transcodeExists,
		HasAssets:                hasAssets,
		AssetCount:               assetCount,
		HasTranscode:             hasTranscode,
		TranscodeCount:           transcodeCount,
	}, nil
}


func (storage *productionSetupMediaStorage) EnsureDirectories(ctx context.Context) error {
	if err := os.MkdirAll(storage.assetDir, 0755); err != nil {
		return fmt.Errorf("create media asset directory: %w", err)
	}
	if err := os.MkdirAll(storage.transcodeDir, 0755); err != nil {
		return fmt.Errorf("create media transcode directory: %w", err)
	}
	return nil
}

func (storage *productionSetupMediaStorage) VerifyReadWrite(ctx context.Context) error {
	if err := storage.EnsureDirectories(ctx); err != nil {
		return err
	}
	probeFile := filepath.Join(storage.assetDir, ".xymusic-setup-probe")
	if err := os.WriteFile(probeFile, []byte("probe"), 0644); err != nil {
		return fmt.Errorf("verify write permission on asset directory: %w", err)
	}
	_ = os.Remove(probeFile)

	probeTranscode := filepath.Join(storage.transcodeDir, ".xymusic-setup-probe")
	if err := os.WriteFile(probeTranscode, []byte("probe"), 0644); err != nil {
		return fmt.Errorf("verify write permission on transcode directory: %w", err)
	}
	_ = os.Remove(probeTranscode)
	return nil
}

func (storage *productionSetupMediaStorage) Clear(ctx context.Context) error {
	if err := os.RemoveAll(storage.assetDir); err != nil {
		return err
	}
	if err := os.RemoveAll(storage.transcodeDir); err != nil {
		return err
	}
	return storage.EnsureDirectories(ctx)
}

func (storage *productionSetupMediaStorage) Close() {}

type CommandMediaTool struct{}

func (CommandMediaTool) Version(ctx context.Context, executable, label string) (string, error) {
	checkContext, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	command := exec.CommandContext(checkContext, executable, "-version")
	stdout := &cappedBuffer{maximum: 4096}
	stderr := &cappedBuffer{maximum: 4096}
	command.Stdout = stdout
	command.Stderr = stderr
	if err := command.Run(); err != nil {
		if errors.Is(checkContext.Err(), context.DeadlineExceeded) {
			return "", dependencyFailure(label+" version check timed out", err)
		}
		diagnostic := strings.TrimSpace(strings.NewReplacer("\r", " ", "\n", " ").Replace(stderr.String()))
		if len(diagnostic) > 500 {
			diagnostic = diagnostic[:500]
		}
		cause := err
		if diagnostic != "" {
			cause = fmt.Errorf("%w: %s", err, diagnostic)
		}
		return "", dependencyFailure(label+" could not be executed", cause)
	}
	line := strings.TrimSpace(strings.SplitN(stdout.String(), "\n", 2)[0])
	if line == "" {
		return "", dependencyFailure(label+" did not report a version", errors.New("empty version output"))
	}
	identity := strings.ToLower(line)
	expected := strings.ToLower(strings.TrimSpace(label))
	if (expected == "ffmpeg" || expected == "ffprobe") &&
		identity != expected && !strings.HasPrefix(identity, expected+" ") {
		return "", dependencyFailure(
			label+" executable identity check failed",
			fmt.Errorf("unexpected executable identity: %s", truncateRunes(line, 120)),
		)
	}
	return truncateRunes(line, 300), nil
}

type cappedBuffer struct {
	bytes   []byte
	maximum int
}

func (buffer *cappedBuffer) Write(value []byte) (int, error) {
	written := len(value)
	remaining := buffer.maximum - len(buffer.bytes)
	if remaining > 0 {
		buffer.bytes = append(buffer.bytes, value[:min(len(value), remaining)]...)
	}
	return written, nil
}

func (buffer *cappedBuffer) String() string { return string(buffer.bytes) }

type NetworkListenerProbe struct{}

func (NetworkListenerProbe) Check(ctx context.Context, host string, port int) error {
	network := "tcp6"
	if address := net.ParseIP(strings.Trim(host, "[]")); address != nil && address.To4() != nil {
		network = "tcp4"
	}
	listener, err := (&net.ListenConfig{}).Listen(ctx, network, net.JoinHostPort(host, fmt.Sprintf("%d", port)))
	if err != nil {
		return err
	}
	return listener.Close()
}

type OSSourceValidator struct{}

func (OSSourceValidator) Validate(_ context.Context, input SourceInput, root string) (ValidatedSource, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" || utf8.RuneCountInString(name) > 120 {
		return ValidatedSource{}, apperror.Validation("Music source name is invalid")
	}
	if input.Mode != "READ_ONLY" && input.Mode != "READ_WRITE" {
		return ValidatedSource{}, apperror.Validation("Music source mode is invalid")
	}
	if input.Enabled == nil || input.SyncOnStartup == nil {
		return ValidatedSource{}, apperror.Validation("Music source flags are required")
	}
	if input.IncludePatterns == nil || input.ExcludePatterns == nil {
		return ValidatedSource{}, apperror.Validation("Music source patterns are required")
	}
	if input.ScanIntervalMinutes != nil && (*input.ScanIntervalMinutes < 5 || *input.ScanIntervalMinutes > 10080) {
		return ValidatedSource{}, apperror.Validation("Scan interval must be 5 to 10080 minutes")
	}
	directory := strings.TrimSpace(input.Directory)
	if directory == "" || len(directory) > 4000 {
		return ValidatedSource{}, apperror.Validation("Music source path is invalid")
	}
	if !filepath.IsAbs(directory) {
		directory = filepath.Join(root, directory)
	}
	directory = filepath.Clean(directory)
	info, err := os.Stat(directory)
	if err != nil || !info.IsDir() {
		return ValidatedSource{}, apperror.New(
			apperror.CodeValidationError,
			"Music source path is not a directory",
			apperror.WithCause(err),
		)
	}
	opened, err := os.Open(directory)
	if err != nil {
		return ValidatedSource{}, apperror.New(
			apperror.CodeValidationError,
			"Music source path is not readable",
			apperror.WithCause(err),
		)
	}
	_, readErr := opened.Readdirnames(1)
	closeErr := opened.Close()
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return ValidatedSource{}, apperror.New(
			apperror.CodeValidationError,
			"Music source path is not readable",
			apperror.WithCause(readErr),
		)
	}
	if closeErr != nil {
		return ValidatedSource{}, apperror.New(
			apperror.CodeValidationError,
			"Music source path is not readable",
			apperror.WithCause(closeErr),
		)
	}
	if input.Mode == "READ_WRITE" {
		probe, err := os.CreateTemp(directory, ".xymusic-write-probe-*")
		if err != nil {
			return ValidatedSource{}, apperror.New(
				apperror.CodeValidationError,
				"Music source path is not readable and writable",
				apperror.WithCause(err),
			)
		}
		probePath := probe.Name()
		closeErr := probe.Close()
		removeErr := os.Remove(probePath)
		if closeErr != nil || removeErr != nil {
			return ValidatedSource{}, apperror.New(
				apperror.CodeValidationError,
				"Music source path is not readable and writable",
				apperror.WithCause(errors.Join(closeErr, removeErr)),
			)
		}
	}
	includePatterns, err := validateLibraryPatterns(input.IncludePatterns)
	if err != nil {
		return ValidatedSource{}, err
	}
	excludePatterns, err := validateLibraryPatterns(input.ExcludePatterns)
	if err != nil {
		return ValidatedSource{}, err
	}
	status := "UNKNOWN"
	if !*input.Enabled {
		status = "DISABLED"
	}
	return ValidatedSource{
		Name:                name,
		Path:                directory,
		NormalizedPath:      normalizedRootPath(directory),
		Mode:                input.Mode,
		Enabled:             *input.Enabled,
		ScanOnStartup:       *input.SyncOnStartup,
		ScanIntervalMinutes: cloneInt(input.ScanIntervalMinutes),
		IncludePatterns:     includePatterns,
		ExcludePatterns:     excludePatterns,
		Status:              status,
	}, nil
}

func validateLibraryPatterns(patterns []string) ([]string, error) {
	if len(patterns) > 100 {
		return nil, apperror.Validation("A music source cannot contain more than 100 patterns")
	}
	seen := make(map[string]struct{}, len(patterns))
	result := make([]string, 0, len(patterns))
	for _, pattern := range patterns {
		trimmed := strings.TrimSpace(pattern)
		normalized := strings.ReplaceAll(norm.NFKC.String(trimmed), "\\", "/")
		if normalized == "" || utf8.RuneCountInString(normalized) > 500 {
			return nil, apperror.Validation("Music source pattern is invalid")
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	return result, nil
}

func normalizedRootPath(path string) string {
	resolved, err := filepath.Abs(path)
	if err != nil {
		resolved = filepath.Clean(path)
	}
	portable := filepath.ToSlash(norm.NFKC.String(filepath.Clean(resolved)))
	root := filepath.ToSlash(norm.NFKC.String(filepath.VolumeName(resolved) + string(filepath.Separator)))
	if portable != root {
		portable = strings.TrimSuffix(portable, "/")
	}
	if runtime.GOOS == "windows" {
		portable = strings.ToLower(portable)
	}
	return portable
}

type SecurityPasswordHasher struct{}

func (SecurityPasswordHasher) Hash(password string) (string, error) {
	return security.HashPassword(password)
}

func truncateRunes(value string, maximum int) string {
	if utf8.RuneCountInString(value) <= maximum {
		return value
	}
	runes := []rune(value)
	return string(runes[:maximum])
}
