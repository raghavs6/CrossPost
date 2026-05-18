package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"gorm.io/gorm"

	"github.com/raghavs6/CrossPost/internal/config"
	"github.com/raghavs6/CrossPost/internal/middleware"
	"github.com/raghavs6/CrossPost/internal/model"
)

const (
	facebookGraphVersion      = "v24.0"
	facebookDefaultAuthURL    = "https://www.facebook.com/" + facebookGraphVersion + "/dialog/oauth"
	facebookDefaultTokenURL   = "https://graph.facebook.com/" + facebookGraphVersion + "/oauth/access_token"
	facebookDefaultProfileURL = "https://graph.facebook.com/" + facebookGraphVersion + "/me?fields=id,name"
	facebookDefaultPagesURL   = "https://graph.facebook.com/" + facebookGraphVersion + "/me/accounts?fields=id,name,access_token"
	facebookPendingCookieName = "facebook_pending_flow_id"
	facebookPendingLinkMaxAge = 10 * time.Minute
)

// FacebookAuthHandler owns the Facebook Login account-linking flow.
// Unlike Google login, this does not authenticate into CrossPost itself —
// it links a Facebook Page to an already-authenticated CrossPost user.
type FacebookAuthHandler struct {
	appID       string
	appSecret   string
	redirectURL string
	db          *gorm.DB
	httpClient  *http.Client
	authURL     string
	tokenURL    string
	profileURL  string
	pagesURL    string
	frontendURL string
}

// NewFacebookAuthHandler constructs a FacebookAuthHandler from app config.
func NewFacebookAuthHandler(cfg *config.Config, db *gorm.DB) *FacebookAuthHandler {
	return &FacebookAuthHandler{
		appID:       cfg.FacebookAppID,
		appSecret:   cfg.FacebookAppSecret,
		redirectURL: cfg.FacebookRedirectURL,
		db:          db,
		httpClient:  &http.Client{Timeout: 10 * time.Second},
		authURL:     facebookDefaultAuthURL,
		tokenURL:    facebookDefaultTokenURL,
		profileURL:  facebookDefaultProfileURL,
		pagesURL:    facebookDefaultPagesURL,
		frontendURL: cfg.FrontendURL,
	}
}

// FacebookLogin returns a Facebook authorization URL for the authenticated user.
func (h *FacebookAuthHandler) FacebookLogin(w http.ResponseWriter, r *http.Request) {
	if !h.configured() {
		http.Error(w, "facebook login is not configured", http.StatusInternalServerError)
		return
	}

	userID := middleware.UserIDFromContext(r.Context())
	state, err := generateState()
	if err != nil {
		http.Error(w, "failed to generate state", http.StatusInternalServerError)
		return
	}

	for _, c := range []*http.Cookie{
		{Name: "facebook_state", Value: state, MaxAge: 300, HttpOnly: true, SameSite: http.SameSiteLaxMode, Path: "/"},
		{Name: "facebook_linking_user_id", Value: strconv.FormatUint(uint64(userID), 10), MaxAge: 300, HttpOnly: true, SameSite: http.SameSiteLaxMode, Path: "/"},
	} {
		http.SetCookie(w, c)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(TwitterAuthorizationURLResponse{
		AuthorizationURL: h.authorizationURL(state),
	})
}

// FacebookCallback exchanges the OAuth code for a user token, fetches the
// user's manageable Pages, stores those Page choices server-side, and then
// redirects back to the dashboard so the user can choose one Page.
func (h *FacebookAuthHandler) FacebookCallback(w http.ResponseWriter, r *http.Request) {
	stateCookie, err := r.Cookie("facebook_state")
	if err != nil || r.URL.Query().Get("state") != stateCookie.Value {
		redirectOAuthCallbackError(w, r, h.frontendURL, "facebook", "state_mismatch")
		return
	}

	userIDCookie, err := r.Cookie("facebook_linking_user_id")
	if err != nil {
		redirectOAuthCallbackError(w, r, h.frontendURL, "facebook", "missing_user_cookie")
		return
	}

	for _, name := range []string{"facebook_state", "facebook_linking_user_id"} {
		http.SetCookie(w, &http.Cookie{Name: name, MaxAge: -1, Path: "/"})
	}

	rawUID, err := strconv.ParseUint(userIDCookie.Value, 10, 64)
	if err != nil {
		redirectOAuthCallbackError(w, r, h.frontendURL, "facebook", "invalid_user_cookie")
		return
	}
	userID := uint(rawUID)

	code := r.URL.Query().Get("code")
	if code == "" {
		redirectOAuthCallbackError(w, r, h.frontendURL, "facebook", "missing_code")
		return
	}

	userToken, err := h.exchangeCode(code)
	if err != nil {
		redirectOAuthCallbackError(w, r, h.frontendURL, "facebook", "token_exchange_failed")
		return
	}

	profile, err := h.fetchFacebookProfile(userToken)
	if err != nil {
		redirectOAuthCallbackError(w, r, h.frontendURL, "facebook", "profile_fetch_failed")
		return
	}

	pages, err := h.fetchFacebookPages(userToken)
	if err != nil {
		redirectOAuthCallbackError(w, r, h.frontendURL, "facebook", "pages_fetch_failed")
		return
	}
	if len(pages) == 0 {
		redirectOAuthCallbackError(w, r, h.frontendURL, "facebook", "no_pages_found")
		return
	}

	flowID, err := generateState()
	if err != nil {
		redirectOAuthCallbackError(w, r, h.frontendURL, "facebook", "page_selection_state_failed")
		return
	}

	expiresAt := time.Now().Add(facebookPendingLinkMaxAge)
	h.cleanupExpiredPendingLinks()
	if err := h.db.Where("user_id = ?", userID).Delete(&model.PendingFacebookPageLink{}).Error; err != nil {
		redirectOAuthCallbackError(w, r, h.frontendURL, "facebook", "pending_cleanup_failed")
		return
	}

	pendingRows := make([]model.PendingFacebookPageLink, 0, len(pages))
	for _, page := range pages {
		pendingRows = append(pendingRows, model.PendingFacebookPageLink{
			FlowID:           flowID,
			UserID:           userID,
			FacebookUserID:   profile.ID,
			FacebookUserName: profile.Name,
			PageID:           page.ID,
			PageName:         page.Name,
			PageAccessToken:  page.AccessToken,
			ExpiresAt:        expiresAt,
		})
	}
	if err := h.db.Create(&pendingRows).Error; err != nil {
		redirectOAuthCallbackError(w, r, h.frontendURL, "facebook", "pending_pages_store_failed")
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     facebookPendingCookieName,
		Value:    flowID,
		MaxAge:   int(facebookPendingLinkMaxAge.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Path:     "/",
	})

	http.Redirect(w, r, h.frontendURL+"/dashboard?facebook=select", http.StatusFound)
}

// ListPendingFacebookPages returns the short-lived list of Facebook Pages the
// user can choose from after the OAuth callback succeeds.
func (h *FacebookAuthHandler) ListPendingFacebookPages(w http.ResponseWriter, r *http.Request) {
	links, err := h.loadPendingLinks(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	resp := make([]FacebookPageOptionResponse, 0, len(links))
	for _, link := range links {
		resp = append(resp, FacebookPageOptionResponse{
			ID:   link.PageID,
			Name: link.PageName,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// SelectFacebookPage finalises the account link by moving one pending Page
// choice into social_accounts and clearing the short-lived selection state.
func (h *FacebookAuthHandler) SelectFacebookPage(w http.ResponseWriter, r *http.Request) {
	var req SelectFacebookPageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.PageID == "" {
		http.Error(w, "page_id is required", http.StatusBadRequest)
		return
	}

	links, err := h.loadPendingLinks(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	var selected *model.PendingFacebookPageLink
	for i := range links {
		if links[i].PageID == req.PageID {
			selected = &links[i]
			break
		}
	}
	if selected == nil {
		http.Error(w, "facebook page not found in pending choices", http.StatusNotFound)
		return
	}

	var account model.SocialAccount
	result := h.db.Where(model.SocialAccount{UserID: selected.UserID, Platform: "facebook"}).
		Assign(model.SocialAccount{
			PlatformUserID:    selected.FacebookUserID,
			PlatformAccountID: selected.PageID,
			DisplayName:       selected.PageName,
			Username:          "",
			AccessToken:       selected.PageAccessToken,
			TokenExpiry:       time.Time{},
		}).
		FirstOrCreate(&account)
	if result.Error != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}

	if err := h.db.Where("flow_id = ? AND user_id = ?", selected.FlowID, selected.UserID).
		Delete(&model.PendingFacebookPageLink{}).Error; err != nil {
		http.Error(w, "failed to clear pending facebook page choices", http.StatusInternalServerError)
		return
	}

	h.clearPendingFacebookCookie(w)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ConnectionResponse{
		Platform:    "facebook",
		DisplayName: selected.PageName,
		ConnectedAt: account.CreatedAt,
	})
}

// ConnectionResponse is the JSON shape ListConnections returns to the frontend.
type ConnectionResponse struct {
	Platform    string    `json:"platform"`
	DisplayName string    `json:"display_name"`
	Username    string    `json:"username,omitempty"`
	ConnectedAt time.Time `json:"connected_at"`
}

// FacebookPageOptionResponse is the page picker data returned to the dashboard.
type FacebookPageOptionResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// SelectFacebookPageRequest is the protected request body used to confirm one
// of the pending Facebook Pages after OAuth succeeds.
type SelectFacebookPageRequest struct {
	PageID string `json:"page_id"`
}

func (h *FacebookAuthHandler) configured() bool {
	return h.appID != "" && h.appSecret != "" && h.redirectURL != ""
}

func (h *FacebookAuthHandler) authorizationURL(state string) string {
	u, _ := url.Parse(h.authURL)
	q := u.Query()
	q.Set("client_id", h.appID)
	q.Set("redirect_uri", h.redirectURL)
	q.Set("state", state)
	q.Set("response_type", "code")
	q.Set("scope", "public_profile,pages_show_list,pages_read_engagement,pages_manage_posts,business_management")
	u.RawQuery = q.Encode()
	return u.String()
}

type facebookTokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
}

func (h *FacebookAuthHandler) exchangeCode(code string) (string, error) {
	u, _ := url.Parse(h.tokenURL)
	q := u.Query()
	q.Set("client_id", h.appID)
	q.Set("client_secret", h.appSecret)
	q.Set("redirect_uri", h.redirectURL)
	q.Set("code", code)
	u.RawQuery = q.Encode()

	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return "", fmt.Errorf("failed to build token request: %w", err)
	}

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token endpoint returned %d", resp.StatusCode)
	}

	var tokenResp facebookTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", fmt.Errorf("failed to decode token response: %w", err)
	}
	if tokenResp.AccessToken == "" {
		return "", fmt.Errorf("facebook returned no access token")
	}

	return tokenResp.AccessToken, nil
}

type facebookProfile struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (h *FacebookAuthHandler) fetchFacebookProfile(accessToken string) (*facebookProfile, error) {
	req, err := http.NewRequest(http.MethodGet, h.profileURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build profile request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("facebook profile request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("facebook profile returned status %d", resp.StatusCode)
	}

	var profile facebookProfile
	if err := json.NewDecoder(resp.Body).Decode(&profile); err != nil {
		return nil, fmt.Errorf("failed to decode facebook profile: %w", err)
	}
	if profile.ID == "" || profile.Name == "" {
		return nil, fmt.Errorf("facebook returned incomplete user info")
	}

	return &profile, nil
}

type facebookPage struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	AccessToken string `json:"access_token"`
}

type facebookPagesResponse struct {
	Data []facebookPage `json:"data"`
}

func (h *FacebookAuthHandler) fetchFacebookPages(accessToken string) ([]facebookPage, error) {
	req, err := http.NewRequest(http.MethodGet, h.pagesURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build pages request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("facebook pages request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("facebook pages returned status %d", resp.StatusCode)
	}

	var result facebookPagesResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode facebook pages: %w", err)
	}

	pages := make([]facebookPage, 0, len(result.Data))
	for _, page := range result.Data {
		if page.ID == "" || page.Name == "" || page.AccessToken == "" {
			continue
		}
		pages = append(pages, page)
	}

	return pages, nil
}

func (h *FacebookAuthHandler) loadPendingLinks(r *http.Request) ([]model.PendingFacebookPageLink, error) {
	flowCookie, err := r.Cookie(facebookPendingCookieName)
	if err != nil || flowCookie.Value == "" {
		return nil, fmt.Errorf("no pending facebook page selection found")
	}

	userID := middleware.UserIDFromContext(r.Context())
	h.cleanupExpiredPendingLinks()

	var links []model.PendingFacebookPageLink
	if err := h.db.Where("flow_id = ? AND user_id = ? AND expires_at > ?", flowCookie.Value, userID, time.Now()).
		Order("page_name asc").
		Find(&links).Error; err != nil {
		return nil, fmt.Errorf("failed to load pending facebook page choices")
	}
	if len(links) == 0 {
		return nil, fmt.Errorf("no pending facebook page selection found")
	}

	return links, nil
}

func (h *FacebookAuthHandler) cleanupExpiredPendingLinks() {
	h.db.Where("expires_at <= ?", time.Now()).Delete(&model.PendingFacebookPageLink{})
}

func (h *FacebookAuthHandler) clearPendingFacebookCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:   facebookPendingCookieName,
		Value:  "",
		MaxAge: -1,
		Path:   "/",
	})
}
