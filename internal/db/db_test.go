package db

import (
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/raghavs6/CrossPost/internal/model"
)

func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}

	return db
}

func createLegacySocialAccountsTable(t *testing.T, db *gorm.DB) {
	t.Helper()

	if err := db.Exec(`
		CREATE TABLE social_accounts (
			id integer primary key autoincrement,
			created_at datetime,
			updated_at datetime,
			deleted_at datetime,
			user_id integer not null,
			platform text not null,
			platform_user_id text not null,
			username text not null,
			access_token text not null,
			refresh_token text,
			token_expiry datetime
		)
	`).Error; err != nil {
		t.Fatalf("create legacy social_accounts table: %v", err)
	}
}

func TestRunCompatibilityMigrations_BackfillsLegacySocialAccounts(t *testing.T) {
	db := openTestDB(t)
	createLegacySocialAccountsTable(t, db)

	if err := db.Exec(`
		INSERT INTO social_accounts (user_id, platform, platform_user_id, username, access_token)
		VALUES
			(1, 'twitter', 'twitter-user-1', 'myhandle', 'token-1'),
			(2, 'facebook', 'fb-user-2', '', 'token-2')
	`).Error; err != nil {
		t.Fatalf("seed legacy rows: %v", err)
	}

	if err := runCompatibilityMigrations(db); err != nil {
		t.Fatalf("runCompatibilityMigrations: %v", err)
	}

	type socialAccountProjection struct {
		UserID      uint
		DisplayName string
	}

	var rows []socialAccountProjection
	if err := db.Table("social_accounts").Order("user_id asc").Find(&rows).Error; err != nil {
		t.Fatalf("load migrated rows: %v", err)
	}

	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	if rows[0].DisplayName != "myhandle" {
		t.Errorf("expected username backfill for first row, got %q", rows[0].DisplayName)
	}
	if rows[1].DisplayName != "fb-user-2" {
		t.Errorf("expected platform_user_id fallback for second row, got %q", rows[1].DisplayName)
	}
}

func TestRunCompatibilityMigrations_AllowsAutoMigrateAfterLegacySchema(t *testing.T) {
	db := openTestDB(t)
	createLegacySocialAccountsTable(t, db)

	if err := db.Exec(`
		INSERT INTO social_accounts (user_id, platform, platform_user_id, username, access_token)
		VALUES (1, 'twitter', 'twitter-user-1', 'legacy-user', 'token-1')
	`).Error; err != nil {
		t.Fatalf("seed legacy row: %v", err)
	}

	if err := runCompatibilityMigrations(db); err != nil {
		t.Fatalf("runCompatibilityMigrations: %v", err)
	}

	if err := db.AutoMigrate(&model.User{}, &model.Post{}, &model.SocialAccount{}, &model.PendingFacebookPageLink{}); err != nil {
		t.Fatalf("AutoMigrate after compatibility migration: %v", err)
	}

	if !db.Migrator().HasColumn(&model.SocialAccount{}, "DisplayName") {
		t.Fatal("expected social_accounts.display_name to exist after migration")
	}
}
