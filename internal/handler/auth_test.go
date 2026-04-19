package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/oauth2"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/raghavs6/CrossPost/internal/model"
)

// setupTestDB opens an in-memory SQLite database and migrates the User schema.
// Using SQLite (not PostgreSQL) keeps tests fast and offline.
// The tradeoff: SQLite has minor dialect differences from PostgreSQL, but for
// basic CRUD operations the behaviour is identical.
func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		// Silence GORM's query logs during tests so output stays readable.
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("failed to open in-memory SQLite: %v", err)
	}
	if err := db.AutoMigrate(&model.User{}, &model.Post{}); err != nil {
		t.Fatalf("failed to migrate User and Post tables: %v", err)
	}
	return db
}

// setupAuthHandler creates a fully wired AuthHandler pointed at local test
// transports instead of real Google endpoints.
func setupAuthHandler(t *testing.T, db *gorm.DB, googleID, email string) *AuthHandler {
	t.Helper()

	h := &AuthHandler{
		oauthConf: &oauth2.Config{
			ClientID:     "test-client-id",
			ClientSecret: "test-client-secret",
			RedirectURL:  "http://localhost/callback",
			Endpoint: oauth2.Endpoint{
				AuthURL:  "https://accounts.google.test/o/oauth2/auth",
				TokenURL: "https://oauth.google.test/token",
			},
		},
		db:          db,
		jwtSecret:   []byte("test-jwt-secret"),
		userInfoURL: "https://oauth.google.test/userinfo",
		httpClient: &http.Client{
			Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				switch {
				case r.URL.String() == "https://oauth.google.test/token":
					return jsonResponse(http.StatusOK, map[string]any{
						"access_token": "fake-access-token",
						"token_type":   "Bearer",
						"expires_in":   3600,
					}), nil
				case strings.HasPrefix(r.URL.String(), "https://oauth.google.test/userinfo?access_token=fake-access-token"):
					return jsonResponse(http.StatusOK, googleUserInfo{ID: googleID, Email: email}), nil
				default:
					return jsonResponse(http.StatusNotFound, map[string]string{"error": "not found"}), nil
				}
			}),
		},
		frontendURL: "http://localhost:5173",
	}

	return h
}

// TestGenerateState verifies that generateState returns a 32-character
// lowercase hex string and that two calls produce different values.
func TestGenerateState(t *testing.T) {
	s1, err := generateState()
	if err != nil {
		t.Fatalf("generateState returned error: %v", err)
	}
	if len(s1) != 32 {
		t.Errorf("expected state length 32, got %d", len(s1))
	}

	s2, err := generateState()
	if err != nil {
		t.Fatalf("generateState returned error on second call: %v", err)
	}
	if s1 == s2 {
		t.Error("two calls to generateState returned the same value — not random")
	}
}

func TestGoogleLogin_MissingOAuthConfig(t *testing.T) {
	h := &AuthHandler{}
	req := httptest.NewRequest(http.MethodGet, "/api/auth/google", nil)
	w := httptest.NewRecorder()

	h.GoogleLogin(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when OAuth is misconfigured, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "google oauth is not configured") {
		t.Fatalf("expected configuration error message, got %q", w.Body.String())
	}
}

func TestGoogleLogin_RedirectsToGoogle(t *testing.T) {
	h := &AuthHandler{
		oauthConf: &oauth2.Config{
			ClientID:     "test-client-id",
			ClientSecret: "test-client-secret",
			RedirectURL:  "http://localhost/callback",
			Endpoint: oauth2.Endpoint{
				AuthURL: "https://accounts.google.test/o/oauth2/auth",
			},
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/auth/google", nil)
	w := httptest.NewRecorder()

	h.GoogleLogin(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusTemporaryRedirect {
		t.Fatalf("expected 307 redirect, got %d", resp.StatusCode)
	}
	location := resp.Header.Get("Location")
	if !strings.Contains(location, "client_id=test-client-id") {
		t.Fatalf("expected redirect to include client ID, got %q", location)
	}
	if !strings.Contains(location, "redirect_uri=http%3A%2F%2Flocalhost%2Fcallback") {
		t.Fatalf("expected redirect to include encoded redirect URI, got %q", location)
	}

	cookies := resp.Cookies()
	if len(cookies) == 0 || cookies[0].Name != "oauth_state" || cookies[0].Value == "" {
		t.Fatalf("expected oauth_state cookie to be set, got %+v", cookies)
	}
}

// TestIssueJWT verifies that a signed token can be parsed back and contains
// the correct user ID.
func TestIssueJWT(t *testing.T) {
	h := &AuthHandler{jwtSecret: []byte("test-secret")}
	tokenStr, err := h.issueJWT(42)
	if err != nil {
		t.Fatalf("issueJWT returned error: %v", err)
	}
	if tokenStr == "" {
		t.Fatal("issueJWT returned an empty string")
	}

	// Parse the token back and verify the claims.
	var claims jwtClaims
	token, err := jwt.ParseWithClaims(tokenStr, &claims, func(t *jwt.Token) (interface{}, error) {
		return []byte("test-secret"), nil
	})
	if err != nil {
		t.Fatalf("failed to parse issued token: %v", err)
	}
	if !token.Valid {
		t.Error("issued token is not valid")
	}
	if claims.UserID != 42 {
		t.Errorf("expected UserID=42, got %d", claims.UserID)
	}
	if claims.ExpiresAt == nil || claims.ExpiresAt.Before(time.Now()) {
		t.Error("token expiry is missing or already in the past")
	}
}

// TestFindOrCreateUser_NewUser verifies that a new Google user is inserted
// into the database on first login.
func TestFindOrCreateUser_NewUser(t *testing.T) {
	db := setupTestDB(t)
	h := &AuthHandler{db: db}

	info := &googleUserInfo{ID: "google-123", Email: "newuser@example.com"}
	user, err := h.findOrCreateUser(info)
	if err != nil {
		t.Fatalf("findOrCreateUser returned error: %v", err)
	}

	if user.ID == 0 {
		t.Error("expected user.ID to be set, got 0")
	}
	if user.Email != "newuser@example.com" {
		t.Errorf("expected email newuser@example.com, got %q", user.Email)
	}
	if user.GoogleID == nil || *user.GoogleID != "google-123" {
		t.Errorf("expected GoogleID=google-123, got %v", user.GoogleID)
	}

	// Confirm the row actually exists in the database.
	var count int64
	db.Model(&model.User{}).Where("email = ?", "newuser@example.com").Count(&count)
	if count != 1 {
		t.Errorf("expected 1 user in DB, got %d", count)
	}
}

// TestFindOrCreateUser_ExistingUser verifies that a returning Google user is
// found (not duplicated) on subsequent logins.
func TestFindOrCreateUser_ExistingUser(t *testing.T) {
	db := setupTestDB(t)
	h := &AuthHandler{db: db}

	info := &googleUserInfo{ID: "google-456", Email: "existing@example.com"}

	// First login — creates the user.
	first, err := h.findOrCreateUser(info)
	if err != nil {
		t.Fatalf("first findOrCreateUser returned error: %v", err)
	}

	// Second login — should return the same user, not create a new row.
	second, err := h.findOrCreateUser(info)
	if err != nil {
		t.Fatalf("second findOrCreateUser returned error: %v", err)
	}

	if first.ID != second.ID {
		t.Errorf("expected same user ID (%d) on second login, got %d", first.ID, second.ID)
	}

	var count int64
	db.Model(&model.User{}).Where("google_id = ?", "google-456").Count(&count)
	if count != 1 {
		t.Errorf("expected exactly 1 user row, got %d", count)
	}
}

// TestGoogleCallback_NewUser is an end-to-end test of the callback handler.
// It replaces the real Google endpoints with local httptest servers and
// verifies that: state is checked, a new user is created, and the redirect
// contains a valid JWT.
func TestGoogleCallback_NewUser(t *testing.T) {
	db := setupTestDB(t)
	h := setupAuthHandler(t, db, "google-789", "callback@example.com")

	// Build a fake callback request that looks like what Google would send.
	// We need a matching state in both the cookie and the query parameter.
	state := "test-state-value"
	req := httptest.NewRequest(http.MethodGet, "/api/auth/google/callback?code=fake-code&state="+state, nil)
	req.AddCookie(&http.Cookie{Name: "oauth_state", Value: state})

	w := httptest.NewRecorder()
	h.GoogleCallback(w, req)

	resp := w.Result()

	// The handler should redirect (307) to the frontend.
	if resp.StatusCode != http.StatusTemporaryRedirect {
		t.Fatalf("expected 307 redirect, got %d", resp.StatusCode)
	}

	location := resp.Header.Get("Location")
	if !strings.Contains(location, "/auth/callback?token=") {
		t.Fatalf("redirect location %q does not contain /auth/callback?token=", location)
	}

	// Extract and validate the JWT from the redirect URL.
	parts := strings.SplitN(location, "token=", 2)
	if len(parts) != 2 {
		t.Fatal("could not extract token from redirect location")
	}
	tokenStr := parts[1]

	var claims jwtClaims
	token, err := jwt.ParseWithClaims(tokenStr, &claims, func(t *jwt.Token) (interface{}, error) {
		return []byte("test-jwt-secret"), nil
	})
	if err != nil {
		t.Fatalf("redirect contained an invalid JWT: %v", err)
	}
	if !token.Valid {
		t.Error("JWT in redirect is not valid")
	}
	if claims.UserID == 0 {
		t.Error("JWT claims have UserID=0, expected a real user ID")
	}

	// Confirm the user was created in the database.
	var count int64
	db.Model(&model.User{}).Where("email = ?", "callback@example.com").Count(&count)
	if count != 1 {
		t.Errorf("expected 1 user in DB, got %d", count)
	}
}

// TestGoogleCallback_StateMismatch verifies that a tampered or missing state
// results in a 400 Bad Request, not a successful login.
func TestGoogleCallback_StateMismatch(t *testing.T) {
	db := setupTestDB(t)
	h := setupAuthHandler(t, db, "google-999", "attacker@example.com")

	req := httptest.NewRequest(http.MethodGet, "/api/auth/google/callback?code=fake-code&state=WRONG", nil)
	// Cookie says "correct-state" but URL says "WRONG" — mismatch.
	req.AddCookie(&http.Cookie{Name: "oauth_state", Value: "correct-state"})

	w := httptest.NewRecorder()
	h.GoogleCallback(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 on state mismatch, got %d", w.Code)
	}

	// No user should have been created.
	var count int64
	db.Model(&model.User{}).Count(&count)
	if count != 0 {
		t.Errorf("expected 0 users in DB after rejected callback, got %d", count)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func jsonResponse(status int, body any) *http.Response {
	payload, _ := json.Marshal(body)

	return &http.Response{
		StatusCode: status,
		Header: http.Header{
			"Content-Type": []string{"application/json"},
		},
		Body:          io.NopCloser(strings.NewReader(string(payload))),
		ContentLength: int64(len(payload)),
	}
}
