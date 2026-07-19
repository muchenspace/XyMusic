package setup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"xymusic/server/internal/config"
	"xymusic/server/internal/shared/apperror"
)

func TestBuildSetupDatabaseURLSupportsIPv6AndEscapesCredentials(t *testing.T) {
	value, err := BuildSetupDatabaseURL(DatabaseInput{
		Host:           "::1",
		Port:           5432,
		Database:       "music library",
		Username:       "music admin",
		Password:       "p@ss/word",
		SSLMode:        "require",
		MaxConnections: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"postgresql://music%20admin:p%40ss%2Fword@[::1]:5432/music%20library", "sslmode=require"} {
		if !strings.Contains(value, expected) {
			t.Fatalf("database URL %q does not contain %q", value, expected)
		}
	}
	for _, invalid := range []string{"bad:host", "[::1", "localhost:5432"} {
		input := DatabaseInput{
			Host: invalid, Port: 5432, Database: "xymusic", Username: "xymusic",
			Password: "secret", SSLMode: "disable", MaxConnections: 10,
		}
		if _, err := BuildSetupDatabaseURL(input); !apperror.IsCode(err, apperror.CodeValidationError) {
			t.Fatalf("expected invalid host %q to fail validation, got %v", invalid, err)
		}
	}
	componentURL, err := BuildSetupDatabaseURL(DatabaseInput{
		Host: "localhost", Port: 5432, Database: "music/library", Username: "user",
		Password: "secret", SSLMode: "disable", MaxConnections: 10,
	})
	if err != nil || !strings.Contains(componentURL, "/music%2Flibrary?") {
		t.Fatalf("database name was not encoded as one URL component: %q, %v", componentURL, err)
	}
}

func TestBuildSetupDatabaseURLReturnsFieldSpecificValidation(t *testing.T) {
	valid := DatabaseInput{
		Host: "127.0.0.1", Port: 5432, Database: "xymusic", Username: "xymusic",
		Password: "secret", SSLMode: "disable", MaxConnections: 10,
	}
	tests := []struct {
		name   string
		field  string
		mutate func(*DatabaseInput)
	}{
		{"host", "host", func(input *DatabaseInput) { input.Host = "host:5432" }},
		{"port", "port", func(input *DatabaseInput) { input.Port = 0 }},
		{"database", "database", func(input *DatabaseInput) { input.Database = "" }},
		{"username", "username", func(input *DatabaseInput) { input.Username = "" }},
		{"password", "password", func(input *DatabaseInput) { input.Password = "" }},
		{"ssl mode", "sslMode", func(input *DatabaseInput) { input.SSLMode = "invalid" }},
		{"maximum connections", "maxConnections", func(input *DatabaseInput) { input.MaxConnections = 0 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := valid
			test.mutate(&input)
			_, err := BuildSetupDatabaseURL(input)
			applicationError, ok := apperror.As(err)
			if !ok || applicationError.Code != apperror.CodeValidationError {
				t.Fatalf("expected validation error, got %#v", err)
			}
			fieldErrors, _ := applicationError.Metadata["fieldErrors"].(map[string][]string)
			if len(fieldErrors[test.field]) != 1 || !strings.Contains(applicationError.Detail, "数据库") {
				t.Fatalf("field %q did not receive a specific database validation error: %#v", test.field, applicationError)
			}
		})
	}
}

func TestStorageProbeIsReadOnlyAndMediaFingerprintIsOptional(t *testing.T) {
	root := prepareSetupRoot(t)
	runtime := newFakeRuntime()
	objects := &fakeStorage{}
	media := &fakeMediaTool{}
	service := mustService(t, Options{
		RootDirectory:   root,
		Runtime:         runtime,
		Store:           &fakeStore{},
		Databases:       &fakeDatabaseFactory{database: &fakeDatabase{}},
		ObjectStorage:   &fakeStorageFactory{storage: objects},
		MediaTool:       media,
		ListenerProbe:   &fakeListener{},
		Passwords:       fakePasswords{},
		SecretGenerator: fixedSecret,
	})
	storageResult, err := service.TestStorage(context.Background(), validSetupInput().Storage)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(storageResult.StorageInspection, (StorageInspection{})) {
		t.Fatalf("unexpected empty storage inspection: %#v", storageResult.StorageInspection)
	}
	if !reflect.DeepEqual(objects.calls, []string{"inspect", "close"}) {
		t.Fatalf("storage test mutated storage: %#v", objects.calls)
	}

	input := validSetupInput().Media
	result, err := service.TestMedia(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if result.FPcalc != nil || result.FingerprintConfigured {
		t.Fatalf("unexpected fingerprint result: %#v", result)
	}
	if !reflect.DeepEqual(media.labels, []string{"ffmpeg", "ffprobe"}) {
		t.Fatalf("unexpected media probes: %#v", media.labels)
	}

	fpcalc := "tools/fpcalc.exe"
	input.FPcalcPath = &fpcalc
	result, err = service.TestMedia(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if result.FPcalc == nil || *result.FPcalc != "fpcalc version test" || result.FingerprintConfigured {
		t.Fatalf("optional fpcalc probe mismatch: %#v", result)
	}
}

func TestAdministratorValidationMatchesLegacyPasswordBoundary(t *testing.T) {
	service := mustService(t, Options{
		RootDirectory:   prepareSetupRoot(t),
		Runtime:         newFakeRuntime(),
		Store:           &fakeStore{},
		Databases:       &fakeDatabaseFactory{database: &fakeDatabase{}},
		ObjectStorage:   &fakeStorageFactory{storage: &fakeStorage{}},
		MediaTool:       &fakeMediaTool{},
		ListenerProbe:   &fakeListener{},
		Passwords:       fakePasswords{},
		SecretGenerator: fixedSecret,
	})
	valid := AdministratorInput{Username: "admin", DisplayName: "Admin", Password: "123456"}
	if _, err := service.TestAdministrator(context.Background(), valid); err != nil {
		t.Fatalf("six-character password was rejected: %v", err)
	}
	valid.Password = "12345"
	if _, err := service.TestAdministrator(context.Background(), valid); !apperror.IsCode(err, apperror.CodeValidationError) {
		t.Fatalf("short password should fail validation, got %v", err)
	}
}

func TestCompleteRunsProbesMigrationsProvisioningRuntimeAndAtomicConfiguration(t *testing.T) {
	root := prepareSetupRoot(t)
	events := &eventLog{}
	runtime := newFakeRuntime()
	runtime.events = events
	store := &fakeStore{events: events}
	db := &fakeDatabase{events: events}
	objects := &fakeStorage{events: events, createBucket: true}
	listener := &fakeListener{events: events}
	media := &fakeMediaTool{events: events}
	service := mustService(t, Options{
		RootDirectory: root,
		ActualListener: ActualListener{
			IPv4: ListenerAddress{Host: "0.0.0.0", Port: 3000},
			IPv6: ListenerAddress{Host: "::", Port: 3000},
		},
		Runtime:         runtime,
		Store:           store,
		Databases:       &fakeDatabaseFactory{database: db, events: events},
		ObjectStorage:   &fakeStorageFactory{storage: objects, events: events},
		MediaTool:       media,
		ListenerProbe:   listener,
		Passwords:       fakePasswords{events: events},
		SecretGenerator: fixedSecret,
	})

	result, err := service.Complete(context.Background(), validSetupInput(), "trace-setup-complete")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Configured || result.RuntimeGeneration != 1 || len(result.RestartRequiredFields) != 0 {
		t.Fatalf("unexpected completion response: %#v", result)
	}
	if !store.exists || store.saved.Environment != config.Production {
		t.Fatalf("production configuration was not saved: %#v", store.saved)
	}
	if store.saved.Database.URL == "" || len(store.saved.Security.AccessTokenSecret) < 32 {
		t.Fatalf("saved configuration is incomplete: %#v", store.saved)
	}
	if db.provisioned.Administrator.NormalizedUsername != "administrator" || db.provisioned.Source.Path != filepath.Join(root, "music") {
		t.Fatalf("installation records are incomplete: %#v", db.provisioned)
	}
	if filepath.Clean(db.migrationsDirectory) != filepath.Join(root, "migrations") {
		t.Fatalf("migrations were not resolved from the executable root: %s", db.migrationsDirectory)
	}
	if listener.port != 0 {
		t.Fatalf("bootstrap listener port should be replaced with an ephemeral probe, got %d", listener.port)
	}
	assertEventOrder(t, events.snapshot(), []string{
		"listener.check",
		"database.open", "database.ping", "database.permission", "database.compatibility", "database.inspect", "database.close",
		"storage.open", "storage.inspect", "storage.close",
		"media.ffmpeg", "media.ffprobe",
		"store.load",
		"database.open", "storage.open", "storage.inspect", "storage.ensure", "storage.verify",
		"database.inspect", "database.migrate", "password.hash", "database.provision",
		"runtime.initialize", "store.save", "database.audit",
	})
	if _, err := service.TestAdministrator(context.Background(), validSetupInput().Administrator); !apperror.IsCode(err, apperror.CodeForbidden) {
		t.Fatalf("setup probes must be disabled after completion, got %v", err)
	}
	status := service.Status()
	if status.SetupRequired || !status.Configured || status.ConfigurationSource != RuntimeSourceManaged {
		t.Fatalf("unexpected post-setup status: %#v", status)
	}
}

func TestHTTPProbeChecksIPv4AndIPv6ListenersIndependently(t *testing.T) {
	listener := &fakeListener{}
	service := mustService(t, Options{
		RootDirectory: t.TempDir(),
		Runtime:       newFakeRuntime(),
		ListenerProbe: listener,
		ActualListener: ActualListener{
			IPv4: ListenerAddress{Host: "0.0.0.0", Port: 3000},
			IPv6: ListenerAddress{Host: "::", Port: 3000},
		},
	})
	input := HTTPInput{
		IPv4Host: "192.0.2.10", IPv4Port: 3100,
		IPv6Host: "2001:db8::10", IPv6Port: 3200,
		TrustedProxyAddresses: []string{},
	}
	if _, err := service.TestHTTP(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	want := []ListenerAddress{{Host: "192.0.2.10", Port: 3100}, {Host: "2001:db8::10", Port: 3200}}
	if !reflect.DeepEqual(listener.calls, want) {
		t.Fatalf("listener probes = %#v, want %#v", listener.calls, want)
	}
	if _, err := service.TestHTTP(context.Background(), HTTPInput{
		IPv4Host: "::", IPv4Port: 3000, IPv6Host: "::", IPv6Port: 3000,
		TrustedProxyAddresses: []string{},
	}); !apperror.IsCode(err, apperror.CodeValidationError) {
		t.Fatalf("mismatched IPv4 address family should fail, got %v", err)
	} else if applicationError, ok := apperror.As(err); !ok || !fieldErrorExists(applicationError, "ipv4Host") {
		t.Fatalf("IPv4 validation error is not attached to ipv4Host: %#v", err)
	}

	listener.err = errors.New("bind denied")
	if _, err := service.TestHTTP(context.Background(), input); !apperror.IsCode(err, apperror.CodeValidationError) {
		t.Fatalf("listener bind failure should be a validation error, got %v", err)
	} else if applicationError, ok := apperror.As(err); !ok ||
		!fieldErrorExists(applicationError, "ipv4Host") || !fieldErrorExists(applicationError, "ipv4Port") {
		t.Fatalf("listener bind failure is not attached to IPv4 fields: %#v", err)
	}
}

func fieldErrorExists(err *apperror.Error, field string) bool {
	fieldErrors, ok := err.Metadata["fieldErrors"].(map[string][]string)
	return ok && len(fieldErrors[field]) > 0
}

func TestCompleteCompensatesConfigurationRuntimeDataAndCreatedBucket(t *testing.T) {
	root := prepareSetupRoot(t)
	events := &eventLog{}
	runtime := newFakeRuntime()
	runtime.events = events
	store := &fakeStore{events: events}
	db := &fakeDatabase{events: events, auditErr: errors.New("audit unavailable")}
	objects := &fakeStorage{events: events, createBucket: true}
	service := mustService(t, Options{
		RootDirectory:   root,
		Runtime:         runtime,
		Store:           store,
		Databases:       &fakeDatabaseFactory{database: db, events: events},
		ObjectStorage:   &fakeStorageFactory{storage: objects, events: events},
		MediaTool:       &fakeMediaTool{events: events},
		ListenerProbe:   &fakeListener{events: events},
		Passwords:       fakePasswords{events: events},
		SecretGenerator: fixedSecret,
	})

	_, err := service.Complete(context.Background(), validSetupInput(), "trace-rollback")
	applicationError, ok := err.(*apperror.Error)
	if !ok || applicationError.Code != apperror.CodeSetupFailed {
		t.Fatalf("expected normalized setup failure, got %#v", err)
	}
	if applicationError.Metadata["setupStage"] != "completion_audit" || applicationError.Metadata["rollbackIncomplete"] != false {
		t.Fatalf("unexpected setup failure metadata: %#v", applicationError.Metadata)
	}
	if store.exists || runtime.Status().Phase != "SETUP_REQUIRED" {
		t.Fatalf("configuration/runtime rollback was incomplete: store=%v runtime=%#v", store.exists, runtime.Status())
	}
	if !db.compensated || !objects.bucketRemoved {
		t.Fatalf("database/storage compensation was incomplete: database=%v bucket=%v", db.compensated, objects.bucketRemoved)
	}
	assertEventOrder(t, events.snapshot(), []string{
		"runtime.initialize", "store.save", "database.audit",
		"runtime.close", "store.clear", "database.compensate", "storage.removeBucket",
	})
	if _, probeErr := service.TestAdministrator(context.Background(), validSetupInput().Administrator); probeErr != nil {
		t.Fatalf("failed setup must remain retryable, got %v", probeErr)
	}
}

func TestCompleteReportsMalformedEnvironmentAsConflict(t *testing.T) {
	store := &fakeStore{loadErr: fmtInvalidConfiguration()}
	service := mustService(t, Options{
		RootDirectory:   prepareSetupRoot(t),
		Runtime:         newFakeRuntime(),
		Store:           store,
		Databases:       &fakeDatabaseFactory{database: &fakeDatabase{}},
		ObjectStorage:   &fakeStorageFactory{storage: &fakeStorage{}},
		MediaTool:       &fakeMediaTool{},
		ListenerProbe:   &fakeListener{},
		Passwords:       fakePasswords{},
		SecretGenerator: fixedSecret,
	})
	_, err := service.Complete(context.Background(), validSetupInput(), "trace-invalid-env")
	if !apperror.IsCode(err, apperror.CodeResourceConflict) {
		t.Fatalf("invalid environment should be a conflict, got %v", err)
	}
}

func TestCompleteMapsUnknownProbeFailureToSafeStageError(t *testing.T) {
	service := mustService(t, Options{
		RootDirectory:   prepareSetupRoot(t),
		Runtime:         newFakeRuntime(),
		Store:           &fakeStore{},
		Databases:       &fakeDatabaseFactory{database: &fakeDatabase{}},
		ObjectStorage:   &fakeStorageFactory{storage: &fakeStorage{}},
		MediaTool:       &fakeMediaTool{err: errors.New("postgresql://administrator:private-password@database/xymusic")},
		ListenerProbe:   &fakeListener{},
		Passwords:       fakePasswords{},
		SecretGenerator: fixedSecret,
	})
	_, err := service.Complete(context.Background(), validSetupInput(), "trace-stage")
	applicationError, ok := err.(*apperror.Error)
	if !ok || applicationError.Code != apperror.CodeSetupFailed {
		t.Fatalf("unknown probe failure was not normalized: %#v", err)
	}
	if applicationError.Metadata["setupStage"] != "media_probe" || applicationError.Metadata["rollbackIncomplete"] != false {
		t.Fatalf("unexpected stage metadata: %#v", applicationError.Metadata)
	}
	if strings.Contains(applicationError.Error(), "private-password") {
		t.Fatalf("private diagnostic escaped into the public error: %v", applicationError)
	}
}

func TestCompleteRequiresFingerprintExecutableAndAcoustIDTogether(t *testing.T) {
	service := mustService(t, Options{
		RootDirectory:   prepareSetupRoot(t),
		Runtime:         newFakeRuntime(),
		Store:           &fakeStore{},
		Databases:       &fakeDatabaseFactory{database: &fakeDatabase{}},
		ObjectStorage:   &fakeStorageFactory{storage: &fakeStorage{}},
		MediaTool:       &fakeMediaTool{},
		ListenerProbe:   &fakeListener{},
		Passwords:       fakePasswords{},
		SecretGenerator: fixedSecret,
	})
	input := validSetupInput()
	fpcalc := "tools/fpcalc.exe"
	input.Media.FPcalcPath = &fpcalc
	_, err := service.Complete(context.Background(), input, "trace-fingerprint")
	if !apperror.IsCode(err, apperror.CodeValidationError) {
		t.Fatalf("incomplete fingerprint configuration should fail validation, got %v", err)
	}
}

func TestRollbackFailureIsReportedWithoutClaimingSuccess(t *testing.T) {
	root := prepareSetupRoot(t)
	runtime := newFakeRuntime()
	store := &fakeStore{clearErr: errors.New("configuration cannot be removed")}
	db := &fakeDatabase{auditErr: errors.New("audit unavailable")}
	service := mustService(t, Options{
		RootDirectory:   root,
		Runtime:         runtime,
		Store:           store,
		Databases:       &fakeDatabaseFactory{database: db},
		ObjectStorage:   &fakeStorageFactory{storage: &fakeStorage{createBucket: true}},
		MediaTool:       &fakeMediaTool{},
		ListenerProbe:   &fakeListener{},
		Passwords:       fakePasswords{},
		SecretGenerator: fixedSecret,
	})
	_, err := service.Complete(context.Background(), validSetupInput(), "trace-incomplete-rollback")
	applicationError, ok := err.(*apperror.Error)
	if !ok || applicationError.Code != apperror.CodeSetupFailed || applicationError.Metadata["rollbackIncomplete"] != true {
		t.Fatalf("rollback failure must be explicit, got %#v", err)
	}
	if service.Status().Configured {
		t.Fatal("failed completion reported a configured installation")
	}
}

func TestCompleteRejectsRuntimeThatDidNotActivateCandidate(t *testing.T) {
	runtime := &nonActivatingRuntime{}
	store := &fakeStore{}
	db := &fakeDatabase{}
	service := mustService(t, Options{
		RootDirectory:   prepareSetupRoot(t),
		Runtime:         runtime,
		Store:           store,
		Databases:       &fakeDatabaseFactory{database: db},
		ObjectStorage:   &fakeStorageFactory{storage: &fakeStorage{createBucket: true}},
		MediaTool:       &fakeMediaTool{},
		ListenerProbe:   &fakeListener{},
		Passwords:       fakePasswords{},
		SecretGenerator: fixedSecret,
	})
	_, err := service.Complete(context.Background(), validSetupInput(), "trace-runtime-noop")
	applicationError, ok := err.(*apperror.Error)
	if !ok || applicationError.Code != apperror.CodeSetupFailed || applicationError.Metadata["setupStage"] != "runtime_initialize" {
		t.Fatalf("non-activating runtime was not rejected: %#v", err)
	}
	if !runtime.closed || store.exists || !db.compensated {
		t.Fatalf("non-activating runtime was not compensated: runtime=%v store=%v database=%v", runtime.closed, store.exists, db.compensated)
	}
}

func TestConfiguredRuntimeDisablesEverySetupOperation(t *testing.T) {
	runtime := newFakeRuntime()
	runtime.snapshot = RuntimeSnapshot{Phase: RuntimePhaseReady, Source: RuntimeSourceManaged, Generation: 1}
	service := mustService(t, Options{
		RootDirectory:   prepareSetupRoot(t),
		Runtime:         runtime,
		Store:           &fakeStore{exists: true},
		Databases:       &fakeDatabaseFactory{database: &fakeDatabase{}},
		ObjectStorage:   &fakeStorageFactory{storage: &fakeStorage{}},
		MediaTool:       &fakeMediaTool{},
		ListenerProbe:   &fakeListener{},
		Passwords:       fakePasswords{},
		SecretGenerator: fixedSecret,
	})
	if _, err := service.TestHTTP(context.Background(), validSetupInput().HTTP); !apperror.IsCode(err, apperror.CodeForbidden) {
		t.Fatalf("configured HTTP probe should be forbidden, got %v", err)
	}
	if _, err := service.Complete(context.Background(), validSetupInput(), "trace-disabled"); !apperror.IsCode(err, apperror.CodeForbidden) {
		t.Fatalf("configured completion should be forbidden, got %v", err)
	}
}

func TestDatabaseProbeReportsExistingInstallation(t *testing.T) {
	inspection := InstallationInspection{
		State:   DatabaseStatePartial,
		HasData: true, HasAdministrator: true, HasActiveAdministrator: true,
		Reusable: []string{"administrator", "catalog"}, Missing: []string{"librarySource"},
	}
	service := mustService(t, Options{
		RootDirectory: prepareSetupRoot(t), Runtime: newFakeRuntime(), Store: &fakeStore{},
		Databases:     &fakeDatabaseFactory{database: &fakeDatabase{inspection: inspection}},
		ObjectStorage: &fakeStorageFactory{storage: &fakeStorage{}}, MediaTool: &fakeMediaTool{},
		ListenerProbe: &fakeListener{}, Passwords: fakePasswords{}, SecretGenerator: fixedSecret,
	})
	input := validSetupInput()
	result, err := service.TestDatabase(context.Background(), DatabaseTestInput{
		Database: input.Database, MigrationsDirectory: input.Paths.MigrationsDirectory,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result.DatabaseInspection, inspection) {
		t.Fatalf("unexpected database inspection: %#v", result.DatabaseInspection)
	}
}

func TestDatabaseProbeClassifiesMissingDatabase(t *testing.T) {
	service := mustService(t, Options{
		RootDirectory: prepareSetupRoot(t), Runtime: newFakeRuntime(), Store: &fakeStore{},
		Databases:     &fakeDatabaseFactory{openErr: &pgconn.PgError{Code: "3D000"}},
		ObjectStorage: &fakeStorageFactory{storage: &fakeStorage{}}, MediaTool: &fakeMediaTool{},
		ListenerProbe: &fakeListener{}, Passwords: fakePasswords{}, SecretGenerator: fixedSecret,
	})
	input := validSetupInput()
	_, err := service.TestDatabase(context.Background(), DatabaseTestInput{
		Database: input.Database, MigrationsDirectory: input.Paths.MigrationsDirectory,
	})
	applicationError, ok := apperror.As(err)
	if !ok {
		t.Fatalf("expected classified database error, got %T", err)
	}
	assertDatabaseApplicationError(
		t,
		applicationError,
		apperror.CodeDatabaseNotFound,
		"数据库名不存在，请确认数据库已经创建且名称填写正确。",
		[]string{"database"},
	)
}

func TestCompletePreservesSpecificDatabaseFailureAndSetupStage(t *testing.T) {
	service := mustService(t, Options{
		RootDirectory: prepareSetupRoot(t), Runtime: newFakeRuntime(), Store: &fakeStore{},
		Databases:     &fakeDatabaseFactory{openErr: &pgconn.PgError{Code: "28P01"}},
		ObjectStorage: &fakeStorageFactory{storage: &fakeStorage{}}, MediaTool: &fakeMediaTool{},
		ListenerProbe: &fakeListener{}, Passwords: fakePasswords{}, SecretGenerator: fixedSecret,
	})
	_, err := service.Complete(context.Background(), validSetupInput(), "trace-database-auth")
	applicationError, ok := apperror.As(err)
	if !ok {
		t.Fatalf("expected classified completion error, got %T", err)
	}
	assertDatabaseApplicationError(
		t,
		applicationError,
		apperror.CodeDatabaseAuthenticationFailed,
		"数据库认证失败，用户名、密码或 pg_hba.conf 认证规则不匹配。",
		[]string{"password", "username"},
	)
	if applicationError.Metadata["setupStage"] != "database_probe" {
		t.Fatalf("setup stage = %#v, want database_probe", applicationError.Metadata["setupStage"])
	}
}

func TestDatabaseDecisionMatchesInspectionState(t *testing.T) {
	tests := []struct {
		name       string
		inspection InstallationInspection
		action     string
		wantError  bool
	}{
		{name: "empty without action", inspection: InstallationInspection{State: DatabaseStateEmpty}},
		{name: "partial without action", inspection: InstallationInspection{State: DatabaseStatePartial}, wantError: true},
		{name: "partial reuse", inspection: InstallationInspection{State: DatabaseStatePartial}, action: databaseActionReusePartial},
		{name: "partial reset", inspection: InstallationInspection{State: DatabaseStatePartial}, action: databaseActionReset},
		{name: "partial rejects migrate", inspection: InstallationInspection{State: DatabaseStatePartial}, action: databaseActionMigrate, wantError: true},
		{name: "complete without action", inspection: InstallationInspection{State: DatabaseStateComplete}, wantError: true},
		{name: "complete migrate", inspection: InstallationInspection{State: DatabaseStateComplete}, action: databaseActionMigrate},
		{name: "complete reset", inspection: InstallationInspection{State: DatabaseStateComplete}, action: databaseActionReset},
		{name: "complete rejects partial reuse", inspection: InstallationInspection{State: DatabaseStateComplete}, action: databaseActionReusePartial, wantError: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateDatabaseDecision(test.inspection, test.action)
			if test.wantError && !apperror.IsCode(err, apperror.CodeSetupDecisionRequired) && !apperror.IsCode(err, apperror.CodeResourceConflict) {
				t.Fatalf("expected a decision error, got %v", err)
			}
			if !test.wantError && err != nil {
				t.Fatalf("unexpected decision error: %v", err)
			}
		})
	}
}

func TestStorageProbeReportsExistingObjects(t *testing.T) {
	inspection := StorageInspection{BucketExists: true, HasObjects: true, ObjectCount: 42, CountLimited: true}
	service := mustService(t, Options{
		RootDirectory: prepareSetupRoot(t), Runtime: newFakeRuntime(), Store: &fakeStore{},
		Databases:     &fakeDatabaseFactory{database: &fakeDatabase{}},
		ObjectStorage: &fakeStorageFactory{storage: &fakeStorage{inspection: inspection}}, MediaTool: &fakeMediaTool{},
		ListenerProbe: &fakeListener{}, Passwords: fakePasswords{}, SecretGenerator: fixedSecret,
	})
	result, err := service.TestStorage(context.Background(), validSetupInput().Storage)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result.StorageInspection, inspection) {
		t.Fatalf("unexpected storage inspection: %#v", result.StorageInspection)
	}
}

func TestCompleteRequiresExistingDataDecisionWhenAdministratorExists(t *testing.T) {
	db := &fakeDatabase{inspection: InstallationInspection{
		State:   DatabaseStateComplete,
		HasData: true, HasAdministrator: true, HasActiveAdministrator: true,
		Reusable: []string{"administrator"}, Missing: []string{"librarySource"},
	}}
	service := mustService(t, Options{
		RootDirectory: prepareSetupRoot(t), Runtime: newFakeRuntime(), Store: &fakeStore{},
		Databases:     &fakeDatabaseFactory{database: db},
		ObjectStorage: &fakeStorageFactory{storage: &fakeStorage{}}, MediaTool: &fakeMediaTool{},
		ListenerProbe: &fakeListener{}, Passwords: fakePasswords{}, SecretGenerator: fixedSecret,
	})
	_, err := service.Complete(context.Background(), validSetupInput(), "trace-existing-decision")
	applicationError, ok := err.(*apperror.Error)
	if !ok || applicationError.Code != apperror.CodeSetupDecisionRequired {
		t.Fatalf("existing administrator did not require a decision: %#v", err)
	}
	if db.provisioned.Administrator.Username != "" {
		t.Fatalf("database was provisioned before the decision: %#v", db.provisioned)
	}
}

func TestCompleteReusesExistingInstallationAndFillsMissingParts(t *testing.T) {
	db := &fakeDatabase{inspection: InstallationInspection{
		State:   DatabaseStateComplete,
		HasData: true, HasAdministrator: true, HasActiveAdministrator: true,
		Reusable: []string{"administrator", "catalog"}, Missing: []string{"librarySource"},
	}}
	service := mustService(t, Options{
		RootDirectory: prepareSetupRoot(t), Runtime: newFakeRuntime(), Store: &fakeStore{},
		Databases:     &fakeDatabaseFactory{database: db},
		ObjectStorage: &fakeStorageFactory{storage: &fakeStorage{}}, MediaTool: &fakeMediaTool{},
		ListenerProbe: &fakeListener{}, Passwords: fakePasswords{}, SecretGenerator: fixedSecret,
	})
	input := validSetupInput()
	input.DatabaseAction = databaseActionMigrate
	input.Administrator = AdministratorInput{}
	if _, err := service.Complete(context.Background(), input, "trace-existing-reuse"); err != nil {
		t.Fatal(err)
	}
	if !db.provisioned.ReuseExisting || db.reset {
		t.Fatalf("existing installation was not reused safely: provision=%#v reset=%v", db.provisioned, db.reset)
	}
	if db.provisioned.Administrator != (AdministratorRecord{}) {
		t.Fatalf("reused installation unexpectedly provisioned an administrator: %#v", db.provisioned.Administrator)
	}
}

func TestDatabaseResetDoesNotClearObjectStorage(t *testing.T) {
	db := &fakeDatabase{inspection: InstallationInspection{
		State:   DatabaseStateComplete,
		HasData: true, HasAdministrator: true, HasActiveAdministrator: true,
	}}
	objects := &fakeStorage{}
	service := mustService(t, Options{
		RootDirectory: prepareSetupRoot(t), Runtime: newFakeRuntime(), Store: &fakeStore{},
		Databases:     &fakeDatabaseFactory{database: db},
		ObjectStorage: &fakeStorageFactory{storage: objects}, MediaTool: &fakeMediaTool{},
		ListenerProbe: &fakeListener{}, Passwords: fakePasswords{}, SecretGenerator: fixedSecret,
	})
	input := validSetupInput()
	input.DatabaseAction = databaseActionReset
	if _, err := service.Complete(context.Background(), input, "trace-existing-reset"); err != nil {
		t.Fatal(err)
	}
	if !db.reset || db.provisioned.ReuseExisting || slices.Contains(objects.calls, "clear") {
		t.Fatalf("database decision affected the wrong resource: reset=%v provision=%#v storage=%#v", db.reset, db.provisioned, objects.calls)
	}
}

func TestCompleteRequiresIndependentStorageDecision(t *testing.T) {
	db := &fakeDatabase{}
	objects := &fakeStorage{inspection: StorageInspection{
		BucketExists: true, HasObjects: true, ObjectCount: 3,
	}}
	service := mustService(t, Options{
		RootDirectory: prepareSetupRoot(t), Runtime: newFakeRuntime(), Store: &fakeStore{},
		Databases:     &fakeDatabaseFactory{database: db},
		ObjectStorage: &fakeStorageFactory{storage: objects}, MediaTool: &fakeMediaTool{},
		ListenerProbe: &fakeListener{}, Passwords: fakePasswords{}, SecretGenerator: fixedSecret,
	})
	_, err := service.Complete(context.Background(), validSetupInput(), "trace-storage-decision")
	applicationError, ok := err.(*apperror.Error)
	if !ok || applicationError.Code != apperror.CodeSetupDecisionRequired || applicationError.Metadata["decisionResource"] != "storage" {
		t.Fatalf("nonempty storage did not require an independent decision: %#v", err)
	}
	if db.reset || slices.Contains(objects.calls, "clear") {
		t.Fatalf("resources changed before storage decision: database=%v storage=%#v", db.reset, objects.calls)
	}
}

func TestStorageResetDoesNotClearDatabase(t *testing.T) {
	db := &fakeDatabase{}
	objects := &fakeStorage{inspection: StorageInspection{
		BucketExists: true, HasObjects: true, ObjectCount: 3,
	}}
	service := mustService(t, Options{
		RootDirectory: prepareSetupRoot(t), Runtime: newFakeRuntime(), Store: &fakeStore{},
		Databases:     &fakeDatabaseFactory{database: db},
		ObjectStorage: &fakeStorageFactory{storage: objects}, MediaTool: &fakeMediaTool{},
		ListenerProbe: &fakeListener{}, Passwords: fakePasswords{}, SecretGenerator: fixedSecret,
	})
	input := validSetupInput()
	input.StorageAction = storageActionReset
	if _, err := service.Complete(context.Background(), input, "trace-storage-reset"); err != nil {
		t.Fatal(err)
	}
	if db.reset || !slices.Contains(objects.calls, "clear") {
		t.Fatalf("storage reset affected the wrong resource: database=%v storage=%#v", db.reset, objects.calls)
	}
}

func TestStorageConfigurationRejectsClientInaccessibleAddresses(t *testing.T) {
	for _, endpoint := range []string{
		"http://127.0.0.1:9000", "http://127.20.30.40:9000", "http://localhost:9000",
		"http://0.0.0.0:9000", "http://[::1]:9000", "http://[::]:9000",
	} {
		input := validSetupInput().Storage
		input.Endpoint = endpoint
		if _, err := storageConfig(input); !apperror.IsCode(err, apperror.CodeValidationError) {
			t.Fatalf("storage endpoint %q was accepted: %v", endpoint, err)
		}
	}
	input := validSetupInput().Storage
	publicURL := "http://127.0.0.1:9000"
	input.PublicBaseURL = &publicURL
	if _, err := storageConfig(input); !apperror.IsCode(err, apperror.CodeValidationError) {
		t.Fatalf("loopback public base URL was accepted: %v", err)
	}
}

func TestMediaPathsUseSystemPathWheneverSelectedValuesAreBlank(t *testing.T) {
	service := mustService(t, Options{
		RootDirectory: prepareSetupRoot(t), Runtime: newFakeRuntime(), Store: &fakeStore{},
		Databases:     &fakeDatabaseFactory{database: &fakeDatabase{}},
		ObjectStorage: &fakeStorageFactory{storage: &fakeStorage{}}, MediaTool: &fakeMediaTool{},
		ListenerProbe: &fakeListener{}, Passwords: fakePasswords{}, SecretGenerator: fixedSecret,
	})
	empty := ""
	paths, err := service.resolveMediaPaths(MediaInput{FFmpegPath: &empty, FFprobePath: &empty})
	if err != nil {
		t.Fatal(err)
	}
	if paths.FFmpegPath != "ffmpeg" || paths.FFprobePath != "ffprobe" {
		t.Fatalf("blank advanced paths did not use PATH commands: %#v", paths)
	}
	paths, err = service.resolveMediaPaths(MediaInput{Directory: &empty})
	if err != nil {
		t.Fatal(err)
	}
	if paths.FFmpegPath != "ffmpeg" || paths.FFprobePath != "ffprobe" {
		t.Fatalf("blank automatic directory did not use PATH commands: %#v", paths)
	}
}

func prepareSetupRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, directory := range []string{"migrations/meta", "admin", "music", "tools"} {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(directory)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	journal := `{"entries":[{"tag":"0000_test","when":1}]}`
	if err := os.WriteFile(filepath.Join(root, "migrations", "meta", "_journal.json"), []byte(journal), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "migrations", "0000_test.sql"), []byte("select 1;"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "admin", "index.html"), []byte("<!doctype html>"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tool := range []string{"ffmpeg.exe", "ffprobe.exe", "fpcalc.exe"} {
		if err := os.WriteFile(filepath.Join(root, "tools", tool), []byte("test"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func validSetupInput() SetupInput {
	forcePathStyle := true
	enabled := true
	syncOnStartup := false
	registration := false
	ffmpeg := "tools/ffmpeg.exe"
	ffprobe := "tools/ffprobe.exe"
	return SetupInput{
		HTTP: HTTPInput{
			Host: "0.0.0.0", Port: 3000,
			TrustedProxyAddresses: []string{},
		},
		Paths: PathsInput{MigrationsDirectory: "migrations", AdminWebDirectory: "admin"},
		Database: DatabaseInput{
			Host: "127.0.0.1", Port: 5432, Database: "xymusic", Username: "xymusic",
			Password: "database-password", SSLMode: "disable", MaxConnections: 10,
		},
		Storage: StorageInput{
			Endpoint: "http://minio.internal:9000", Region: "us-east-1", Bucket: "xymusic",
			AccessKeyID: "access-key", SecretAccessKey: "secret-key", ForcePathStyle: &forcePathStyle,
			SignedURLTTLSeconds: 300, MaxUploadBytes: 1024,
		},
		Media: MediaInput{FFmpegPath: &ffmpeg, FFprobePath: &ffprobe},
		Source: SourceInput{
			Name: "Music", Directory: "music", Mode: "READ_ONLY", Enabled: &enabled,
			SyncOnStartup: &syncOnStartup, IncludePatterns: []string{}, ExcludePatterns: []string{},
		},
		Registration: RegistrationInput{Enabled: &registration},
		Administrator: AdministratorInput{
			Username: "administrator", DisplayName: "Administrator", Password: "strong-password-1234",
		},
	}
}

func mustService(t *testing.T, options Options) *Service {
	t.Helper()
	service, err := NewService(options)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func fixedSecret() (string, error) { return strings.Repeat("s", 43), nil }

func fmtInvalidConfiguration() error {
	return errors.Join(ErrInvalidConfiguration, errors.New("Backend .env is invalid"))
}

type eventLog struct {
	mu     sync.Mutex
	events []string
}

func (log *eventLog) add(event string) {
	if log == nil {
		return
	}
	log.mu.Lock()
	log.events = append(log.events, event)
	log.mu.Unlock()
}

func (log *eventLog) snapshot() []string {
	if log == nil {
		return nil
	}
	log.mu.Lock()
	defer log.mu.Unlock()
	return append([]string(nil), log.events...)
}

func assertEventOrder(t *testing.T, actual, expected []string) {
	t.Helper()
	position := 0
	for _, event := range actual {
		if position < len(expected) && event == expected[position] {
			position++
		}
	}
	if position != len(expected) {
		t.Fatalf("events do not contain expected order\nactual:   %#v\nexpected: %#v", actual, expected)
	}
}

type fakeRuntime struct {
	mu       sync.Mutex
	snapshot RuntimeSnapshot
	events   *eventLog
}

type nonActivatingRuntime struct{ closed bool }

func (*nonActivatingRuntime) Status() RuntimeSnapshot {
	return RuntimeSnapshot{Phase: "SETUP_REQUIRED", Source: RuntimeSourceSetup}
}
func (*nonActivatingRuntime) Initialize(context.Context, config.Config, string) error { return nil }
func (runtime *nonActivatingRuntime) Close(context.Context) error {
	runtime.closed = true
	return nil
}

func newFakeRuntime() *fakeRuntime {
	return &fakeRuntime{snapshot: RuntimeSnapshot{Phase: "SETUP_REQUIRED", Source: RuntimeSourceSetup}}
}

func (runtime *fakeRuntime) Status() RuntimeSnapshot {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	return runtime.snapshot
}

func (runtime *fakeRuntime) Initialize(_ context.Context, _ config.Config, source string) error {
	runtime.events.add("runtime.initialize")
	runtime.mu.Lock()
	runtime.snapshot.Phase = RuntimePhaseReady
	runtime.snapshot.Source = source
	runtime.snapshot.Generation++
	runtime.mu.Unlock()
	return nil
}

func (runtime *fakeRuntime) Close(context.Context) error {
	runtime.events.add("runtime.close")
	runtime.mu.Lock()
	runtime.snapshot = RuntimeSnapshot{Phase: "SETUP_REQUIRED", Source: RuntimeSourceSetup}
	runtime.mu.Unlock()
	return nil
}

type fakeStore struct {
	events   *eventLog
	exists   bool
	loadErr  error
	clearErr error
	saved    config.Config
}

func (store *fakeStore) Load(context.Context) (config.Config, bool, error) {
	store.events.add("store.load")
	return store.saved, store.exists, store.loadErr
}

func (store *fakeStore) Save(_ context.Context, candidate config.Config) error {
	store.events.add("store.save")
	store.saved = candidate
	store.exists = true
	return nil
}

func (store *fakeStore) Clear(context.Context) error {
	store.events.add("store.clear")
	if store.clearErr != nil {
		return store.clearErr
	}
	store.exists = false
	store.saved = config.Config{}
	return nil
}

type fakeDatabaseFactory struct {
	database *fakeDatabase
	events   *eventLog
	openErr  error
}

func (factory *fakeDatabaseFactory) Open(context.Context, config.Database) (InstallationDatabase, error) {
	factory.events.add("database.open")
	if factory.openErr != nil {
		return nil, factory.openErr
	}
	return factory.database, nil
}

type fakeDatabase struct {
	events              *eventLog
	provisioned         ProvisionInput
	migrationsDirectory string
	compensated         bool
	auditErr            error
	inspection          InstallationInspection
	reset               bool
}

func (database *fakeDatabase) Ping(context.Context) error {
	database.events.add("database.ping")
	return nil
}

func (database *fakeDatabase) CanCreateInCurrentSchema(context.Context) (bool, error) {
	database.events.add("database.permission")
	return true, nil
}

func (database *fakeDatabase) CheckMigrationCompatibility(_ context.Context, directory string) error {
	database.events.add("database.compatibility")
	database.migrationsDirectory = directory
	return nil
}

func (database *fakeDatabase) RunMigrations(_ context.Context, directory string) error {
	database.events.add("database.migrate")
	database.migrationsDirectory = directory
	return nil
}

func (database *fakeDatabase) Inspect(context.Context, string) (InstallationInspection, error) {
	database.events.add("database.inspect")
	inspection := database.inspection
	if inspection.State == "" {
		inspection.State = DatabaseStateEmpty
	}
	return inspection, nil
}

func (database *fakeDatabase) Reset(context.Context) error {
	database.events.add("database.reset")
	database.reset = true
	return nil
}

func (database *fakeDatabase) Provision(_ context.Context, input ProvisionInput) (ProvisionedInstallation, error) {
	database.events.add("database.provision")
	database.provisioned = input
	return ProvisionedInstallation{
		AdministratorID: "administrator-id", LibraryRootID: "root-id",
		CreatedAdministrator: true, CreatedLibraryRoot: true,
	}, nil
}

func (database *fakeDatabase) Compensate(context.Context, ProvisionedInstallation, string) error {
	database.events.add("database.compensate")
	database.compensated = true
	return nil
}

func (database *fakeDatabase) RecordSetupSuccess(context.Context, string, string, string) error {
	database.events.add("database.audit")
	return database.auditErr
}

func (database *fakeDatabase) Close() { database.events.add("database.close") }

type fakeStorageFactory struct {
	storage *fakeStorage
	events  *eventLog
}

func (factory *fakeStorageFactory) Open(config.Storage) (SetupObjectStorage, error) {
	factory.events.add("storage.open")
	return factory.storage, nil
}

type fakeStorage struct {
	events        *eventLog
	calls         []string
	createBucket  bool
	bucketRemoved bool
	inspection    StorageInspection
}

func (storage *fakeStorage) call(value string) {
	storage.calls = append(storage.calls, value)
	storage.events.add("storage." + value)
}

func (storage *fakeStorage) Probe(context.Context) error { storage.call("probe"); return nil }
func (storage *fakeStorage) Inspect(context.Context) (StorageInspection, error) {
	storage.call("inspect")
	return storage.inspection, nil
}
func (storage *fakeStorage) EnsureBucket(context.Context) (bool, error) {
	storage.call("ensure")
	return storage.createBucket, nil
}
func (storage *fakeStorage) VerifyReadWrite(context.Context) error {
	storage.call("verify")
	return nil
}
func (storage *fakeStorage) Clear(context.Context) error { storage.call("clear"); return nil }
func (storage *fakeStorage) RemoveBucket(context.Context) error {
	storage.call("removeBucket")
	storage.bucketRemoved = true
	return nil
}
func (storage *fakeStorage) Close() { storage.call("close") }

type fakeMediaTool struct {
	events *eventLog
	labels []string
	err    error
}

func (tool *fakeMediaTool) Version(_ context.Context, _ string, label string) (string, error) {
	tool.labels = append(tool.labels, label)
	tool.events.add("media." + label)
	if tool.err != nil {
		return "", tool.err
	}
	return label + " version test", nil
}

type fakeListener struct {
	events *eventLog
	host   string
	port   int
	calls  []ListenerAddress
	err    error
}

func (listener *fakeListener) Check(_ context.Context, host string, port int) error {
	listener.events.add("listener.check")
	listener.host = host
	listener.port = port
	listener.calls = append(listener.calls, ListenerAddress{Host: host, Port: port})
	return listener.err
}

type fakePasswords struct{ events *eventLog }

func (passwords fakePasswords) Hash(string) (string, error) {
	passwords.events.add("password.hash")
	return "$argon2id$test", nil
}
