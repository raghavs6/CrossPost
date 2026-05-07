package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/raghavs6/CrossPost/internal/model"
)

func setupFacebookHandler(t *testing.T, profileID, profileName string) *FacebookAuthHandler {
	t.Helper()
	db := setupTestDB(t)

	return &FacebookAuthHandler{
		appID:       "facebook-app-id",
		appSecret:   "facebook-app-secret",
		redirectURL: "http://127.0.0.1:8080/api/auth/facebook/callback",
		db:          db,
		httpClient: &http.Client{
			Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				switch {
				case strings.HasPrefix(r.URL.String(), "https://graph.facebook.test/oauth/access_token"):
					return jsonResponse(http.StatusOK, map[string]any{
						"access_token": "fake-facebook-token",
						"token_type":   "bearer",
						"expires_in":   3600,
					}), nil
				case strings.HasPrefix(r.URL.String(), "https://graph.facebook.test/me?"):
					return jsonResponse(http.StatusOK, facebookProfile{
						ID:   profileID,
						Name: profileName,
					}), nil
				default:
					return jsonResponse(http.StatusNotFound, map[string]string{"error": "not found"}), nil
				}
			}),
		},
		authURL:     "https://www.facebook.test/dialog/oauth",
		tokenURL:    "https://graph.facebook.test/oauth/access_token",
		profileURL:  "https://graph.facebook.test/me?fields=id,name",
		frontendURL: "http://127.0.0.1:5173",
	}
}

func TestFacebookLogin_ReturnsAuthorizationURL(t *testing.T) {
	h := setupFacebookHandler(t, "fb-123", "Ada Lovelace")

	req := newPostRequest(t, http.MethodGet, "/api/auth/facebook", nil, 1, "")
	w := runWithAuth(h.FacebookLogin, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", resp.StatusCode, w.Body.String())
	}

	var payload TwitterAuthorizationURLResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !strings.Contains(payload.AuthorizationURL, "facebook.test/dialog/oauth") {
		t.Fatalf("expected Facebook auth URL, got %q", payload.AuthorizationURL)
	}
	if !strings.Contains(payload.AuthorizationURL, "pages_manage_posts") {
		t.Fatalf("expected page scopes in auth URL, got %q", payload.AuthorizationURL)
	}

	cookieMap := make(map[string]string)
	for _, c := range resp.Cookies() {
		cookieMap[c.Name] = c.Value
	}
	for _, name := range []string{"facebook_state", "facebook_linking_user_id"} {
		if cookieMap[name] == "" {
			t.Errorf("expected cookie %q to be set and non-empty", name)
		}
	}
	if cookieMap["facebook_linking_user_id"] != "1" {
		t.Errorf("expected facebook_linking_user_id=1, got %q", cookieMap["facebook_linking_user_id"])
	}
}

func TestFacebookCallback_StoresSocialAccount(t *testing.T) {
	h := setupFacebookHandler(t, "fb-456", "Grace Hopper")

	state := "facebook-state"
	req := httptest.NewRequest(http.MethodGet,
		"/api/auth/facebook/callback?code=fake-code&state="+state, nil)
	req.AddCookie(&http.Cookie{Name: "facebook_state", Value: state})
	req.AddCookie(&http.Cookie{Name: "facebook_linking_user_id", Value: "1"})

	w := httptest.NewRecorder()
	h.FacebookCallback(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("expected 302 redirect, got %d (body: %s)", resp.StatusCode, w.Body.String())
	}
	location := resp.Header.Get("Location")
	if !strings.Contains(location, "/dashboard?facebook=connected") {
		t.Fatalf("expected redirect to /dashboard?facebook=connected, got %q", location)
	}

	var account model.SocialAccount
	if err := h.db.Where("user_id = ? AND platform = ?", 1, "facebook").First(&account).Error; err != nil {
		t.Fatalf("expected social_accounts row to exist, got error: %v", err)
	}
	if account.DisplayName != "Grace Hopper" {
		t.Errorf("expected display_name=Grace Hopper, got %q", account.DisplayName)
	}
	if account.Username != "" {
		t.Errorf("expected empty username for facebook account, got %q", account.Username)
	}
	if account.PlatformUserID != "fb-456" {
		t.Errorf("expected platform_user_id=fb-456, got %q", account.PlatformUserID)
	}
	if account.AccessToken != "fake-facebook-token" {
		t.Errorf("expected access_token=fake-facebook-token, got %q", account.AccessToken)
	}
}

func TestFacebookCallback_StateMismatch_Returns400(t *testing.T) {
	h := setupFacebookHandler(t, "fb-999", "Mallory")

	req := httptest.NewRequest(http.MethodGet,
		"/api/auth/facebook/callback?code=fake-code&state=WRONG", nil)
	req.AddCookie(&http.Cookie{Name: "facebook_state", Value: "correct-state"})
	req.AddCookie(&http.Cookie{Name: "facebook_linking_user_id", Value: "1"})

	w := httptest.NewRecorder()
	h.FacebookCallback(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 on state mismatch, got %d", w.Code)
	}
}

func TestFacebookCallback_MissingCode_Returns400(t *testing.T) {
	h := setupFacebookHandler(t, "fb-999", "Mallory")

	state := "facebook-state"
	req := httptest.NewRequest(http.MethodGet,
		"/api/auth/facebook/callback?state="+state, nil)
	req.AddCookie(&http.Cookie{Name: "facebook_state", Value: state})
	req.AddCookie(&http.Cookie{Name: "facebook_linking_user_id", Value: "1"})

	w := httptest.NewRecorder()
	h.FacebookCallback(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 when code is missing, got %d", w.Code)
	}
}

func TestFacebookCallback_RelinkUpdatesExistingRow(t *testing.T) {
	h := setupFacebookHandler(t, "fb-777", "Updated Name")

	if err := h.db.Create(&model.SocialAccount{
		UserID:         1,
		Platform:       "facebook",
		PlatformUserID: "fb-old",
		DisplayName:    "Old Name",
		AccessToken:    "old-token",
	}).Error; err != nil {
		t.Fatalf("seed existing facebook account: %v", err)
	}

	state := "facebook-state"
	req := httptest.NewRequest(http.MethodGet,
		"/api/auth/facebook/callback?code=fake-code&state="+state, nil)
	req.AddCookie(&http.Cookie{Name: "facebook_state", Value: state})
	req.AddCookie(&http.Cookie{Name: "facebook_linking_user_id", Value: "1"})

	w := httptest.NewRecorder()
	h.FacebookCallback(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("expected 302 redirect, got %d", w.Code)
	}

	var accounts []model.SocialAccount
	if err := h.db.Where("user_id = ? AND platform = ?", 1, "facebook").Find(&accounts).Error; err != nil {
		t.Fatalf("load facebook accounts: %v", err)
	}
	if len(accounts) != 1 {
		t.Fatalf("expected exactly one facebook row, got %d", len(accounts))
	}
	if accounts[0].DisplayName != "Updated Name" {
		t.Errorf("expected updated display name, got %q", accounts[0].DisplayName)
	}
	if accounts[0].AccessToken != "fake-facebook-token" {
		t.Errorf("expected updated access token, got %q", accounts[0].AccessToken)
	}
}
