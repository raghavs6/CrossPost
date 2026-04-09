package config

import (
	"testing"
)

// setEnv is a helper that sets environment variables for a test
// and automatically restores them when the test finishes.
func setEnv(t *testing.T, vars map[string]string) {
	t.Helper()
	for k, v := range vars {
		t.Setenv(k, v)
	}
}

var validEnv = map[string]string{
	"POSTGRES_USER":     "testuser",
	"POSTGRES_PASSWORD": "testpass",
	"POSTGRES_DB":       "testdb",
	"POSTGRES_HOST":     "localhost",
	"POSTGRES_PORT":     "5432",
	"REDIS_HOST":        "localhost",
	"REDIS_PORT":        "6379",
}

func TestLoad_Success(t *testing.T) {
	setEnv(t, validEnv)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if cfg.PostgresUser != "testuser" {
		t.Errorf("expected PostgresUser=testuser, got %q", cfg.PostgresUser)
	}
	if cfg.ServerPort != "8080" {
		t.Errorf("expected default ServerPort=8080, got %q", cfg.ServerPort)
	}
}

func TestLoad_CustomServerPort(t *testing.T) {
	setEnv(t, validEnv)
	t.Setenv("SERVER_PORT", "9090")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if cfg.ServerPort != "9090" {
		t.Errorf("expected ServerPort=9090, got %q", cfg.ServerPort)
	}
}

func TestLoad_MissingRequired(t *testing.T) {
	requiredKeys := []string{
		"POSTGRES_USER",
		"POSTGRES_PASSWORD",
		"POSTGRES_DB",
		"POSTGRES_HOST",
		"POSTGRES_PORT",
		"REDIS_HOST",
		"REDIS_PORT",
	}

	for _, missing := range requiredKeys {
		t.Run("missing_"+missing, func(t *testing.T) {
			setEnv(t, validEnv)
			t.Setenv(missing, "") // blank out the required var

			_, err := Load()
			if err == nil {
				t.Errorf("expected error when %s is missing, got nil", missing)
			}
		})
	}
}

func TestDSN(t *testing.T) {
	setEnv(t, validEnv)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "host=localhost user=testuser password=testpass dbname=testdb port=5432 sslmode=disable"
	if cfg.DSN() != expected {
		t.Errorf("expected DSN %q, got %q", expected, cfg.DSN())
	}
}

func TestRedisAddr(t *testing.T) {
	setEnv(t, validEnv)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "localhost:6379"
	if cfg.RedisAddr() != expected {
		t.Errorf("expected RedisAddr %q, got %q", expected, cfg.RedisAddr())
	}
}
