package model

import (
	"time"

	"gorm.io/gorm"
)

// SocialAccount stores OAuth tokens for a connected social media platform.
// Each user can have at most one connection per platform — enforced by the
// composite unique index idx_user_platform.
//
// PlatformUserID is the platform's own numeric/string identifier for the user
// (e.g. Twitter's numeric user ID).  This lets us detect if a different Twitter
// account tries to link to the same CrossPost user.
//
// DisplayName is the provider-agnostic human label we can show in the UI.
// Twitter also exposes a username/handle, but providers like Facebook are
// better represented by a display name only.
//
// AccessToken and RefreshToken are stored as TEXT (not VARCHAR) because OAuth
// tokens can be long strings and we never need to index them.
type SocialAccount struct {
	gorm.Model
	UserID         uint   `gorm:"not null;index;uniqueIndex:idx_user_platform"`
	Platform       string `gorm:"not null;uniqueIndex:idx_user_platform"` // e.g. "twitter"
	PlatformUserID string `gorm:"not null"`                               // platform's own user ID
	DisplayName    string `gorm:"not null"`
	Username       string
	AccessToken    string `gorm:"not null;type:text"`
	RefreshToken   string `gorm:"type:text"`
	TokenExpiry    time.Time
}
