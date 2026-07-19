package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"xymusic/server/internal/config"
	"xymusic/server/internal/platform/database"
	"xymusic/server/internal/platform/security"
)

const isolatedPrefix = "xymusic_it_"

type accountCredentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
	UserID   string `json:"userId"`
}

type testCredentials struct {
	BaseURL string             `json:"baseUrl"`
	Admin   accountCredentials `json:"admin"`
	User    accountCredentials `json:"user"`
	Windows accountCredentials `json:"windows"`
	Android accountCredentials `json:"android"`
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(arguments []string) error {
	if len(arguments) == 0 {
		return errors.New("usage: xymusic-testenv create|destroy|reset-rate-limits [options]")
	}
	switch arguments[0] {
	case "create":
		return runCreate(arguments[1:])
	case "destroy":
		return runDestroy(arguments[1:])
	case "reset-rate-limits":
		return runResetRateLimits(arguments[1:])
	default:
		return fmt.Errorf("unsupported command %q", arguments[0])
	}
}

func runCreate(arguments []string) (resultErr error) {
	flags := flag.NewFlagSet("create", flag.ContinueOnError)
	sourcePath := flags.String("source", "", "source .env path")
	outputDirectory := flags.String("output", "", "isolated runtime directory")
	port := flags.Int("port", 3101, "legacy test server port")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 || strings.TrimSpace(*sourcePath) == "" || strings.TrimSpace(*outputDirectory) == "" {
		return errors.New("create requires -source and -output")
	}
	if *port < 1 || *port > 65535 {
		return errors.New("port must be from 1 to 65535")
	}
	source, err := filepath.Abs(*sourcePath)
	if err != nil {
		return err
	}
	output, err := filepath.Abs(*outputDirectory)
	if err != nil {
		return err
	}
	if err := ensureCreateDirectory(output); err != nil {
		return err
	}
	createdDirectory := true
	defer func() {
		if resultErr != nil && createdDirectory {
			_ = os.RemoveAll(output)
		}
	}()

	raw, err := config.NewStore(source).Load()
	if err != nil {
		return err
	}
	sourceRoot := filepath.Dir(source)
	resolved, err := config.ResolveRuntime(raw, sourceRoot)
	if err != nil {
		return err
	}
	suffix, err := randomIdentifier(8)
	if err != nil {
		return err
	}
	databaseName := isolatedPrefix + suffix
	bucketName := "xymusic-it-" + strings.ReplaceAll(suffix, "_", "-")
	databaseURL, adminURL, err := isolatedDatabaseURLs(raw.Database.URL, databaseName)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	adminConnection, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		return fmt.Errorf("connect to PostgreSQL administration database: %w", err)
	}
	defer adminConnection.Close(context.Background())
	if _, err := adminConnection.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{databaseName}.Sanitize()+" TEMPLATE template0 ENCODING 'UTF8'"); err != nil {
		return fmt.Errorf("create isolated PostgreSQL database: %w", err)
	}
	databaseCreated := true
	storageCreated := false
	defer func() {
		if resultErr == nil {
			return
		}
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if storageCreated {
			_ = removeBucket(cleanupContext, raw.Storage, bucketName)
		}
		if databaseCreated {
			_, _ = adminConnection.Exec(cleanupContext, "DROP DATABASE IF EXISTS "+pgx.Identifier{databaseName}.Sanitize()+" WITH (FORCE)")
		}
	}()

	isolated := raw
	isolated.Environment = config.Test
	isolated.Paths.MigrationsDirectory = resolved.Paths.MigrationsDirectory
	isolated.Paths.AdminWebDirectory = resolved.Paths.AdminWebDirectory
	isolated.Paths.MediaToolsDirectory = resolved.Paths.MediaToolsDirectory
	isolated.Paths.LocalMusicDirectory = filepath.Join(output, "music")
	isolated.HTTP.IPv4Host = "127.0.0.1"
	isolated.HTTP.IPv4Port = *port
	isolated.HTTP.IPv6Host = "::1"
	isolated.HTTP.IPv6Port = *port
	isolated.HTTP.Host = isolated.HTTP.IPv4Host
	isolated.HTTP.Port = isolated.HTTP.IPv4Port
	isolated.Database.URL = databaseURL
	isolated.Database.MaxConnections = min(isolated.Database.MaxConnections, 10)
	isolated.Storage.Bucket = bucketName
	isolated.Storage.PublicBaseURL = ""
	isolated.Media = resolved.Media
	isolated.Scraping = resolved.Scraping
	isolated.LocalLibrary.Name = "Isolated integration library"
	isolated.LocalLibrary.Directory = isolated.Paths.LocalMusicDirectory
	isolated.LocalLibrary.Enabled = false
	isolated.LocalLibrary.SyncOnStartup = false
	isolated.LocalLibrary.ScanIntervalMinutes = nil
	isolated.LocalLibrary.IncludePatterns = []string{}
	isolated.LocalLibrary.ExcludePatterns = []string{}
	isolated.Registration.Enabled = false
	if isolated.Security.AccessTokenSecret, err = randomSecret(); err != nil {
		return err
	}
	if isolated.Security.IdempotencyEncryptionSecret, err = randomSecret(); err != nil {
		return err
	}
	if isolated.Security.CursorSigningSecret, err = randomSecret(); err != nil {
		return err
	}
	if err := os.MkdirAll(isolated.Paths.LocalMusicDirectory, 0o755); err != nil {
		return err
	}

	storageClient, err := newStorageClient(isolated.Storage)
	if err != nil {
		return err
	}
	if err := storageClient.MakeBucket(ctx, bucketName, minio.MakeBucketOptions{Region: isolated.Storage.Region}); err != nil {
		exists, checkErr := storageClient.BucketExists(ctx, bucketName)
		if checkErr != nil || !exists {
			return fmt.Errorf("create isolated object-storage bucket: %w", err)
		}
	}
	storageCreated = true

	resolvedIsolated, err := config.ResolveRuntime(isolated, output)
	if err != nil {
		return err
	}
	pool, err := database.Open(ctx, resolvedIsolated.Database)
	if err != nil {
		return err
	}
	if err := database.RunMigrations(ctx, pool.Pool, resolvedIsolated.Paths.MigrationsDirectory); err != nil {
		pool.Close()
		return err
	}
	credentialsDocument, err := seedAccounts(ctx, pool)
	if err != nil {
		pool.Close()
		return err
	}
	pool.Close()

	environmentPath := filepath.Join(output, ".env")
	if err := config.NewStore(environmentPath).Save(isolated); err != nil {
		return err
	}
	credentialsDocument.BaseURL = "http://127.0.0.1:" + strconv.Itoa(*port)
	credentialsPath := filepath.Join(output, "test-credentials.json")
	if err := writeTestCredentials(credentialsPath, credentialsDocument); err != nil {
		return err
	}
	legacyExecutable := filepath.Join(sourceRoot, "xymusic.exe")
	if info, statErr := os.Stat(legacyExecutable); statErr == nil && !info.IsDir() {
		if err := copyFile(legacyExecutable, filepath.Join(output, "xymusic.exe"), 0o755); err != nil {
			return err
		}
	}
	databaseCreated = false
	storageCreated = false
	createdDirectory = false
	fmt.Println(environmentPath)
	fmt.Println("database=" + databaseName)
	fmt.Println("bucket=" + bucketName)
	fmt.Println("legacy=http://127.0.0.1:" + strconv.Itoa(*port))
	fmt.Println("credentials=" + credentialsPath)
	return nil
}

func runDestroy(arguments []string) error {
	flags := flag.NewFlagSet("destroy", flag.ContinueOnError)
	environmentPath := flags.String("environment", "", "isolated .env path")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 || strings.TrimSpace(*environmentPath) == "" {
		return errors.New("destroy requires -environment")
	}
	absolute, err := filepath.Abs(*environmentPath)
	if err != nil {
		return err
	}
	root := filepath.Dir(absolute)
	if !strings.HasPrefix(strings.ToLower(filepath.Base(root)), "xymusic-it-") {
		return errors.New("refusing to destroy an environment outside an xymusic-it-* directory")
	}
	raw, err := config.NewStore(absolute).Load()
	if err != nil {
		return err
	}
	databaseName, adminURL, err := databaseIdentity(raw.Database.URL)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(databaseName, isolatedPrefix) || !strings.HasPrefix(raw.Storage.Bucket, "xymusic-it-") {
		return errors.New("refusing to destroy resources without isolated prefixes")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := removeBucket(ctx, raw.Storage, raw.Storage.Bucket); err != nil {
		return err
	}
	connection, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		return err
	}
	defer connection.Close(context.Background())
	if _, err := connection.Exec(ctx, "DROP DATABASE IF EXISTS "+pgx.Identifier{databaseName}.Sanitize()+" WITH (FORCE)"); err != nil {
		return fmt.Errorf("drop isolated PostgreSQL database: %w", err)
	}
	return os.RemoveAll(root)
}

func runResetRateLimits(arguments []string) error {
	flags := flag.NewFlagSet("reset-rate-limits", flag.ContinueOnError)
	environmentPath := flags.String("environment", "", "isolated .env path")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 || strings.TrimSpace(*environmentPath) == "" {
		return errors.New("reset-rate-limits requires -environment")
	}
	absolute, raw, err := loadIsolatedEnvironment(*environmentPath)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	connection, err := pgx.Connect(ctx, raw.Database.URL)
	if err != nil {
		return fmt.Errorf("connect to isolated PostgreSQL database: %w", err)
	}
	defer connection.Close(context.Background())
	result, err := connection.Exec(ctx, "DELETE FROM rate_limit_buckets")
	if err != nil {
		return fmt.Errorf("reset isolated rate limits: %w", err)
	}
	fmt.Printf("environment=%s\ncleared=%d\n", absolute, result.RowsAffected())
	return nil
}

func loadIsolatedEnvironment(environmentPath string) (string, config.Config, error) {
	absolute, err := filepath.Abs(environmentPath)
	if err != nil {
		return "", config.Config{}, err
	}
	root := filepath.Dir(absolute)
	if !strings.HasPrefix(strings.ToLower(filepath.Base(root)), "xymusic-it-") {
		return "", config.Config{}, errors.New("refusing to use an environment outside an xymusic-it-* directory")
	}
	raw, err := config.NewStore(absolute).Load()
	if err != nil {
		return "", config.Config{}, err
	}
	databaseName, _, err := databaseIdentity(raw.Database.URL)
	if err != nil {
		return "", config.Config{}, err
	}
	if !strings.HasPrefix(databaseName, isolatedPrefix) || !strings.HasPrefix(raw.Storage.Bucket, "xymusic-it-") {
		return "", config.Config{}, errors.New("refusing to use resources without isolated prefixes")
	}
	return absolute, raw, nil
}

func ensureCreateDirectory(path string) error {
	if !strings.HasPrefix(strings.ToLower(filepath.Base(path)), "xymusic-it-") {
		return errors.New("output directory name must start with xymusic-it-")
	}
	if entries, err := os.ReadDir(path); err == nil && len(entries) != 0 {
		return errors.New("output directory must be empty")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.MkdirAll(path, 0o755)
}

func isolatedDatabaseURLs(source, databaseName string) (string, string, error) {
	parsed, err := url.Parse(source)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", "", errors.New("DATABASE_URL is invalid")
	}
	isolated := *parsed
	isolated.Path = "/" + databaseName
	admin := *parsed
	admin.Path = "/postgres"
	return isolated.String(), admin.String(), nil
}

func databaseIdentity(raw string) (string, string, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", "", errors.New("DATABASE_URL is invalid")
	}
	databaseName := strings.TrimPrefix(parsed.Path, "/")
	admin := *parsed
	admin.Path = "/postgres"
	return databaseName, admin.String(), nil
}

func newStorageClient(cfg config.Storage) (*minio.Client, error) {
	parsed, err := url.Parse(cfg.Endpoint)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, errors.New("S3_ENDPOINT is invalid")
	}
	lookup := minio.BucketLookupAuto
	if cfg.ForcePathStyle {
		lookup = minio.BucketLookupPath
	}
	return minio.New(parsed.Host, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		Secure: parsed.Scheme == "https", Region: cfg.Region, BucketLookup: lookup,
	})
}

func removeBucket(ctx context.Context, cfg config.Storage, bucket string) error {
	client, err := newStorageClient(cfg)
	if err != nil {
		return err
	}
	for object := range client.ListObjects(ctx, bucket, minio.ListObjectsOptions{Recursive: true}) {
		if object.Err != nil {
			return object.Err
		}
		if err := client.RemoveObject(ctx, bucket, object.Key, minio.RemoveObjectOptions{}); err != nil {
			return err
		}
	}
	if err := client.RemoveBucket(ctx, bucket); err != nil {
		exists, checkErr := client.BucketExists(ctx, bucket)
		if checkErr == nil && !exists {
			return nil
		}
		return fmt.Errorf("remove isolated object-storage bucket: %w", err)
	}
	return nil
}

func seedAccounts(ctx context.Context, pool *database.Pool) (testCredentials, error) {
	credentialsDocument := testCredentials{}
	for _, account := range []struct {
		username string
		role     string
		target   string
	}{
		{username: "isolated_admin", role: "ADMIN", target: "admin"},
		{username: "isolated_user", role: "USER", target: "user"},
		{username: "isolated_windows", role: "USER", target: "windows"},
		{username: "isolated_android", role: "USER", target: "android"},
	} {
		password, err := randomSecret()
		if err != nil {
			return testCredentials{}, err
		}
		hash, err := security.HashPassword(password)
		if err != nil {
			return testCredentials{}, err
		}
		var userID string
		if err := pool.QueryRow(ctx, `INSERT INTO users(
			username,normalized_username,password_hash,role,status
		) VALUES($1,$1,$2,$3,'ACTIVE') RETURNING id`, account.username, hash, account.role).Scan(&userID); err != nil {
			return testCredentials{}, err
		}
		if _, err := pool.Exec(ctx, `INSERT INTO user_profiles(user_id,display_name) VALUES($1,$2)`, userID, account.username); err != nil {
			return testCredentials{}, err
		}
		if _, err := pool.Exec(ctx, `INSERT INTO auth_sessions(
			user_id,installation_id,device_name,platform,app_version,last_seen_at
		) VALUES($1,$2,'isolated-test','WINDOWS','integration',now())`, userID, uuid.NewString()); err != nil {
			return testCredentials{}, err
		}
		credentials := accountCredentials{Username: account.username, Password: password, UserID: userID}
		switch account.target {
		case "admin":
			credentialsDocument.Admin = credentials
		case "user":
			credentialsDocument.User = credentials
		case "windows":
			credentialsDocument.Windows = credentials
		case "android":
			credentialsDocument.Android = credentials
		}
	}
	return credentialsDocument, nil
}

func writeTestCredentials(path string, document testCredentials) error {
	payload, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	output, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := output.Write(payload)
	closeErr := output.Close()
	return errors.Join(writeErr, closeErr)
}

func randomIdentifier(bytes int) (string, error) {
	value := make([]byte, bytes)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return strings.ToLower(fmt.Sprintf("%x", value)), nil
}

func randomSecret() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func copyFile(source, destination string, mode os.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	return errors.Join(copyErr, closeErr)
}
