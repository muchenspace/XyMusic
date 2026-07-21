package identity

import "time"

type UserRole string

const (
	RoleUser  UserRole = "USER"
	RoleAdmin UserRole = "ADMIN"
)

type UserStatus string

const (
	UserStatusActive    UserStatus = "ACTIVE"
	UserStatusSuspended UserStatus = "SUSPENDED"
	UserStatusDeleted   UserStatus = "DELETED"
)

type DevicePlatform string

const (
	DevicePlatformAndroid DevicePlatform = "ANDROID"
	DevicePlatformWindows DevicePlatform = "WINDOWS"
	DevicePlatformWeb     DevicePlatform = "WEB"
)

type RegisterInput struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type DeviceInfoInput struct {
	InstallationID string         `json:"installationId"`
	Name           string         `json:"name"`
	Platform       DevicePlatform `json:"platform"`
	AppVersion     string         `json:"appVersion"`
}

type LoginInput struct {
	Username string          `json:"username"`
	Password string          `json:"password"`
	Device   DeviceInfoInput `json:"device"`
}

// AuthenticatedActor is the authorization context handed to downstream
// application services after both the JWT and its backing session are checked.
type AuthenticatedActor struct {
	UserID      string
	SessionID   string
	Role        UserRole
	AuthVersion int
}

type User struct {
	ID                 string
	Username           string
	NormalizedUsername string
	PasswordHash       string
	Role               UserRole
	Status             UserStatus
	AuthVersion        int
	Version            int
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type AuthSession struct {
	ID             string
	UserID         string
	InstallationID string
	DeviceName     string
	Platform       DevicePlatform
	AppVersion     string
	CreatedAt      time.Time
	LastSeenAt     time.Time
	RevokedAt      *time.Time
}

type RefreshToken struct {
	ID            string
	SessionID     string
	TokenHash     string
	FamilyID      string
	ParentTokenID *string
	CreatedAt     time.Time
	ExpiresAt     time.Time
	UsedAt        *time.Time
	RevokedAt     *time.Time
}

type RefreshRecord struct {
	Token   RefreshToken
	Session AuthSession
	User    User
}

type AvatarAsset struct {
	ID             string
	MimeType       string
	ChecksumSHA256 *string
	Width          *int
	Height         *int
	UpdatedAt      time.Time
}

type CurrentUserRecord struct {
	User             User
	DisplayName      string
	Bio              *string
	ProfileUpdatedAt time.Time
	Avatar           *AvatarAsset
}
