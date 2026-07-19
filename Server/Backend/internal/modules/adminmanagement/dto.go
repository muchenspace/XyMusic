package adminmanagement

import (
	"bytes"
	"encoding/json"

	"xymusic/server/internal/modules/catalog"
)

type UserRole string

const (
	RoleUser  UserRole = "USER"
	RoleAdmin UserRole = "ADMIN"
)

type UserStatus string

const (
	StatusActive    UserStatus = "ACTIVE"
	StatusSuspended UserStatus = "SUSPENDED"
	StatusDeleted   UserStatus = "DELETED"
)

type UserDTO struct {
	ID          string              `json:"id"`
	Username    string              `json:"username"`
	DisplayName string              `json:"displayName"`
	Bio         *string             `json:"bio"`
	Role        UserRole            `json:"role"`
	Status      UserStatus          `json:"status"`
	Version     int                 `json:"version"`
	CreatedAt   string              `json:"createdAt"`
	UpdatedAt   string              `json:"updatedAt"`
	Avatar      *catalog.ArtworkDTO `json:"avatar"`
}

type SessionDTO struct {
	ID             string  `json:"id"`
	InstallationID string  `json:"installationId"`
	DeviceName     string  `json:"deviceName"`
	Platform       string  `json:"platform"`
	AppVersion     string  `json:"appVersion"`
	Active         bool    `json:"active"`
	CreatedAt      string  `json:"createdAt"`
	LastSeenAt     string  `json:"lastSeenAt"`
	RevokedAt      *string `json:"revokedAt"`
}

type UserDetailDTO struct {
	UserDTO
	Sessions          []SessionDTO `json:"sessions"`
	SessionPage       int          `json:"sessionPage"`
	SessionPageSize   int          `json:"sessionPageSize"`
	SessionTotal      int          `json:"sessionTotal"`
	SessionTotalPages int          `json:"sessionTotalPages"`
}

type UserPageDTO struct {
	Items      []UserDTO `json:"items"`
	Page       int       `json:"page"`
	PageSize   int       `json:"pageSize"`
	Total      int       `json:"total"`
	TotalPages int       `json:"totalPages"`
}

type ActorDTO struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
}

type DashboardActivityDTO struct {
	ID         string         `json:"id"`
	Action     string         `json:"action"`
	TargetType string         `json:"targetType"`
	TargetID   *string        `json:"targetId"`
	Result     string         `json:"result"`
	TraceID    string         `json:"traceId"`
	Details    map[string]any `json:"details"`
	Actor      *ActorDTO      `json:"actor"`
	CreatedAt  string         `json:"createdAt"`
}

type DashboardDTO struct {
	Users struct {
		Total          int `json:"total"`
		Active         int `json:"active"`
		Administrators int `json:"administrators"`
	} `json:"users"`
	Catalog struct {
		Artists int            `json:"artists"`
		Albums  int            `json:"albums"`
		Tracks  map[string]int `json:"tracks"`
	} `json:"catalog"`
	Sources        map[string]int         `json:"sources"`
	Jobs           map[string]int         `json:"jobs"`
	RecentActivity []DashboardActivityDTO `json:"recentActivity"`
}

type ListUsersInput struct {
	Page     int
	PageSize int
	Query    string
	Role     UserRole
	Status   UserStatus
}

type SessionPageInput struct {
	Page     int
	PageSize int
}

type CreateUserInput struct {
	Username    string   `json:"username"`
	Password    string   `json:"password"`
	DisplayName string   `json:"displayName"`
	Role        UserRole `json:"role"`
}

type OptionalString struct {
	Set   bool
	Value string
}

func (value *OptionalString) UnmarshalJSON(raw []byte) error {
	value.Set = true
	return json.Unmarshal(raw, &value.Value)
}

type OptionalNullableString struct {
	Set   bool
	Value *string
}

func (value *OptionalNullableString) UnmarshalJSON(raw []byte) error {
	value.Set = true
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		value.Value = nil
		return nil
	}
	var decoded string
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return err
	}
	value.Value = &decoded
	return nil
}

type OptionalRole struct {
	Set   bool
	Value UserRole
}

func (value *OptionalRole) UnmarshalJSON(raw []byte) error {
	value.Set = true
	return json.Unmarshal(raw, &value.Value)
}

type OptionalStatus struct {
	Set   bool
	Value UserStatus
}

func (value *OptionalStatus) UnmarshalJSON(raw []byte) error {
	value.Set = true
	return json.Unmarshal(raw, &value.Value)
}

type UpdateUserInput struct {
	ExpectedVersion int                    `json:"expectedVersion"`
	Username        OptionalString         `json:"username"`
	DisplayName     OptionalString         `json:"displayName"`
	Bio             OptionalNullableString `json:"bio"`
	Role            OptionalRole           `json:"role"`
	Status          OptionalStatus         `json:"status"`
	Reason          string                 `json:"reason"`
}

type PasswordInput struct {
	ExpectedVersion int    `json:"expectedVersion"`
	Password        string `json:"password"`
	Reason          string `json:"reason"`
}

type VersionReasonInput struct {
	ExpectedVersion int    `json:"expectedVersion"`
	Reason          string `json:"reason"`
}

type ReasonInput struct {
	Reason string `json:"reason"`
}

type StatusInput struct {
	ExpectedVersion int        `json:"expectedVersion"`
	Status          UserStatus `json:"status"`
	Reason          string     `json:"reason"`
}

type UpdatedDTO struct {
	Updated bool `json:"updated"`
}

type RevokedDTO struct {
	Revoked bool `json:"revoked"`
}
