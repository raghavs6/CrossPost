package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/raghavs6/CrossPost/internal/model"
)

func setupFacebookHandler(t *testing.T) *FacebookAuthHandler {
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
						"access_token": "fake-facebook-user-token",
						"token_type":   "bearer",
					}), nil
				case strings.HasPrefix(r.URL.String(), "https://graph.facebook.test/me?fields=id,name"):
					return jsonResponse(http.StatusOK, facebookProfile{
						ID:   "fb-user-123",
						Name: "Ada Lovelace",
					}), nil
				case strings.HasPrefix(r.URL.String(), "https://graph.facebook.test/me/accounts?fields=id,name,access_token"):
					return jsonResponse(http.StatusOK, facebookPagesResponse{
						Data: []facebookPage{
							{ID: "page-1", Name: "CrossPost Bakery", AccessToken: "page-token-1"},
							{ID: "page-2", Name: "CrossPost Cafe", AccessToken: "page-token-2"},
						},
					}), nil
				default:
					return jsonResponse(http.StatusNotFound, map[string]string{"error": "not found"}), nil
				}
			}),
		},
		authURL:     "https://www.facebook.test/dialog/oauth",
		tokenURL:    "https://graph.facebook.test/oauth/access_token",
		profileURL:  "https://graph.facebook.test/me?fields=id,name",
		pagesURL:    "https://graph.facebook.test/me/accounts?fields=id,name,access_token",
		frontendURL: "http://127.0.0.1:5173",
	}
}

func TestFacebookLogin_ReturnsAuthorizationURL(t *testing.T) {
	h := setupFacebookHandler(t)

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

func TestFacebookCallback_StoresPendingPages(t *testing.T) {
	h := setupFacebookHandler(t)

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
	if !strings.Contains(location, "/dashboard?facebook=select") {
		t.Fatalf("expected redirect to /dashboard?facebook=select, got %q", location)
	}

	var links []model.PendingFacebookPageLink
	if err := h.db.Where("user_id = ?", 1).Order("page_id asc").Find(&links).Error; err != nil {
		t.Fatalf("expected pending rows to exist, got error: %v", err)
	}
	if len(links) != 2 {
		t.Fatalf("expected 2 pending rows, got %d", len(links))
	}
	if links[0].FacebookUserID != "fb-user-123" {
		t.Errorf("expected facebook user id to be stored, got %q", links[0].FacebookUserID)
	}
	if links[0].PageAccessToken == "" {
		t.Error("expected page access token to be stored")
	}

	cookies := resp.Cookies()
	foundPendingCookie := false
	for _, c := range cookies {
		if c.Name == facebookPendingCookieName && c.Value != "" {
			foundPendingCookie = true
		}
	}
	if !foundPendingCookie {
		t.Fatal("expected pending facebook flow cookie to be set")
	}
}

func TestFacebookCallback_StateMismatch_Returns400(t *testing.T) {
	h := setupFacebookHandler(t)

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
	h := setupFacebookHandler(t)

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

func TestListPendingFacebookPages_ReturnsPageChoices(t *testing.T) {
	h := setupFacebookHandler(t)
	flowID := "pending-flow"

	for _, page := range []model.PendingFacebookPageLink{
		{
			FlowID:           flowID,
			UserID:           1,
			FacebookUserID:   "fb-user-123",
			FacebookUserName: "Ada Lovelace",
			PageID:           "page-2",
			PageName:         "CrossPost Cafe",
			PageAccessToken:  "page-token-2",
			ExpiresAt:        time.Now().Add(5 * time.Minute),
		},
		{
			FlowID:           flowID,
			UserID:           1,
			FacebookUserID:   "fb-user-123",
			FacebookUserName: "Ada Lovelace",
			PageID:           "page-1",
			PageName:         "CrossPost Bakery",
			PageAccessToken:  "page-token-1",
			ExpiresAt:        time.Now().Add(5 * time.Minute),
		},
	} {
		if err := h.db.Create(&page).Error; err != nil {
			t.Fatalf("seed pending page: %v", err)
		}
	}

	req := newPostRequest(t, http.MethodGet, "/api/auth/facebook/pages", nil, 1, "")
	req.AddCookie(&http.Cookie{Name: facebookPendingCookieName, Value: flowID})
	w := runWithAuth(h.ListPendingFacebookPages, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var pages []FacebookPageOptionResponse
	if err := json.NewDecoder(w.Body).Decode(&pages); err != nil {
		t.Fatalf("decode page options: %v", err)
	}
	if len(pages) != 2 {
		t.Fatalf("expected 2 page options, got %d", len(pages))
	}
	if pages[0].Name != "CrossPost Bakery" {
		t.Errorf("expected alphabetical ordering, got first page %q", pages[0].Name)
	}
}

func TestSelectFacebookPage_StoresSocialAccountAndClearsPendingRows(t *testing.T) {
	h := setupFacebookHandler(t)
	flowID := "pending-flow"

	if err := h.db.Create(&model.PendingFacebookPageLink{
		FlowID:           flowID,
		UserID:           1,
		FacebookUserID:   "fb-user-123",
		FacebookUserName: "Ada Lovelace",
		PageID:           "page-1",
		PageName:         "CrossPost Bakery",
		PageAccessToken:  "page-token-1",
		ExpiresAt:        time.Now().Add(5 * time.Minute),
	}).Error; err != nil {
		t.Fatalf("seed pending page: %v", err)
	}

	req := newPostRequest(t, http.MethodPost, "/api/auth/facebook/select-page", SelectFacebookPageRequest{
		PageID: "page-1",
	}, 1, "")
	req.AddCookie(&http.Cookie{Name: facebookPendingCookieName, Value: flowID})
	w := runWithAuth(h.SelectFacebookPage, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", w.Code, w.Body.String())
	}

	var account model.SocialAccount
	if err := h.db.Where("user_id = ? AND platform = ?", 1, "facebook").First(&account).Error; err != nil {
		t.Fatalf("expected facebook social account row, got error: %v", err)
	}
	if account.PlatformUserID != "fb-user-123" {
		t.Errorf("expected platform user id fb-user-123, got %q", account.PlatformUserID)
	}
	if account.PlatformAccountID != "page-1" {
		t.Errorf("expected page id page-1, got %q", account.PlatformAccountID)
	}
	if account.DisplayName != "CrossPost Bakery" {
		t.Errorf("expected page display name CrossPost Bakery, got %q", account.DisplayName)
	}
	if account.AccessToken != "page-token-1" {
		t.Errorf("expected page access token page-token-1, got %q", account.AccessToken)
	}

	var pendingCount int64
	h.db.Model(&model.PendingFacebookPageLink{}).Where("flow_id = ?", flowID).Count(&pendingCount)
	if pendingCount != 0 {
		t.Fatalf("expected pending page rows to be cleared, got %d", pendingCount)
	}
}

func TestSelectFacebookPage_RelinkUpdatesExistingRow(t *testing.T) {
	h := setupFacebookHandler(t)
	flowID := "pending-flow"

	if err := h.db.Create(&model.SocialAccount{
		UserID:            1,
		Platform:          "facebook",
		PlatformUserID:    "fb-old-user",
		PlatformAccountID: "old-page",
		DisplayName:       "Old Name",
		AccessToken:       "old-token",
	}).Error; err != nil {
		t.Fatalf("seed existing facebook account: %v", err)
	}
	if err := h.db.Create(&model.PendingFacebookPageLink{
		FlowID:           flowID,
		UserID:           1,
		FacebookUserID:   "fb-user-123",
		FacebookUserName: "Ada Lovelace",
		PageID:           "page-2",
		PageName:         "CrossPost Cafe",
		PageAccessToken:  "page-token-2",
		ExpiresAt:        time.Now().Add(5 * time.Minute),
	}).Error; err != nil {
		t.Fatalf("seed pending page: %v", err)
	}

	req := newPostRequest(t, http.MethodPost, "/api/auth/facebook/select-page", SelectFacebookPageRequest{
		PageID: "page-2",
	}, 1, "")
	req.AddCookie(&http.Cookie{Name: facebookPendingCookieName, Value: flowID})
	w := runWithAuth(h.SelectFacebookPage, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var accounts []model.SocialAccount
	if err := h.db.Where("user_id = ? AND platform = ?", 1, "facebook").Find(&accounts).Error; err != nil {
		t.Fatalf("load facebook accounts: %v", err)
	}
	if len(accounts) != 1 {
		t.Fatalf("expected exactly one facebook row, got %d", len(accounts))
	}
	if accounts[0].PlatformAccountID != "page-2" {
		t.Errorf("expected updated page id page-2, got %q", accounts[0].PlatformAccountID)
	}
	if accounts[0].DisplayName != "CrossPost Cafe" {
		t.Errorf("expected updated display name, got %q", accounts[0].DisplayName)
	}
	if accounts[0].AccessToken != "page-token-2" {
		t.Errorf("expected updated access token, got %q", accounts[0].AccessToken)
	}
}
