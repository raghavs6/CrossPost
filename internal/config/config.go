package config

import (
	"fmt"
	"os"
)

// Config holds all environment-driven configuration for the application.
// Every value is read once at startup and passed to the rest of the app.
type Config struct {
	ServerPort string

	PostgresUser     string
	PostgresPassword string
	PostgresDB       string
	PostgresHost     string
	PostgresPort     string

	RedisHost string
	RedisPort string
}

// Load reads environment variables into a Config struct.
// godotenv.Load() should be called before this so that .env values
// are already present in the environment.
func Load() (*Config, error) {
	cfg := &Config{
		ServerPort: getEnvOrDefault("SERVER_PORT", "8080"),

		PostgresUser:     os.Getenv("POSTGRES_USER"),
		PostgresPassword: os.Getenv("POSTGRES_PASSWORD"),
		PostgresDB:       os.Getenv("POSTGRES_DB"),
		PostgresHost:     os.Getenv("POSTGRES_HOST"),
		PostgresPort:     os.Getenv("POSTGRES_PORT"),

		RedisHost: os.Getenv("REDIS_HOST"),
		RedisPort: os.Getenv("REDIS_PORT"),
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// DSN returns the PostgreSQL connection string for GORM.
func (c *Config) DSN() string {
	return fmt.Sprintf(
		"host=%s user=%s password=%s dbname=%s port=%s sslmode=disable",
		c.PostgresHost, c.PostgresUser, c.PostgresPassword, c.PostgresDB, c.PostgresPort,
	)
}

// RedisAddr returns the Redis address in "host:port" format for Asynq.
func (c *Config) RedisAddr() string {
	return fmt.Sprintf("%s:%s", c.RedisHost, c.RedisPort)
}

// validate returns an error if any required field is missing.
func (c *Config) validate() error {
	required := map[string]string{
		"POSTGRES_USER":     c.PostgresUser,
		"POSTGRES_PASSWORD": c.PostgresPassword,
		"POSTGRES_DB":       c.PostgresDB,
		"POSTGRES_HOST":     c.PostgresHost,
		"POSTGRES_PORT":     c.PostgresPort,
		"REDIS_HOST":        c.RedisHost,
		"REDIS_PORT":        c.RedisPort,
	}

	for name, val := range required {
		if val == "" {
			return fmt.Errorf("missing required environment variable: %s", name)
		}
	}

	return nil
}

func getEnvOrDefault(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}
