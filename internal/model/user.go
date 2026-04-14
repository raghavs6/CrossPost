package model

import "gorm.io/gorm"

// User represents a registered account in CrossPost.
// gorm.Model adds: ID (uint, primary key), CreatedAt, UpdatedAt, DeletedAt.
//
// Both PasswordHash and GoogleID are pointers (*string) so they can be NULL
// in the database.  A Google-authenticated user has no password; an
// email-registered user has no GoogleID.  Using a pointer maps to SQL NULL,
// which is the correct way to represent "no value" — an empty string ""
// would be semantically wrong and would also break the unique index on
// GoogleID (SQL allows multiple NULLs in a unique index; it does not allow
// multiple empty strings).
type User struct {
	gorm.Model
	Email        string  `gorm:"uniqueIndex;not null"`
	PasswordHash *string `gorm:"default:null"`
	GoogleID     *string `gorm:"uniqueIndex"`
}
