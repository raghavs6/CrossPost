package model

import (
	"time"

	"gorm.io/gorm"
)

// PendingFacebookPageLink stores short-lived Page choices after Facebook OAuth
// succeeds but before the user selects which Page CrossPost should publish to.
// The access token stored here is already Page-scoped, so it is safe to move
// into SocialAccount once the user confirms a choice.
type PendingFacebookPageLink struct {
	gorm.Model
	FlowID           string `gorm:"not null;index;uniqueIndex:idx_facebook_flow_page"`
	UserID           uint   `gorm:"not null;index"`
	FacebookUserID   string `gorm:"not null"`
	FacebookUserName string
	PageID           string    `gorm:"not null;uniqueIndex:idx_facebook_flow_page"`
	PageName         string    `gorm:"not null"`
	PageAccessToken  string    `gorm:"not null;type:text"`
	ExpiresAt        time.Time `gorm:"not null;index"`
}
