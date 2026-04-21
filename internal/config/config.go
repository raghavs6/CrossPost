package config

import (
	"fmt"
	"net/url"
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

	// OAuth / JWT fields.
	// JWTSecret is always required: without it the server cannot sign or verify
	// any token, so we fail fast rather than silently issue insecure tokens.
	// Google OAuth is also validated at startup because the auth routes are
	// always mounted; failing early is clearer than redirecting users into a
	// broken external login flow.
	GoogleClientID     string
	GoogleClientSecret string
	GoogleRedirectURL  string
	JWTSecret          string

	// FrontendURL is where the backend redirects after a successful OAuth
	// login.  Defaults to http://127.0.0.1:5173 for local development so the
	// frontend host matches local OAuth callback hostnames exactly.
	FrontendURL string

	// Twitter OAuth 2.0 — optional.  The server starts without these and
	// disables the Twitter connect flow gracefully when they are absent.
	// Use TwitterEnabled() to check before mounting the routes.
	TwitterClientID     string
	TwitterClientSecret string
	TwitterRedirectURL  string
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

		GoogleClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
		GoogleClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
		GoogleRedirectURL:  os.Getenv("GOOGLE_REDIRECT_URL"),
		JWTSecret:          os.Getenv("JWT_SECRET"),
		FrontendURL:        getEnvOrDefault("FRONTEND_URL", "http://127.0.0.1:5173"),

		TwitterClientID:     os.Getenv("TWITTER_CLIENT_ID"),
		TwitterClientSecret: os.Getenv("TWITTER_CLIENT_SECRET"),
		TwitterRedirectURL:  getEnvOrDefault("TWITTER_REDIRECT_URL", "http://127.0.0.1:8080/api/auth/twitter/callback"),
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

// TwitterEnabled reports whether all three Twitter OAuth fields are set.
// Unlike Google OAuth, Twitter OAuth is optional — the server starts without
// it and gracefully disables the Twitter connect routes when it is absent.
func (c *Config) TwitterEnabled() bool {
	return c.TwitterClientID != "" &&
		c.TwitterClientSecret != "" &&
		c.TwitterRedirectURL != ""
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
		"JWT_SECRET":        c.JWTSecret,
	}

	for name, val := range required {
		if val == "" {
			return fmt.Errorf("missing required environment variable: %s", name)
		}
	}

	if err := c.validateGoogleOAuth(); err != nil {
		return err
	}

	return nil
}

func (c *Config) validateGoogleOAuth() error {
	required := map[string]string{
		"GOOGLE_CLIENT_ID":     c.GoogleClientID,
		"GOOGLE_CLIENT_SECRET": c.GoogleClientSecret,
		"GOOGLE_REDIRECT_URL":  c.GoogleRedirectURL,
	}

	for name, val := range required {
		if val == "" {
			return fmt.Errorf("missing required environment variable: %s", name)
		}
	}

	redirectURL, err := url.Parse(c.GoogleRedirectURL)
	if err != nil || !redirectURL.IsAbs() || redirectURL.Host == "" {
		return fmt.Errorf("invalid GOOGLE_REDIRECT_URL: must be an absolute URL")
	}

	return nil
}

func getEnvOrDefault(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}
