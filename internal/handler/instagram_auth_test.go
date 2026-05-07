package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/raghavs6/CrossPost/internal/model"
)

func setupInstagramHandler(t *testing.T, profileUserID, username, name string) *InstagramAuthHandler {
	t.Helper()
	db := setupTestDB(t)

	return &InstagramAuthHandler{
		clientID:     "instagram-client-id",
		clientSecret: "instagram-client-secret",
		redirectURL:  "http://127.0.0.1:8080/api/auth/instagram/callback",
		db:           db,
		httpClient: &http.Client{
			Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				switch {
				case r.URL.String() == "https://api.instagram.test/oauth/access_token":
					return jsonResponse(http.StatusOK, map[string]any{
						"access_token": "fake-instagram-token",
						"expires_in":   3600,
					}), nil
				case strings.HasPrefix(r.URL.String(), "https://graph.instagram.test/me?"):
					return jsonResponse(http.StatusOK, instagramProfile{
						UserID:   profileUserID,
						Username: username,
						Name:     name,
					}), nil
				default:
					return jsonResponse(http.StatusNotFound, map[string]string{"error": "not found"}), nil
				}
			}),
		},
		authURL:     "https://www.instagram.test/oauth/authorize",
		tokenURL:    "https://api.instagram.test/oauth/access_token",
		profileURL:  "https://graph.instagram.test/me?fields=user_id,username,name",
		frontendURL: "http://127.0.0.1:5173",
	}
}

func TestInstagramLogin_ReturnsAuthorizationURL(t *testing.T) {
	h := setupInstagramHandler(t, "ig-123", "crosspost", "CrossPost Creator")

	req := newPostRequest(t, http.MethodGet, "/api/auth/instagram", nil, 1, "")
	w := runWithAuth(h.InstagramLogin, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", resp.StatusCode, w.Body.String())
	}

	var payload TwitterAuthorizationURLResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !strings.Contains(payload.AuthorizationURL, "instagram.test/oauth/authorize") {
		t.Fatalf("expected Instagram auth URL, got %q", payload.AuthorizationURL)
	}
	if !strings.Contains(payload.AuthorizationURL, "instagram_business_basic") {
		t.Fatalf("expected instagram_business_basic scope, got %q", payload.AuthorizationURL)
	}

	cookieMap := make(map[string]string)
	for _, c := range resp.Cookies() {
		cookieMap[c.Name] = c.Value
	}
	for _, name := range []string{"instagram_state", "instagram_linking_user_id"} {
		if cookieMap[name] == "" {
			t.Errorf("expected cookie %q to be set and non-empty", name)
		}
	}
	if cookieMap["instagram_linking_user_id"] != "1" {
		t.Errorf("expected instagram_linking_user_id=1, got %q", cookieMap["instagram_linking_user_id"])
	}
}

func TestInstagramCallback_StoresSocialAccount(t *testing.T) {
	h := setupInstagramHandler(t, "ig-456", "creatorhandle", "Creator Profile")

	state := "instagram-state"
	req := httptest.NewRequest(http.MethodGet,
		"/api/auth/instagram/callback?code=fake-code&state="+state, nil)
	req.AddCookie(&http.Cookie{Name: "instagram_state", Value: state})
	req.AddCookie(&http.Cookie{Name: "instagram_linking_user_id", Value: "1"})

	w := httptest.NewRecorder()
	h.InstagramCallback(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("expected 302 redirect, got %d (body: %s)", resp.StatusCode, w.Body.String())
	}
	location := resp.Header.Get("Location")
	if !strings.Contains(location, "/dashboard?instagram=connected") {
		t.Fatalf("expected redirect to /dashboard?instagram=connected, got %q", location)
	}

	var account model.SocialAccount
	if err := h.db.Where("user_id = ? AND platform = ?", 1, "instagram").First(&account).Error; err != nil {
		t.Fatalf("expected social_accounts row to exist, got error: %v", err)
	}
	if account.DisplayName != "Creator Profile" {
		t.Errorf("expected display_name=Creator Profile, got %q", account.DisplayName)
	}
	if account.Username != "creatorhandle" {
		t.Errorf("expected username=creatorhandle, got %q", account.Username)
	}
	if account.PlatformUserID != "ig-456" {
		t.Errorf("expected platform_user_id=ig-456, got %q", account.PlatformUserID)
	}
	if account.AccessToken != "fake-instagram-token" {
		t.Errorf("expected access_token=fake-instagram-token, got %q", account.AccessToken)
	}
}

func TestInstagramCallback_StateMismatch_Returns400(t *testing.T) {
	h := setupInstagramHandler(t, "ig-999", "mallory", "Mallory")

	req := httptest.NewRequest(http.MethodGet,
		"/api/auth/instagram/callback?code=fake-code&state=WRONG", nil)
	req.AddCookie(&http.Cookie{Name: "instagram_state", Value: "correct-state"})
	req.AddCookie(&http.Cookie{Name: "instagram_linking_user_id", Value: "1"})

	w := httptest.NewRecorder()
	h.InstagramCallback(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 on state mismatch, got %d", w.Code)
	}
}

func TestInstagramCallback_MissingCode_Returns400(t *testing.T) {
	h := setupInstagramHandler(t, "ig-999", "mallory", "Mallory")

	state := "instagram-state"
	req := httptest.NewRequest(http.MethodGet,
		"/api/auth/instagram/callback?state="+state, nil)
	req.AddCookie(&http.Cookie{Name: "instagram_state", Value: state})
	req.AddCookie(&http.Cookie{Name: "instagram_linking_user_id", Value: "1"})

	w := httptest.NewRecorder()
	h.InstagramCallback(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 when code is missing, got %d", w.Code)
	}
}

func TestInstagramCallback_RelinkUpdatesExistingRow(t *testing.T) {
	h := setupInstagramHandler(t, "ig-777", "updatedhandle", "Updated Name")

	if err := h.db.Create(&model.SocialAccount{
		UserID:         1,
		Platform:       "instagram",
		PlatformUserID: "ig-old",
		DisplayName:    "Old Name",
		Username:       "oldhandle",
		AccessToken:    "old-token",
	}).Error; err != nil {
		t.Fatalf("seed existing instagram account: %v", err)
	}

	state := "instagram-state"
	req := httptest.NewRequest(http.MethodGet,
		"/api/auth/instagram/callback?code=fake-code&state="+state, nil)
	req.AddCookie(&http.Cookie{Name: "instagram_state", Value: state})
	req.AddCookie(&http.Cookie{Name: "instagram_linking_user_id", Value: "1"})

	w := httptest.NewRecorder()
	h.InstagramCallback(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("expected 302 redirect, got %d", w.Code)
	}

	var accounts []model.SocialAccount
	if err := h.db.Where("user_id = ? AND platform = ?", 1, "instagram").Find(&accounts).Error; err != nil {
		t.Fatalf("load instagram accounts: %v", err)
	}
	if len(accounts) != 1 {
		t.Fatalf("expected exactly one instagram row, got %d", len(accounts))
	}
	if accounts[0].DisplayName != "Updated Name" {
		t.Errorf("expected updated display name, got %q", accounts[0].DisplayName)
	}
	if accounts[0].Username != "updatedhandle" {
		t.Errorf("expected updated username, got %q", accounts[0].Username)
	}
	if accounts[0].AccessToken != "fake-instagram-token" {
		t.Errorf("expected updated access token, got %q", accounts[0].AccessToken)
	}
}
