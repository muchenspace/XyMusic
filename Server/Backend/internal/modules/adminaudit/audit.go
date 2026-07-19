package adminaudit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"xymusic/server/internal/modules/adminmanagement"
	"xymusic/server/internal/shared/apperror"
	"xymusic/server/internal/shared/pagination"
)

type ListInput struct {
	Page, PageSize                                                               int
	Search, ActorID, Action, TargetType, TargetID, Result, From, To, Sort, Order string
}
type ActorDTO struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
}
type ItemDTO struct {
	ID           string         `json:"id"`
	Actor        *ActorDTO      `json:"actor"`
	Action       string         `json:"action"`
	ResourceType string         `json:"resourceType"`
	ResourceID   *string        `json:"resourceId"`
	Result       string         `json:"result"`
	TraceID      string         `json:"traceId"`
	Metadata     map[string]any `json:"metadata"`
	CreatedAt    string         `json:"createdAt"`
}
type PageDTO struct {
	Items      []ItemDTO `json:"items"`
	Page       int       `json:"page"`
	PageSize   int       `json:"pageSize"`
	Total      int       `json:"total"`
	TotalPages int       `json:"totalPages"`
}
type Service struct{ pool *pgxpool.Pool }

func NewService(pool *pgxpool.Pool) (*Service, error) {
	if pool == nil {
		return nil, errors.New("admin audit database is required")
	}
	return &Service{pool: pool}, nil
}
func (service *Service) List(ctx context.Context, input ListInput) (PageDTO, error) {
	page, err := pagination.ParseOffset(input.Page, input.PageSize, 25)
	if err != nil {
		return PageDTO{}, err
	}
	if input.Sort == "" {
		input.Sort = "createdAt"
	}
	if input.Order == "" {
		input.Order = "desc"
	}
	columns := map[string]string{"createdAt": "a.created_at", "action": "a.action", "result": "a.result", "targetType": "a.target_type"}
	column := columns[input.Sort]
	if column == "" || (input.Order != "asc" && input.Order != "desc") {
		return PageDTO{}, apperror.Validation("Audit query is invalid")
	}
	from, toInclusive, toExclusive, err := dateRange(input.From, input.To)
	if err != nil {
		return PageDTO{}, err
	}
	arguments := []any{}
	conditions := []string{}
	add := func(value any) int { arguments = append(arguments, value); return len(arguments) }
	if input.ActorID != "" {
		conditions = append(conditions, fmt.Sprintf("a.actor_id=$%d", add(input.ActorID)))
	}
	if action := strings.TrimSpace(input.Action); action != "" {
		conditions = append(conditions, fmt.Sprintf("a.action ILIKE $%d ESCAPE E'\\\\'", add(escapeLike(action)+"%")))
	}
	if target := strings.TrimSpace(input.TargetType); target != "" {
		conditions = append(conditions, fmt.Sprintf("a.target_type ILIKE $%d ESCAPE E'\\\\'", add(target)))
	}
	if input.TargetID != "" {
		conditions = append(conditions, fmt.Sprintf("a.target_id=$%d", add(input.TargetID)))
	}
	if input.Result != "" {
		conditions = append(conditions, fmt.Sprintf("a.result=$%d", add(input.Result)))
	}
	if from != nil {
		conditions = append(conditions, fmt.Sprintf("a.created_at >= $%d", add(*from)))
	}
	if toExclusive != nil {
		conditions = append(conditions, fmt.Sprintf("a.created_at < $%d", add(*toExclusive)))
	} else if toInclusive != nil {
		conditions = append(conditions, fmt.Sprintf("a.created_at <= $%d", add(*toInclusive)))
	}
	if search := strings.TrimSpace(input.Search); search != "" {
		p := add("%" + escapeLike(search) + "%")
		conditions = append(conditions, fmt.Sprintf(`(a.action ILIKE $%d ESCAPE E'\\' OR a.target_type ILIKE $%d ESCAPE E'\\' OR a.trace_id ILIKE $%d ESCAPE E'\\' OR u.username ILIKE $%d ESCAPE E'\\' OR p.display_name ILIKE $%d ESCAPE E'\\' OR a.target_id::text ILIKE $%d ESCAPE E'\\')`, p, p, p, p, p, p))
	}
	where := ""
	if len(conditions) > 0 {
		where = " WHERE " + strings.Join(conditions, " AND ")
	}
	countArgs := append([]any(nil), arguments...)
	limitPos := add(page.PageSize)
	offsetPos := add(page.Offset)
	statement := `SELECT a.id,a.actor_id,u.username,p.display_name,a.action,a.target_type,a.target_id,a.result::text,a.trace_id,a.details,a.created_at FROM audit_logs a LEFT JOIN users u ON u.id=a.actor_id LEFT JOIN user_profiles p ON p.user_id=u.id` + where + fmt.Sprintf(" ORDER BY %s %s,a.id %s LIMIT $%d OFFSET $%d", column, input.Order, input.Order, limitPos, offsetPos)
	rows, err := service.pool.Query(ctx, statement, arguments...)
	if err != nil {
		return PageDTO{}, fmt.Errorf("query admin audit: %w", err)
	}
	items := []ItemDTO{}
	for rows.Next() {
		var item ItemDTO
		var actorID, username, displayName *string
		var details []byte
		var created time.Time
		if err := rows.Scan(&item.ID, &actorID, &username, &displayName, &item.Action, &item.ResourceType, &item.ResourceID, &item.Result, &item.TraceID, &details, &created); err != nil {
			rows.Close()
			return PageDTO{}, err
		}
		item.CreatedAt = created.UTC().Truncate(time.Millisecond).Format("2006-01-02T15:04:05.000Z")
		if actorID != nil && username != nil {
			name := *username
			if displayName != nil {
				name = *displayName
			}
			item.Actor = &ActorDTO{ID: *actorID, Username: *username, DisplayName: name}
		}
		var metadata map[string]any
		if err := json.Unmarshal(details, &metadata); err != nil {
			rows.Close()
			return PageDTO{}, err
		}
		item.Metadata = adminmanagement.RedactAuditMap(metadata)
		items = append(items, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return PageDTO{}, err
	}
	var total int
	if err := service.pool.QueryRow(ctx, `SELECT count(*)::int FROM audit_logs a LEFT JOIN users u ON u.id=a.actor_id LEFT JOIN user_profiles p ON p.user_id=u.id`+where, countArgs...).Scan(&total); err != nil {
		return PageDTO{}, err
	}
	return PageDTO{Items: items, Page: page.Page, PageSize: page.PageSize, Total: total, TotalPages: pagination.BoundedTotalPages(total, page.PageSize)}, nil
}

func dateRange(fromValue, toValue string) (*time.Time, *time.Time, *time.Time, error) {
	var from, toInclusive, toExclusive *time.Time
	var err error
	if fromValue != "" {
		v, e := parseDate(fromValue)
		if e != nil {
			return nil, nil, nil, apperror.Validation("from is not a valid date")
		}
		from = &v
	}
	if toValue != "" {
		v, e := parseDate(toValue)
		if e != nil {
			return nil, nil, nil, apperror.Validation("to is not a valid date")
		}
		if dateOnly(toValue) {
			v = v.Add(24 * time.Hour)
			toExclusive = &v
		} else {
			toInclusive = &v
		}
	}
	upper := toInclusive
	if toExclusive != nil {
		upper = toExclusive
	}
	if from != nil && upper != nil && !from.Before(*upper) {
		err = apperror.Validation("from must be earlier than to")
	}
	return from, toInclusive, toExclusive, err
}
func parseDate(value string) (time.Time, error) {
	candidate := strings.TrimSpace(value)
	if dateOnly(candidate) {
		return time.Parse("2006-01-02", candidate)
	}
	return time.Parse(time.RFC3339Nano, candidate)
}
func dateOnly(value string) bool {
	if len(value) != 10 {
		return false
	}
	_, err := time.Parse("2006-01-02", value)
	return err == nil
}
func escapeLike(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `%`, `\%`)
	return strings.ReplaceAll(value, `_`, `\_`)
}
