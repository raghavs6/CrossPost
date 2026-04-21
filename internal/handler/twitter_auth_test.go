package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/oauth2"

	"github.com/raghavs6/CrossPost/internal/model"
)

// setupTwitterHandler creates a TwitterAuthHandler whose HTTP transport is
// replaced by a roundTripFunc that intercepts:
//   - POST to the test token URL → fake access + refresh tokens
//   - GET  to the test user-info URL → fake Twitter user profile
//
// The auth and token URLs use a ".test" TLD so they can never accidentally
// hit real Twitter endpoints from a test run.
func setupTwitterHandler(t *testing.T, twitterID, username string) *TwitterAuthHandler {
	t.Helper()
	db := setupTestDB(t) // setupTestDB now migrates SocialAccount too

	return &TwitterAuthHandler{
		oauthConf: &oauth2.Config{
			ClientID:     "test-twitter-client-id",
			ClientSecret: "test-twitter-client-secret",
			RedirectURL:  "http://127.0.0.1/callback",
			Endpoint: oauth2.Endpoint{
				AuthURL:  "https://twitter.test/i/oauth2/authorize",
				TokenURL: "https://api.twitter.test/2/oauth2/token",
			},
		},
		db: db,
		httpClient: &http.Client{
			// roundTripFunc is already defined in auth_test.go (same package).
			Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				switch {
				case strings.HasPrefix(r.URL.String(), "https://api.twitter.test/2/oauth2/token"):
					// Fake Twitter token endpoint response.
					return jsonResponse(http.StatusOK, map[string]any{
						"access_token":  "fake-twitter-token",
						"refresh_token": "fake-refresh-token",
						"token_type":    "Bearer",
						"expires_in":    7200,
					}), nil
				case strings.HasPrefix(r.URL.String(), "https://api.twitter.test/2/users/me"):
					// Fake Twitter user-info endpoint response.
					return jsonResponse(http.StatusOK, twitterUserResponse{
						Data: twitterUserInfo{
							ID:       twitterID,
							Name:     "Test User",
							Username: username,
						},
					}), nil
				default:
					return jsonResponse(http.StatusNotFound, map[string]string{"error": "not found"}), nil
				}
			}),
		},
		userInfoURL: "https://api.twitter.test/2/users/me",
		frontendURL: "http://127.0.0.1:5173",
	}
}

// ---------------------------------------------------------------------------
// TwitterLogin
// ---------------------------------------------------------------------------

// TestTwitterLogin_RedirectsToTwitter verifies that a valid JWT-authenticated
// request to GET /api/auth/twitter:
//   - Redirects (307) to the Twitter consent page URL.
//   - Includes a PKCE code_challenge in the redirect URL.
//   - Sets all three required HttpOnly cookies.
func TestTwitterLogin_RedirectsToTwitter(t *testing.T) {
	h := setupTwitterHandler(t, "twitter-123", "testuser")

	// newPostRequest and runWithAuth are defined in post_test.go (same package).
	// They build a request with a valid JWT and run it through RequireAuth.
	req := newPostRequest(t, http.MethodGet, "/api/auth/twitter", nil, 1, "")
	w := runWithAuth(h.TwitterLogin, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", resp.StatusCode, w.Body.String())
	}

	if got := resp.Header.Get("Content-Type"); !strings.Contains(got, "application/json") {
		t.Fatalf("expected JSON content type, got %q", got)
	}

	var payload TwitterAuthorizationURLResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("failed to decode JSON response: %v", err)
	}

	if !strings.Contains(payload.AuthorizationURL, "twitter.test/i/oauth2/authorize") {
		t.Fatalf("expected Twitter auth URL, got %q", payload.AuthorizationURL)
	}
	// S256ChallengeOption should inject code_challenge into the URL.
	if !strings.Contains(payload.AuthorizationURL, "code_challenge") {
		t.Fatalf("expected PKCE code_challenge in auth URL, got %q", payload.AuthorizationURL)
	}

	// All three cookies must be set and non-empty.
	cookieMap := make(map[string]string)
	for _, c := range resp.Cookies() {
		cookieMap[c.Name] = c.Value
	}
	for _, name := range []string{"twitter_state", "twitter_pkce_verifier", "twitter_linking_user_id"} {
		if cookieMap[name] == "" {
			t.Errorf("expected cookie %q to be set and non-empty", name)
		}
	}
	// The linking user ID cookie must match the JWT subject (user 1).
	if cookieMap["twitter_linking_user_id"] != "1" {
		t.Errorf("expected twitter_linking_user_id=1, got %q", cookieMap["twitter_linking_user_id"])
	}
}

// ---------------------------------------------------------------------------
// TwitterCallback
// ---------------------------------------------------------------------------

// TestTwitterCallback_StoresSocialAccount is an end-to-end test of the
// callback handler.  It uses a mock transport (no real Twitter calls) and
// verifies that: the state is checked, tokens are exchanged, the social_accounts
// row is created, and the browser is redirected to /dashboard?twitter=connected.
func TestTwitterCallback_StoresSocialAccount(t *testing.T) {
	h := setupTwitterHandler(t, "twitter-456", "twitteruser")

	state := "test-state-abc"
	req := httptest.NewRequest(http.MethodGet,
		"/api/auth/twitter/callback?code=fake-code&state="+state, nil)
	req.AddCookie(&http.Cookie{Name: "twitter_state", Value: state})
	req.AddCookie(&http.Cookie{Name: "twitter_pkce_verifier", Value: "fake-verifier"})
	req.AddCookie(&http.Cookie{Name: "twitter_linking_user_id", Value: "1"})

	w := httptest.NewRecorder()
	h.TwitterCallback(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("expected 302 redirect, got %d (body: %s)", resp.StatusCode, w.Body.String())
	}
	location := resp.Header.Get("Location")
	if !strings.Contains(location, "/dashboard?twitter=connected") {
		t.Fatalf("expected redirect to /dashboard?twitter=connected, got %q", location)
	}

	// The social_accounts row must have been created.
	var account model.SocialAccount
	if err := h.db.Where("user_id = ? AND platform = ?", 1, "twitter").First(&account).Error; err != nil {
		t.Fatalf("expected social_accounts row to exist, got error: %v", err)
	}
	if account.Username != "twitteruser" {
		t.Errorf("expected username=twitteruser, got %q", account.Username)
	}
	if account.PlatformUserID != "twitter-456" {
		t.Errorf("expected platform_user_id=twitter-456, got %q", account.PlatformUserID)
	}
	if account.AccessToken != "fake-twitter-token" {
		t.Errorf("expected access_token=fake-twitter-token, got %q", account.AccessToken)
	}
}

// TestTwitterCallback_StateMismatch_Returns400 verifies that a tampered or
// missing state results in 400 Bad Request — no DB row is created.
func TestTwitterCallback_StateMismatch_Returns400(t *testing.T) {
	h := setupTwitterHandler(t, "twitter-999", "attacker")

	req := httptest.NewRequest(http.MethodGet,
		"/api/auth/twitter/callback?code=fake-code&state=WRONG", nil)
	// Cookie has "correct-state" but URL param has "WRONG" — deliberate mismatch.
	req.AddCookie(&http.Cookie{Name: "twitter_state", Value: "correct-state"})
	req.AddCookie(&http.Cookie{Name: "twitter_pkce_verifier", Value: "fake-verifier"})
	req.AddCookie(&http.Cookie{Name: "twitter_linking_user_id", Value: "1"})

	w := httptest.NewRecorder()
	h.TwitterCallback(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 on state mismatch, got %d", w.Code)
	}

	// No social_accounts row should have been created.
	var count int64
	h.db.Model(&model.SocialAccount{}).Count(&count)
	if count != 0 {
		t.Errorf("expected 0 social_accounts rows after rejected callback, got %d", count)
	}
}

// ---------------------------------------------------------------------------
// ListConnections
// ---------------------------------------------------------------------------

// TestListConnections_ReturnsConnectedPlatforms verifies that a user sees their
// linked social accounts in the JSON response.
func TestListConnections_ReturnsConnectedPlatforms(t *testing.T) {
	h := setupTwitterHandler(t, "", "")

	// Seed a Twitter connection for user 1.
	h.db.Create(&model.SocialAccount{
		UserID:         1,
		Platform:       "twitter",
		PlatformUserID: "twitter-111",
		Username:       "myhandle",
		AccessToken:    "some-token",
	})

	req := newPostRequest(t, http.MethodGet, "/api/connections", nil, 1, "")
	w := runWithAuth(h.ListConnections, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", w.Code, w.Body.String())
	}

	var connections []ConnectionResponse
	if err := json.NewDecoder(w.Body).Decode(&connections); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(connections) != 1 {
		t.Fatalf("expected 1 connection, got %d", len(connections))
	}
	if connections[0].Platform != "twitter" {
		t.Errorf("expected platform=twitter, got %q", connections[0].Platform)
	}
	if connections[0].Username != "myhandle" {
		t.Errorf("expected username=myhandle, got %q", connections[0].Username)
	}
}

// TestListConnections_ReturnsEmptyForNewUser verifies that a user with no
// linked accounts gets an empty JSON array (not null).
func TestListConnections_ReturnsEmptyForNewUser(t *testing.T) {
	h := setupTwitterHandler(t, "", "")

	req := newPostRequest(t, http.MethodGet, "/api/connections", nil, 99, "")
	w := runWithAuth(h.ListConnections, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var connections []ConnectionResponse
	if err := json.NewDecoder(w.Body).Decode(&connections); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(connections) != 0 {
		t.Errorf("expected empty connections array, got %d items", len(connections))
	}
}
