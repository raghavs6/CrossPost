package db

import (
	"fmt"
	"strings"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/raghavs6/CrossPost/internal/config"
	"github.com/raghavs6/CrossPost/internal/model"
)

// Connect opens a connection to PostgreSQL and runs AutoMigrate.
// Returns a *gorm.DB that the rest of the app uses for all queries.
func Connect(cfg *config.Config) (*gorm.DB, error) {
	db, err := gorm.Open(postgres.Open(cfg.DSN()), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	if err := runCompatibilityMigrations(db); err != nil {
		return nil, fmt.Errorf("failed to run compatibility migrations: %w", err)
	}

	if err := db.AutoMigrate(&model.User{}, &model.Post{}, &model.SocialAccount{}, &model.PendingFacebookPageLink{}); err != nil {
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	return db, nil
}

func runCompatibilityMigrations(db *gorm.DB) error {
	if !db.Migrator().HasTable(&model.SocialAccount{}) {
		return nil
	}

	return ensureSocialAccountsDisplayName(db)
}

func ensureSocialAccountsDisplayName(db *gorm.DB) error {
	if db.Migrator().HasColumn(&model.SocialAccount{}, "DisplayName") {
		return nil
	}

	if err := db.Exec(`ALTER TABLE social_accounts ADD COLUMN display_name text`).Error; err != nil {
		return fmt.Errorf("add social_accounts.display_name: %w", err)
	}

	if err := db.Exec(`
		UPDATE social_accounts
		SET display_name = COALESCE(
			NULLIF(TRIM(username), ''),
			NULLIF(TRIM(platform_user_id), ''),
			platform
		)
	`).Error; err != nil {
		return fmt.Errorf("backfill social_accounts.display_name: %w", err)
	}

	if strings.EqualFold(db.Dialector.Name(), "postgres") {
		if err := db.Exec(`ALTER TABLE social_accounts ALTER COLUMN display_name SET NOT NULL`).Error; err != nil {
			return fmt.Errorf("set social_accounts.display_name not null: %w", err)
		}
	}

	return nil
}
