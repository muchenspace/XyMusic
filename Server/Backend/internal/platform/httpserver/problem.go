package httpserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"xymusic/server/internal/shared/apperror"
)

const (
	// ProblemMediaType is the RFC 7807 response media type.
	ProblemMediaType = "application/problem+json"
	problemTypeBase  = "https://xymusic.example/problems/"
)

// Problem is an RFC 7807 problem document with stable XYMusic extensions.
type Problem struct {
	Type       string         `json:"type"`
	Title      string         `json:"title"`
	Status     int            `json:"status"`
	Code       string         `json:"code"`
	Detail     string         `json:"detail"`
	Suggestion string         `json:"suggestion,omitempty"`
	Instance   string         `json:"instance,omitempty"`
	TraceID    string         `json:"traceId"`
	Extensions map[string]any `json:"-"`
}

// MarshalJSON flattens RFC extension members into the top-level document.
func (p Problem) MarshalJSON() ([]byte, error) {
	document := map[string]any{
		"type":       p.Type,
		"title":      p.Title,
		"status":     p.Status,
		"code":       p.Code,
		"detail":     p.Detail,
		"suggestion": p.Suggestion,
		"traceId":    p.TraceID,
	}
	for key, value := range p.Extensions {
		if _, reserved := reservedProblemMembers[key]; reserved {
			continue
		}
		document[key] = value
	}
	return json.Marshal(document)
}

type problemSpec struct {
	status        int
	title         string
	defaultDetail string
}

var applicationProblemSpecs = map[apperror.Code]problemSpec{
	apperror.CodeValidationError:               {http.StatusBadRequest, "请求参数有误", "填写或提交的内容有误，请检查后重试。"},
	apperror.CodeInvalidCursor:                 {http.StatusBadRequest, "分页参数无效", "分页位置已失效，请返回列表首页后重试。"},
	apperror.CodeAuthenticationRequired:        {http.StatusUnauthorized, "需要登录", "请先登录后再继续操作。"},
	apperror.CodeAccessTokenExpired:            {http.StatusUnauthorized, "登录状态已失效", "登录状态已过期，请重新登录。"},
	apperror.CodeSessionRevoked:                {http.StatusUnauthorized, "登录状态已失效", "当前会话已失效，请重新登录。"},
	apperror.CodeInvalidCredentials:            {http.StatusUnauthorized, "登录失败", "用户名或密码不正确，请检查后重试。"},
	apperror.CodeAccountSuspended:              {http.StatusForbidden, "账号已停用", "当前账号已停用，请联系管理员。"},
	apperror.CodeForbidden:                     {http.StatusForbidden, "权限不足", "你暂无权限执行此操作。"},
	apperror.CodeResourceNotFound:              {http.StatusNotFound, "资源不存在", "相关数据不存在或已被删除。"},
	apperror.CodeDuplicateUsername:             {http.StatusConflict, "用户名已存在", "该用户名已被使用，请更换后重试。"},
	apperror.CodeIdempotencyKeyReused:          {http.StatusConflict, "请求标识冲突", "请勿将同一请求标识用于不同操作。"},
	apperror.CodeVersionConflict:               {http.StatusConflict, "数据已发生变化", "数据已被其他操作更新，请刷新后重试。"},
	apperror.CodeResourceConflict:              {http.StatusConflict, "操作冲突", "当前数据状态不允许此操作，请刷新后重试。"},
	apperror.CodeInvalidStateTransition:        {http.StatusUnprocessableEntity, "当前状态不支持此操作", "请刷新数据并确认当前状态后重试。"},
	apperror.CodeTrackNotPlayable:              {http.StatusUnprocessableEntity, "暂时无法播放", "当前曲目没有可用的播放资源。"},
	apperror.CodeTrackAlreadyInPlaylist:        {http.StatusConflict, "曲目已在歌单中", "该曲目已经存在于当前歌单。"},
	apperror.CodeSourceFileDeleteFailed:        {http.StatusUnprocessableEntity, "源文件处理失败", "源文件未能安全移入待删除区，请检查文件权限后重试。"},
	apperror.CodeSourceFileRestoreFailed:       {http.StatusUnprocessableEntity, "源文件恢复失败", "删除操作已回滚，但部分源文件未能恢复，请立即检查服务端日志。"},
	apperror.CodeMediaUploadMismatch:           {http.StatusUnprocessableEntity, "上传校验失败", "上传文件与提交信息不一致，请重新选择文件后重试。"},
	apperror.CodePayloadTooLarge:               {http.StatusRequestEntityTooLarge, "提交内容过大", "提交内容超过允许大小，请缩小后重试。"},
	apperror.CodeRateLimited:                   {http.StatusTooManyRequests, "操作过于频繁", "请求过于频繁，请稍后重试。"},
	apperror.CodeDependencyUnavailable:         {http.StatusServiceUnavailable, "服务暂时不可用", "相关服务暂时不可用，请稍后重试。"},
	apperror.CodeDatabaseHostUnresolved:        {http.StatusUnprocessableEntity, "数据库地址无法解析", "数据库主机名无法解析，请检查地址和 DNS 配置。"},
	apperror.CodeDatabaseEndpointUnreachable:   {http.StatusServiceUnavailable, "无法连接数据库", "指定的数据库地址和端口未接受连接。"},
	apperror.CodeDatabaseConnectionTimeout:     {http.StatusServiceUnavailable, "数据库连接超时", "数据库未在限定时间内响应连接。"},
	apperror.CodeDatabaseNotFound:              {http.StatusUnprocessableEntity, "数据库不存在", "填写的数据库名不存在。"},
	apperror.CodeDatabaseAuthenticationFailed:  {http.StatusUnprocessableEntity, "数据库认证失败", "数据库用户名、密码或服务端认证规则不匹配。"},
	apperror.CodeDatabaseTLSFailed:             {http.StatusUnprocessableEntity, "数据库 SSL 连接失败", "数据库 SSL 模式或证书配置不正确。"},
	apperror.CodeDatabasePermissionDenied:      {http.StatusUnprocessableEntity, "数据库权限不足", "数据库用户缺少初始化所需权限。"},
	apperror.CodeDatabaseConnectionLimit:       {http.StatusServiceUnavailable, "数据库连接数已满", "数据库当前无法接受更多连接。"},
	apperror.CodeDatabaseConnectionFailed:      {http.StatusServiceUnavailable, "数据库连接失败", "数据库连接未能建立。"},
	apperror.CodeDatabaseMigrationFailed:       {http.StatusInternalServerError, "数据库迁移失败", "数据库迁移未能完成。"},
	apperror.CodeDatabaseMigrationIncompatible: {http.StatusConflict, "数据库迁移不兼容", "数据库迁移历史与当前 XyMusic 版本不兼容。"},
	apperror.CodeSetupDecisionRequired:         {http.StatusConflict, "检测到已有配置", "数据库中已存在管理员账号，请选择继续复用或全部清除。"},
	apperror.CodeSetupFailed:                   {http.StatusInternalServerError, "初始化未完成", "初始化在执行过程中失败，请根据失败阶段和追踪 ID 处理。"},
	apperror.CodeInternalError:                 {http.StatusInternalServerError, "服务异常", "操作未完成，请稍后重试；如问题持续出现，请联系管理员。"},
}

var (
	reservedProblemMembers = map[string]struct{}{
		"type": {}, "title": {}, "status": {}, "code": {}, "detail": {}, "suggestion": {}, "instance": {}, "traceId": {},
	}
	safeMetadataText = regexp.MustCompile(`^[A-Za-z0-9._:-]+$`)
	safeFieldName    = regexp.MustCompile(`^[A-Za-z0-9_.\[\]-]{1,128}$`)
	sensitiveDetails = []*regexp.Regexp{
		regexp.MustCompile(`[A-Za-z]:[\\/]`),
		regexp.MustCompile(`(?i)(?:postgres|postgresql)://`),
		regexp.MustCompile(`(?i)\bBearer\s+`),
		regexp.MustCompile(`\beyJ[A-Za-z0-9_-]+\.`),
		regexp.MustCompile(`\b(?:EACCES|EEXIST|EINVAL|EIO|ENOENT|ENOTDIR|EPERM|ETIMEDOUT|ECONNREFUSED|ECONNRESET|SQLSTATE)\b`),
		regexp.MustCompile(`\b(?:TypeError|ReferenceError|SyntaxError):`),
		regexp.MustCompile(`(?:^|\s)at\s+\S+\s*\(`),
	}
)

// HandlerFunc is a Gin handler that returns failures through the shared
// application error model.
type HandlerFunc func(*gin.Context) error

// Handle adapts an error-returning handler to Gin.
func Handle(handler HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := handler(c); err != nil {
			WriteError(c, err)
		}
	}
}

// ProblemHandler converts errors added with Context.Error into RFC 7807
// responses, provided the handler has not already committed a response.
func ProblemHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		if c.Writer.Written() || len(c.Errors) == 0 {
			return
		}
		WriteError(c, c.Errors.Last().Err)
	}
}

// WriteError maps an application or infrastructure error to a safe problem
// document and aborts the current Gin context.
func WriteError(c *gin.Context, err error) {
	problem := ProblemFromError(err, TraceID(c), requestInstance(c))
	writeProblem(c, problem)
}

// ProblemFromError maps transport-neutral failures to an RFC 7807 document.
func ProblemFromError(err error, traceID, instance string) Problem {
	if applicationError, ok := apperror.As(err); ok {
		spec, known := applicationProblemSpecs[applicationError.Code]
		if !known {
			return internalProblem(traceID, instance)
		}
		detail := localizedDetail(applicationError.Code, applicationError.Detail, spec.defaultDetail)
		extensions := publicMetadata(applicationError.Metadata)
		if applicationError.Code == apperror.CodeInternalError {
			extensions = nil
		}
		return Problem{
			Type:       problemType(string(applicationError.Code)),
			Title:      spec.title,
			Status:     spec.status,
			Code:       string(applicationError.Code),
			Detail:     truncateUTF8(detail, 1_000),
			Suggestion: problemSuggestion(applicationError.Code),
			Instance:   instance,
			TraceID:    traceID,
			Extensions: extensions,
		}
	}

	var maximumBytesError *http.MaxBytesError
	if errors.As(err, &maximumBytesError) {
		spec := applicationProblemSpecs[apperror.CodePayloadTooLarge]
		return Problem{
			Type:       problemType(string(apperror.CodePayloadTooLarge)),
			Title:      spec.title,
			Status:     spec.status,
			Code:       string(apperror.CodePayloadTooLarge),
			Detail:     spec.defaultDetail,
			Suggestion: problemSuggestion(apperror.CodePayloadTooLarge),
			Instance:   instance,
			TraceID:    traceID,
		}
	}

	return internalProblem(traceID, instance)
}

func writeProblem(c *gin.Context, problem Problem) {
	payload, err := json.Marshal(problem)
	if err != nil {
		problem = internalProblem(TraceID(c), requestInstance(c))
		payload, _ = json.Marshal(problem)
	}

	c.Header("Cache-Control", "no-store")
	if problem.Status == http.StatusUnauthorized {
		c.Header("WWW-Authenticate", "Bearer")
	}
	if retryAfter, ok := problem.Extensions["retryAfterSeconds"]; ok {
		if seconds, valid := safeInteger(retryAfter, 1, 86_400); valid {
			c.Header("Retry-After", fmt.Sprintf("%d", seconds))
		}
	}
	c.Abort()
	if c.Request.Method == http.MethodHead {
		c.Header("Content-Type", ProblemMediaType)
		c.Status(problem.Status)
		c.Writer.WriteHeaderNow()
		return
	}
	c.Data(problem.Status, ProblemMediaType, payload)
}

func newHTTPProblem(status int, code, title, detail, traceID, instance string) Problem {
	return Problem{
		Type:       problemType(code),
		Title:      title,
		Status:     status,
		Code:       code,
		Detail:     detail,
		Suggestion: problemSuggestion(apperror.Code(code)),
		Instance:   instance,
		TraceID:    traceID,
	}
}

func internalProblem(traceID, instance string) Problem {
	spec := applicationProblemSpecs[apperror.CodeInternalError]
	return Problem{
		Type:       problemType(string(apperror.CodeInternalError)),
		Title:      spec.title,
		Status:     spec.status,
		Code:       string(apperror.CodeInternalError),
		Detail:     spec.defaultDetail,
		Suggestion: problemSuggestion(apperror.CodeInternalError),
		Instance:   instance,
		TraceID:    traceID,
	}
}

func problemType(code string) string {
	return problemTypeBase + strings.ToLower(strings.ReplaceAll(code, "_", "-"))
}

func requestInstance(c *gin.Context) string {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return ""
	}
	return c.Request.URL.Path
}

func publicMetadata(metadata map[string]any) map[string]any {
	if len(metadata) == 0 {
		return nil
	}
	result := make(map[string]any)
	copySafeInteger(metadata, result, "expectedVersion", 0, math.MaxInt64)
	copySafeInteger(metadata, result, "currentVersion", 0, math.MaxInt64)
	copySafeInteger(metadata, result, "retryAfterSeconds", 1, 86_400)
	copySafeText(metadata, result, "conflictResourceType", 64)
	copySafeText(metadata, result, "conflictResourceId", 128)
	copySafeText(metadata, result, "albumId", 128)
	copySafeText(metadata, result, "trackId", 128)
	copySafeText(metadata, result, "setupStage", 64)
	copySafeText(metadata, result, "decisionResource", 64)
	copySafeText(metadata, result, "databaseState", 64)
	copySafeBoolean(metadata, result, "rollbackIncomplete")
	copySafeBoolean(metadata, result, "destructiveStageStarted")
	copySafeBoolean(metadata, result, "migrationRequired")
	copySafeTextList(metadata, result, "reusable", 20, 64)
	copySafeTextList(metadata, result, "missing", 20, 64)
	if fieldErrors := safeFieldErrors(metadata["fieldErrors"]); len(fieldErrors) > 0 {
		result["fieldErrors"] = fieldErrors
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func copySafeBoolean(source, target map[string]any, key string) {
	if value, ok := source[key].(bool); ok {
		target[key] = value
	}
}

func copySafeTextList(source, target map[string]any, key string, maximumItems, maximumLength int) {
	values, ok := source[key].([]string)
	if !ok || len(values) == 0 || len(values) > maximumItems {
		return
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" || len(value) > maximumLength || !safeMetadataText.MatchString(value) {
			return
		}
		result = append(result, value)
	}
	target[key] = result
}

func problemSuggestion(code apperror.Code) string {
	switch code {
	case apperror.CodeValidationError, apperror.CodeInvalidCursor:
		return "检查标记字段和输入格式，修正后重新提交。"
	case apperror.CodeAuthenticationRequired, apperror.CodeAccessTokenExpired, apperror.CodeSessionRevoked, apperror.CodeInvalidCredentials:
		return "重新登录后再执行操作；若仍失败，请确认账号和服务器地址。"
	case apperror.CodeForbidden, apperror.CodeAccountSuspended:
		return "使用具备相应权限的账号，或联系管理员检查账号状态。"
	case apperror.CodeResourceNotFound:
		return "刷新列表确认资源是否已被删除，并避免继续使用旧链接。"
	case apperror.CodeDuplicateUsername, apperror.CodeIdempotencyKeyReused, apperror.CodeVersionConflict, apperror.CodeResourceConflict:
		return "刷新当前数据，确认最新状态后重新操作。"
	case apperror.CodeInvalidStateTransition, apperror.CodeTrackNotPlayable, apperror.CodeTrackAlreadyInPlaylist:
		return "检查资源当前状态和关联数据，按页面提示调整后重试。"
	case apperror.CodeSourceFileDeleteFailed, apperror.CodeSourceFileRestoreFailed:
		return "检查服务端音乐目录权限和文件占用情况，并使用追踪 ID 查看日志。"
	case apperror.CodeMediaUploadMismatch, apperror.CodePayloadTooLarge:
		return "重新选择符合大小和格式要求的文件后上传。"
	case apperror.CodeRateLimited:
		return "等待一段时间后再试，避免连续重复提交。"
	case apperror.CodeDependencyUnavailable:
		return "检查数据库、对象存储和媒体工具状态，恢复依赖后重试。"
	case apperror.CodeDatabaseHostUnresolved:
		return "检查数据库主机名是否填写正确，并确认服务端 DNS 可以解析该地址。"
	case apperror.CodeDatabaseEndpointUnreachable:
		return "确认 PostgreSQL 已启动并监听该地址和端口，同时检查防火墙和网络路由。"
	case apperror.CodeDatabaseConnectionTimeout:
		return "检查数据库服务状态、网络连通性和防火墙规则后重试。"
	case apperror.CodeDatabaseNotFound:
		return "确认数据库已经创建且名称填写正确；不要填写 PostgreSQL 实例名或服务器名。"
	case apperror.CodeDatabaseAuthenticationFailed:
		return "重新核对用户名和密码，并检查 PostgreSQL 的 pg_hba.conf 认证规则。"
	case apperror.CodeDatabaseTLSFailed:
		return "核对 SSL 模式、服务器证书、证书主机名和 PostgreSQL SSL 配置。"
	case apperror.CodeDatabasePermissionDenied:
		return "为数据库用户授予当前数据库和 Schema 的连接、创建及迁移权限后重试。"
	case apperror.CodeDatabaseConnectionLimit:
		return "释放空闲连接或提高 PostgreSQL 最大连接数，确认服务恢复后重试。"
	case apperror.CodeDatabaseConnectionFailed:
		return "检查数据库地址、端口、服务状态和服务端日志，并使用追踪 ID 定位原因。"
	case apperror.CodeDatabaseMigrationFailed:
		return "检查数据库日志、用户权限、磁盘空间和迁移目录，并使用追踪 ID 定位失败步骤。"
	case apperror.CodeDatabaseMigrationIncompatible:
		return "先备份数据库，再确认服务端版本与数据库来源一致；不要手工删除迁移记录。"
	case apperror.CodeSetupDecisionRequired:
		return "确认已有数据是否需要保留，然后明确选择继续复用或全部清除。"
	case apperror.CodeSetupFailed:
		return "根据 setupStage 定位失败步骤；若已进入清除阶段，请先核对数据库和 Bucket 状态。"
	default:
		return "记录追踪 ID 并查看服务端日志；确认问题原因后再重试，避免重复提交。"
	}
}

func copySafeInteger(source, target map[string]any, key string, minimum, maximum int64) {
	if value, ok := safeInteger(source[key], minimum, maximum); ok {
		target[key] = value
	}
}

func safeInteger(value any, minimum, maximum int64) (int64, bool) {
	var candidate int64
	switch typed := value.(type) {
	case int:
		candidate = int64(typed)
	case int8:
		candidate = int64(typed)
	case int16:
		candidate = int64(typed)
	case int32:
		candidate = int64(typed)
	case int64:
		candidate = typed
	case uint:
		if uint64(typed) > math.MaxInt64 {
			return 0, false
		}
		candidate = int64(typed)
	case uint8:
		candidate = int64(typed)
	case uint16:
		candidate = int64(typed)
	case uint32:
		candidate = int64(typed)
	case uint64:
		if typed > math.MaxInt64 {
			return 0, false
		}
		candidate = int64(typed)
	default:
		return 0, false
	}
	return candidate, candidate >= minimum && candidate <= maximum
}

func copySafeText(source, target map[string]any, key string, maximumLength int) {
	value, ok := source[key].(string)
	if !ok || value == "" || len(value) > maximumLength || !safeMetadataText.MatchString(value) {
		return
	}
	target[key] = value
}

func safeFieldErrors(value any) map[string][]string {
	result := make(map[string][]string)
	switch typed := value.(type) {
	case map[string][]string:
		keys := sortedKeys(typed)
		for _, field := range keys[:min(len(keys), 100)] {
			copyFieldMessages(result, field, typed[field])
		}
	case map[string]any:
		keys := sortedKeys(typed)
		for _, field := range keys[:min(len(keys), 100)] {
			messages, ok := typed[field].([]string)
			if ok {
				copyFieldMessages(result, field, messages)
			}
		}
	}
	return result
}

func copyFieldMessages(target map[string][]string, field string, messages []string) {
	if !safeFieldName.MatchString(field) || unsafeFieldName(field) {
		return
	}
	publicMessages := make([]string, 0, min(len(messages), 10))
	for _, message := range messages[:min(len(messages), 10)] {
		publicMessages = append(publicMessages, localizedDetail(
			apperror.CodeValidationError,
			message,
			applicationProblemSpecs[apperror.CodeValidationError].defaultDetail,
		))
	}
	if len(publicMessages) > 0 {
		target[field] = publicMessages
	}
}

func localizedDetail(code apperror.Code, detail, fallback string) string {
	if code == apperror.CodeInternalError {
		return fallback
	}
	normalized := strings.TrimSpace(detail)
	if normalized == "" || !containsHan(normalized) || containsSensitiveDetail(normalized) {
		return fallback
	}
	return truncateUTF8(normalized, 1_000)
}

func containsHan(value string) bool {
	for _, character := range value {
		if character >= '\u3400' && character <= '\u9fff' {
			return true
		}
	}
	return false
}

func containsSensitiveDetail(value string) bool {
	for _, pattern := range sensitiveDetails {
		if pattern.MatchString(value) {
			return true
		}
	}
	return false
}

func unsafeFieldName(field string) bool {
	for _, part := range strings.FieldsFunc(field, func(character rune) bool {
		return character == '.' || character == '[' || character == ']' || character == '-'
	}) {
		if part == "__proto__" || part == "constructor" || part == "prototype" {
			return true
		}
	}
	return false
}

func sortedKeys[V any](input map[string]V) []string {
	keys := make([]string, 0, len(input))
	for key := range input {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func truncateUTF8(value string, maximumRunes int) string {
	if utf8.RuneCountInString(value) <= maximumRunes {
		return value
	}
	runes := []rune(value)
	return string(runes[:maximumRunes])
}
