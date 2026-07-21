package identity

// RegistrationDTO is returned after a user account is created. Its JSON
// shape intentionally matches the legacy TypeScript API.
type RegistrationDTO struct {
	UserID   string     `json:"userId"`
	Username string     `json:"username"`
	Status   UserStatus `json:"status"`
}

// ArtworkDTO describes an avatar that can be downloaded by an API client.
type ArtworkDTO struct {
	AssetID  string `json:"assetId"`
	URL      string `json:"url"`
	CacheKey string `json:"cacheKey"`
	MimeType string `json:"mimeType"`
	// ExpiresAt remains in API v1 for client compatibility. Stable artwork resources set it to null.
	ExpiresAt *string `json:"expiresAt"`
	Width     *int    `json:"width,omitempty"`
	Height    *int    `json:"height,omitempty"`
}

// CurrentUserDTO is the canonical authenticated-user representation used by
// login, refresh and GET /api/v1/users/me.
type CurrentUserDTO struct {
	ID          string      `json:"id"`
	Username    string      `json:"username"`
	DisplayName string      `json:"displayName"`
	Bio         *string     `json:"bio"`
	Avatar      *ArtworkDTO `json:"avatar"`
	Role        UserRole    `json:"role"`
	Status      UserStatus  `json:"status"`
	Version     int         `json:"version"`
	CreatedAt   string      `json:"createdAt"`
	UpdatedAt   string      `json:"updatedAt"`
}

// AuthSessionDTO is returned by login and refresh.
type AuthSessionDTO struct {
	User    CurrentUserDTO `json:"user"`
	Session SessionDTO     `json:"session"`
	Tokens  TokensDTO      `json:"tokens"`
}

type SessionDTO struct {
	ID         string `json:"id"`
	DeviceName string `json:"deviceName"`
	CreatedAt  string `json:"createdAt"`
}

type TokensDTO struct {
	TokenType             string `json:"tokenType"`
	AccessToken           string `json:"accessToken"`
	AccessTokenExpiresAt  string `json:"accessTokenExpiresAt"`
	RefreshToken          string `json:"refreshToken"`
	RefreshTokenExpiresAt string `json:"refreshTokenExpiresAt"`
}

// RefreshResult carries transport metadata separately from the response body.
// HTTP adapters use Replayed for X-Idempotent-Replay and serialize Session.
type RefreshResult struct {
	Session  AuthSessionDTO
	Replayed bool
}
