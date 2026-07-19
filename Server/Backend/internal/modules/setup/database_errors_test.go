package setup

import (
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"strings"
	"syscall"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"xymusic/server/internal/platform/database"
	"xymusic/server/internal/shared/apperror"
)

func TestDatabaseFailureClassifierReturnsSpecificSafeErrors(t *testing.T) {
	tests := []struct {
		name       string
		cause      error
		code       apperror.Code
		detail     string
		fieldNames []string
	}{
		{
			name: "database does not exist", cause: wrappedPostgresError("3D000"),
			code: apperror.CodeDatabaseNotFound, detail: "数据库名不存在，请确认数据库已经创建且名称填写正确。",
			fieldNames: []string{"database"},
		},
		{
			name: "authentication failed", cause: wrappedPostgresError("28P01"),
			code: apperror.CodeDatabaseAuthenticationFailed, detail: "数据库认证失败，用户名、密码或 pg_hba.conf 认证规则不匹配。",
			fieldNames: []string{"password", "username"},
		},
		{
			name: "permission denied", cause: wrappedPostgresError("42501"),
			code: apperror.CodeDatabasePermissionDenied, detail: "数据库用户没有在当前 Schema 中创建对象和执行迁移所需的权限。",
			fieldNames: []string{"username"},
		},
		{
			name: "connection limit", cause: wrappedPostgresError("53300"),
			code: apperror.CodeDatabaseConnectionLimit, detail: "PostgreSQL 连接数已满，当前无法接受新的初始化连接。",
		},
		{
			name: "server starting", cause: wrappedPostgresError("57P03"),
			code: apperror.CodeDatabaseEndpointUnreachable, detail: "PostgreSQL 正在启动、关闭或恢复，当前暂不接受连接。",
		},
		{
			name: "connection protocol failure", cause: wrappedPostgresError("08006"),
			code: apperror.CodeDatabaseConnectionFailed, detail: "PostgreSQL 连接在建立或通信过程中失败。",
		},
		{
			name: "dns failure", cause: &net.DNSError{Err: "no such host", Name: "ssl-database.internal"},
			code: apperror.CodeDatabaseHostUnresolved, detail: "无法解析数据库主机名，请检查数据库 IP、域名和 DNS 配置。",
			fieldNames: []string{"host"},
		},
		{
			name: "connection refused", cause: &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED},
			code: apperror.CodeDatabaseEndpointUnreachable, detail: "无法连接指定的数据库地址和端口，请确认 PostgreSQL 已启动并检查防火墙。",
			fieldNames: []string{"host", "port"},
		},
		{
			name: "connection timeout", cause: databaseTimeoutError{},
			code: apperror.CodeDatabaseConnectionTimeout, detail: "连接数据库超时，目标地址未在限定时间内响应。",
			fieldNames: []string{"host", "port"},
		},
		{
			name: "tls verification", cause: x509.UnknownAuthorityError{},
			code: apperror.CodeDatabaseTLSFailed, detail: "数据库 SSL 连接失败，请检查 SSL 模式、服务器证书和证书主机名。",
			fieldNames: []string{"sslMode"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			classified := classifyKnownDatabaseFailure(test.cause)
			if classified == nil {
				t.Fatal("expected database error classification")
			}
			assertDatabaseApplicationError(t, classified, test.code, test.detail, test.fieldNames)
			if !errors.Is(classified, test.cause) {
				t.Fatal("classified error did not preserve its diagnostic cause")
			}
		})
	}
}

func TestUnknownDatabaseFailureUsesSafeControlledDetail(t *testing.T) {
	cause := errors.New("failed to connect with password=private-value postgresql://secret")
	err := databaseConnectionFailure(cause)
	applicationError, ok := apperror.As(err)
	if !ok {
		t.Fatalf("expected application error, got %T", err)
	}
	assertDatabaseApplicationError(
		t,
		applicationError,
		apperror.CodeDatabaseConnectionFailed,
		"数据库连接未能建立，请检查数据库配置和 PostgreSQL 服务日志。",
		nil,
	)
	if strings.Contains(applicationError.Detail, "private-value") || strings.Contains(applicationError.Detail, "postgresql://") {
		t.Fatalf("database diagnostic leaked through public detail: %q", applicationError.Detail)
	}
	if !errors.Is(applicationError, cause) {
		t.Fatal("unknown connection error did not preserve its cause")
	}
}

func TestDatabaseMigrationCompatibilityKindsHaveSpecificDetails(t *testing.T) {
	tests := []struct {
		kind   database.CompatibilityErrorKind
		detail string
	}{
		{database.CompatibilityNewerSchema, "当前数据库由更高版本的 XyMusic 迁移，不能使用此版本继续初始化。"},
		{database.CompatibilityHistoryForked, "数据库迁移历史与当前 XyMusic 版本不是同一条升级链，不能自动迁移。"},
		{database.CompatibilityHashMismatch, "数据库迁移记录的校验值与当前 XyMusic 版本不一致，不能自动迁移。"},
		{database.CompatibilityHistoryInvalid, "数据库迁移历史已损坏或格式无效，不能自动迁移。"},
	}
	for _, test := range tests {
		t.Run(string(test.kind), func(t *testing.T) {
			cause := &database.CompatibilityError{Kind: test.kind, Message: "internal migration diagnostic"}
			err := databaseMigrationCompatibilityFailure(cause)
			applicationError, ok := apperror.As(err)
			if !ok {
				t.Fatalf("expected application error, got %T", err)
			}
			assertDatabaseApplicationError(
				t,
				applicationError,
				apperror.CodeDatabaseMigrationIncompatible,
				test.detail,
				nil,
			)
		})
	}
}

func TestDatabaseMigrationFailureDoesNotExposeDriverDiagnostic(t *testing.T) {
	cause := errors.New("apply migration 0023_private: SQLSTATE 425XX private table")
	err := databaseMigrationFailure(cause)
	applicationError, ok := apperror.As(err)
	if !ok {
		t.Fatalf("expected application error, got %T", err)
	}
	assertDatabaseApplicationError(
		t,
		applicationError,
		apperror.CodeDatabaseMigrationFailed,
		"执行数据库迁移时失败，请检查数据库权限、磁盘空间和 PostgreSQL 日志。",
		nil,
	)
	if strings.Contains(applicationError.Detail, "0023_private") || strings.Contains(applicationError.Detail, "SQLSTATE") {
		t.Fatalf("migration diagnostic leaked through public detail: %q", applicationError.Detail)
	}
}

func assertDatabaseApplicationError(
	t *testing.T,
	err *apperror.Error,
	code apperror.Code,
	detail string,
	fieldNames []string,
) {
	t.Helper()
	if err.Code != code || err.Detail != detail {
		t.Fatalf("unexpected database error: code=%s detail=%q", err.Code, err.Detail)
	}
	fieldErrors, _ := err.Metadata["fieldErrors"].(map[string][]string)
	if len(fieldErrors) != len(fieldNames) {
		t.Fatalf("unexpected field errors: %#v", fieldErrors)
	}
	for _, field := range fieldNames {
		if len(fieldErrors[field]) != 1 || strings.TrimSpace(fieldErrors[field][0]) == "" {
			t.Fatalf("field %q is missing a useful message: %#v", field, fieldErrors)
		}
	}
}

func wrappedPostgresError(code string) error {
	return fmt.Errorf("database operation failed: %w", &pgconn.PgError{Code: code})
}

type databaseTimeoutError struct{}

func (databaseTimeoutError) Error() string   { return "database dial timeout" }
func (databaseTimeoutError) Timeout() bool   { return true }
func (databaseTimeoutError) Temporary() bool { return true }
