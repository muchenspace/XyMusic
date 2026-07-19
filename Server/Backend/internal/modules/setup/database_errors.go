package setup

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"os"
	"strings"
	"syscall"

	"github.com/jackc/pgx/v5/pgconn"

	"xymusic/server/internal/platform/database"
	"xymusic/server/internal/shared/apperror"
)

func databaseConnectionFailure(cause error) error {
	if classified := classifyKnownDatabaseFailure(cause); classified != nil {
		return classified
	}
	return newDatabaseFailure(
		apperror.CodeDatabaseConnectionFailed,
		"数据库连接未能建立，请检查数据库配置和 PostgreSQL 服务日志。",
		cause,
		nil,
	)
}

func databasePermissionCheckFailure(cause error) error {
	if classified := classifyKnownDatabaseFailure(cause); classified != nil {
		return classified
	}
	return newDatabaseFailure(
		apperror.CodeDatabaseConnectionFailed,
		"验证数据库用户权限时查询失败，请检查 PostgreSQL 服务状态后重试。",
		cause,
		nil,
	)
}

func databasePermissionDenied(cause error) *apperror.Error {
	return newDatabaseFailure(
		apperror.CodeDatabasePermissionDenied,
		"数据库用户没有在当前 Schema 中创建对象和执行迁移所需的权限。",
		cause,
		map[string][]string{
			"username": {"该数据库用户缺少当前 Schema 的 CREATE 权限"},
		},
	)
}

func databaseInspectionFailure(cause error) error {
	if classified := classifyKnownDatabaseFailure(cause); classified != nil {
		return classified
	}
	return newDatabaseFailure(
		apperror.CodeDatabaseConnectionFailed,
		"检查数据库中的现有 XyMusic 配置时查询失败。",
		cause,
		nil,
	)
}

func databaseMigrationCompatibilityFailure(cause error) error {
	var compatibility *database.CompatibilityError
	if errors.As(cause, &compatibility) {
		return newDatabaseFailure(
			apperror.CodeDatabaseMigrationIncompatible,
			databaseCompatibilityDetail(compatibility.Kind),
			cause,
			nil,
		)
	}
	if classified := classifyKnownDatabaseFailure(cause); classified != nil {
		return classified
	}
	return newDatabaseFailure(
		apperror.CodeDatabaseMigrationFailed,
		"检查数据库迁移兼容性时失败，请检查迁移目录和 PostgreSQL 日志。",
		cause,
		nil,
	)
}

func databaseMigrationFailure(cause error) error {
	var compatibility *database.CompatibilityError
	if errors.As(cause, &compatibility) {
		return newDatabaseFailure(
			apperror.CodeDatabaseMigrationIncompatible,
			databaseCompatibilityDetail(compatibility.Kind),
			cause,
			nil,
		)
	}
	if classified := classifyKnownDatabaseFailure(cause); classified != nil {
		return classified
	}
	return newDatabaseFailure(
		apperror.CodeDatabaseMigrationFailed,
		"执行数据库迁移时失败，请检查数据库权限、磁盘空间和 PostgreSQL 日志。",
		cause,
		nil,
	)
}

func classifyKnownDatabaseFailure(cause error) *apperror.Error {
	if cause == nil {
		return nil
	}
	if applicationError, ok := apperror.As(cause); ok {
		return applicationError
	}

	var pgError *pgconn.PgError
	if errors.As(cause, &pgError) {
		switch pgError.Code {
		case "3D000":
			return newDatabaseFailure(
				apperror.CodeDatabaseNotFound,
				"数据库名不存在，请确认数据库已经创建且名称填写正确。",
				cause,
				map[string][]string{"database": {"PostgreSQL 中不存在该数据库"}},
			)
		case "28P01", "28000":
			return newDatabaseFailure(
				apperror.CodeDatabaseAuthenticationFailed,
				"数据库认证失败，用户名、密码或 pg_hba.conf 认证规则不匹配。",
				cause,
				map[string][]string{
					"username": {"数据库用户名、密码或认证规则不匹配"},
					"password": {"数据库用户名、密码或认证规则不匹配"},
				},
			)
		case "42501":
			return databasePermissionDenied(cause)
		case "53300":
			return newDatabaseFailure(
				apperror.CodeDatabaseConnectionLimit,
				"PostgreSQL 连接数已满，当前无法接受新的初始化连接。",
				cause,
				nil,
			)
		case "57P03":
			return newDatabaseFailure(
				apperror.CodeDatabaseEndpointUnreachable,
				"PostgreSQL 正在启动、关闭或恢复，当前暂不接受连接。",
				cause,
				nil,
			)
		}
		if strings.HasPrefix(pgError.Code, "08") {
			return newDatabaseFailure(
				apperror.CodeDatabaseConnectionFailed,
				"PostgreSQL 连接在建立或通信过程中失败。",
				cause,
				nil,
			)
		}
	}

	if isDatabaseTLSError(cause) {
		return newDatabaseFailure(
			apperror.CodeDatabaseTLSFailed,
			"数据库 SSL 连接失败，请检查 SSL 模式、服务器证书和证书主机名。",
			cause,
			map[string][]string{"sslMode": {"当前 SSL 模式或服务器证书无法建立安全连接"}},
		)
	}

	var dnsError *net.DNSError
	if errors.As(cause, &dnsError) {
		return newDatabaseFailure(
			apperror.CodeDatabaseHostUnresolved,
			"无法解析数据库主机名，请检查数据库 IP、域名和 DNS 配置。",
			cause,
			map[string][]string{"host": {"数据库主机名无法解析"}},
		)
	}

	if errors.Is(cause, context.DeadlineExceeded) || errors.Is(cause, os.ErrDeadlineExceeded) || isNetworkTimeout(cause) {
		return newDatabaseFailure(
			apperror.CodeDatabaseConnectionTimeout,
			"连接数据库超时，目标地址未在限定时间内响应。",
			cause,
			map[string][]string{
				"host": {"数据库地址连接超时"},
				"port": {"数据库端口连接超时"},
			},
		)
	}

	if errors.Is(cause, syscall.ECONNREFUSED) || errors.Is(cause, syscall.ENETUNREACH) || errors.Is(cause, syscall.EHOSTUNREACH) {
		return databaseEndpointUnreachable(cause)
	}
	var operationError *net.OpError
	if errors.As(cause, &operationError) {
		return databaseEndpointUnreachable(cause)
	}

	diagnostic := strings.ToLower(cause.Error())
	if strings.Contains(diagnostic, "no such host") || strings.Contains(diagnostic, "hostname resolving") {
		return newDatabaseFailure(
			apperror.CodeDatabaseHostUnresolved,
			"无法解析数据库主机名，请检查数据库 IP、域名和 DNS 配置。",
			cause,
			map[string][]string{"host": {"数据库主机名无法解析"}},
		)
	}
	if strings.Contains(diagnostic, "connection refused") {
		return databaseEndpointUnreachable(cause)
	}
	if strings.Contains(diagnostic, "timeout") || strings.Contains(diagnostic, "timed out") {
		return newDatabaseFailure(
			apperror.CodeDatabaseConnectionTimeout,
			"连接数据库超时，目标地址未在限定时间内响应。",
			cause,
			map[string][]string{
				"host": {"数据库地址连接超时"},
				"port": {"数据库端口连接超时"},
			},
		)
	}
	return nil
}

func databaseEndpointUnreachable(cause error) *apperror.Error {
	return newDatabaseFailure(
		apperror.CodeDatabaseEndpointUnreachable,
		"无法连接指定的数据库地址和端口，请确认 PostgreSQL 已启动并检查防火墙。",
		cause,
		map[string][]string{
			"host": {"无法连接该数据库地址"},
			"port": {"该端口未接受 PostgreSQL 连接"},
		},
	)
}

func isNetworkTimeout(cause error) bool {
	var networkError net.Error
	return errors.As(cause, &networkError) && networkError.Timeout()
}

func isDatabaseTLSError(cause error) bool {
	var certificateVerification *tls.CertificateVerificationError
	if errors.As(cause, &certificateVerification) {
		return true
	}
	var unknownAuthority x509.UnknownAuthorityError
	if errors.As(cause, &unknownAuthority) {
		return true
	}
	var hostnameError x509.HostnameError
	if errors.As(cause, &hostnameError) {
		return true
	}
	var certificateInvalid x509.CertificateInvalidError
	if errors.As(cause, &certificateInvalid) {
		return true
	}
	var recordHeader tls.RecordHeaderError
	if errors.As(cause, &recordHeader) {
		return true
	}
	diagnostic := strings.ToLower(cause.Error())
	return strings.Contains(diagnostic, "tls:") ||
		strings.Contains(diagnostic, "ssl connection") ||
		strings.Contains(diagnostic, "ssl is not enabled") ||
		strings.Contains(diagnostic, "server does not support ssl") ||
		strings.Contains(diagnostic, "server refused tls") ||
		strings.Contains(diagnostic, "certificate") ||
		strings.Contains(diagnostic, "x509:")
}

func databaseCompatibilityDetail(kind database.CompatibilityErrorKind) string {
	switch kind {
	case database.CompatibilityNewerSchema:
		return "当前数据库由更高版本的 XyMusic 迁移，不能使用此版本继续初始化。"
	case database.CompatibilityHistoryForked:
		return "数据库迁移历史与当前 XyMusic 版本不是同一条升级链，不能自动迁移。"
	case database.CompatibilityHashMismatch:
		return "数据库迁移记录的校验值与当前 XyMusic 版本不一致，不能自动迁移。"
	case database.CompatibilityHistoryInvalid:
		return "数据库迁移历史已损坏或格式无效，不能自动迁移。"
	default:
		return "数据库迁移历史与当前 XyMusic 版本不兼容，不能自动迁移。"
	}
}

func newDatabaseFailure(
	code apperror.Code,
	detail string,
	cause error,
	fieldErrors map[string][]string,
) *apperror.Error {
	metadata := make(map[string]any)
	if len(fieldErrors) > 0 {
		metadata["fieldErrors"] = fieldErrors
	}
	return apperror.New(
		code,
		detail,
		apperror.WithCause(cause),
		apperror.WithMetadata(metadata),
	)
}
