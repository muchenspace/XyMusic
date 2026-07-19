package adminmanagement

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"xymusic/server/internal/config"
	"xymusic/server/internal/modules/identity"
	"xymusic/server/internal/platform/database"
	"xymusic/server/internal/testsupport"
)

// TestAdminManagementProductionLifecycle writes only uniquely named rows and
// removes them at the end. It exercises the real PostgreSQL schema and Argon2
// implementation used by the configured release environment.
func TestAdminManagementProductionLifecycle(t *testing.T) {
	environmentPath := os.Getenv("XYMUSIC_INTEGRATION_ENV")
	if environmentPath == "" {
		t.Skip("set XYMUSIC_INTEGRATION_ENV to run the production admin management lifecycle")
	}
	testsupport.RequireWriteIntegration(t)
	absoluteEnvironmentPath, err := filepath.Abs(environmentPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.NewStore(absoluteEnvironmentPath).Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg, err = config.ResolveRuntime(cfg, filepath.Dir(absoluteEnvironmentPath))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	pool, err := database.Open(ctx, cfg.Database)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	var actorID string
	if err := pool.QueryRow(ctx, `
		SELECT id FROM users WHERE role = 'ADMIN' AND status = 'ACTIVE' ORDER BY created_at LIMIT 1
	`).Scan(&actorID); err != nil {
		t.Skipf("configured database has no active administrator: %v", err)
	}

	repository := NewRepository(pool.Pool)
	service, err := NewService(ServiceDependencies{
		Store: repository, Artworks: managementArtworkStub{}, Passwords: identity.SecurityPasswordManager{},
	})
	if err != nil {
		t.Fatal(err)
	}
	suffix := strings.ReplaceAll(uuid.NewString()[:18], "-", "")
	username := "admin_it_" + suffix
	created, err := service.CreateUser(ctx, actorID, "admin-it-create", CreateUserInput{
		Username: username, Password: "integration-password", DisplayName: "Admin Integration", Role: RoleUser,
	})
	if err != nil {
		t.Fatal(err)
	}
	userID := created.ID
	t.Cleanup(func() {
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cleanupCancel()
		_, _ = pool.Exec(cleanupContext, `DELETE FROM audit_logs WHERE target_id = $1`, userID)
		_, _ = pool.Exec(cleanupContext, `DELETE FROM users WHERE id = $1`, userID)
	})

	sessionID := uuid.NewString()
	if _, err := pool.Exec(ctx, `
		INSERT INTO auth_sessions (id, user_id, installation_id, device_name, platform, app_version)
		VALUES ($1, $2, $3, 'Admin integration', 'ANDROID', 'integration/1')
	`, sessionID, userID, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO refresh_tokens (session_id, token_hash, family_id, expires_at)
		VALUES ($1, $2, $3, now() + interval '1 hour')
	`, sessionID, strings.Repeat("a", 32)+strings.ReplaceAll(uuid.NewString(), "-", "")[:32], uuid.NewString()); err != nil {
		t.Fatal(err)
	}

	page, err := service.ListUsers(ctx, ListUsersInput{Page: 1, PageSize: 25, Query: username})
	if err != nil || page.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != userID {
		t.Fatalf("ListUsers = %#v, %v", page, err)
	}
	bio := "production integration"
	updated, err := service.UpdateUser(ctx, actorID, "admin-it-update", userID, UpdateUserInput{
		ExpectedVersion: created.Version,
		DisplayName:     OptionalString{Set: true, Value: "Admin Integration Updated"},
		Bio:             OptionalNullableString{Set: true, Value: &bio},
		Reason:          "production integration",
	})
	if err != nil || updated.Version != created.Version+1 || updated.Bio == nil || *updated.Bio != bio {
		t.Fatalf("UpdateUser = %#v, %v", updated, err)
	}
	passwordResult, err := service.ResetPassword(ctx, actorID, "admin-it-password", userID, PasswordInput{
		ExpectedVersion: updated.Version, Password: "integration-password-2", Reason: "production integration",
	})
	if err != nil || !passwordResult.Updated {
		t.Fatalf("ResetPassword = %#v, %v", passwordResult, err)
	}
	detail, err := service.User(ctx, userID, SessionPageInput{})
	if err != nil || len(detail.Sessions) != 1 || detail.Sessions[0].Active {
		t.Fatalf("User after reset = %#v, %v", detail, err)
	}
	revoked, err := service.RevokeSession(ctx, actorID, "admin-it-revoke", userID, sessionID, "production integration")
	if err != nil || !revoked.Revoked {
		t.Fatalf("RevokeSession = %#v, %v", revoked, err)
	}
	if _, err := service.Dashboard(ctx); err != nil {
		t.Fatalf("Dashboard: %v", err)
	}
}
