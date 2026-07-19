package adminmanagement

import (
	"encoding/json"
	"time"
)

type UserRecord struct {
	ID               string
	Username         string
	DisplayName      string
	Bio              *string
	Role             UserRole
	Status           UserStatus
	AuthVersion      int
	Version          int
	AvatarAssetID    *string
	CreatedAt        time.Time
	UserUpdatedAt    time.Time
	ProfileUpdatedAt time.Time
}

type SessionRecord struct {
	ID             string
	InstallationID string
	DeviceName     string
	Platform       string
	AppVersion     string
	CreatedAt      time.Time
	LastSeenAt     time.Time
	RevokedAt      *time.Time
}

type ListUsersQuery struct {
	Search string
	Role   UserRole
	Status UserStatus
	Limit  int
	Offset int
}

type SessionQuery struct {
	Limit  int
	Offset int
}

type CreateUserParams struct {
	Username           string
	NormalizedUsername string
	PasswordHash       string
	DisplayName        string
	Role               UserRole
}

type UpdateUserParams struct {
	ActorID            string
	UserID             string
	ExpectedVersion    int
	Username           *string
	NormalizedUsername *string
	DisplayName        *string
	SetBio             bool
	Bio                *string
	Role               *UserRole
	Status             *UserStatus
}

type DashboardCounts struct {
	UsersTotal          int
	UsersActive         int
	UsersAdministrators int
	Artists             int
	Albums              int
	Tracks              map[string]int
	Sources             map[string]int
	Jobs                map[string]int
	RecentActivity      []AuditRecord
}

type AuditRecord struct {
	ID               string
	ActorID          *string
	ActorUsername    *string
	ActorDisplayName *string
	Action           string
	TargetType       string
	TargetID         *string
	Result           string
	TraceID          string
	Details          map[string]any
	CreatedAt        time.Time
}

type AuditWrite struct {
	ActorID  string
	Action   string
	TargetID string
	TraceID  string
	Details  map[string]any
}

func (write AuditWrite) JSONDetails() ([]byte, error) {
	return json.Marshal(write.Details)
}
