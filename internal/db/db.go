package db

import (
	"fmt"

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

	if err := db.AutoMigrate(&model.User{}); err != nil {
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	return db, nil
}
