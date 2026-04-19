package model

import (
	"time"

	"gorm.io/gorm"
)

// Post represents a scheduled social-media post created by a User.
//
// gorm.Model adds: ID (uint, primary key), CreatedAt, UpdatedAt, DeletedAt.
// DeletedAt enables soft-delete: the row is hidden (not removed) when deleted,
// so we keep a full audit trail of every post a user ever made.
//
// UserID is a non-pointer uint because every post MUST have an owner — making
// it non-nullable at the database level.  If UserID were *uint (pointer), a
// post could exist with NULL ownership, which should be impossible.
//
// Platforms stores a comma-separated list of target networks, e.g.
// "twitter,linkedin".  This is the simplest approach and easy to split in Go
// with strings.Split.  A normalised join table would be more correct at scale,
// but that's premature complexity for a v1.
//
// Status is a plain string so database rows are human-readable when debugging.
// Valid values: "draft" | "queued" | "published" | "failed".
type Post struct {
	gorm.Model
	UserID      uint      `gorm:"not null;index"`
	Content     string    `gorm:"not null"`
	Platforms   string    `gorm:"not null"`
	ScheduledAt time.Time `gorm:"not null"`
	Status      string    `gorm:"not null;default:'draft'"`
}
