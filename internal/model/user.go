package model

import "gorm.io/gorm"

// User represents a registered account in CrossPost.
// gorm.Model adds: ID (uint, primary key), CreatedAt, UpdatedAt, DeletedAt.
type User struct {
	gorm.Model
	Email        string `gorm:"uniqueIndex;not null"`
	PasswordHash string `gorm:"not null"`
}
