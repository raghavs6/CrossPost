package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/raghavs6/CrossPost/internal/config"
	"github.com/raghavs6/CrossPost/internal/middleware"
	"github.com/raghavs6/CrossPost/internal/model"
)

const (
	instagramDefaultAuthURL    = "https://www.instagram.com/oauth/authorize"
	instagramDefaultTokenURL   = "https://api.instagram.com/oauth/access_token"
	instagramDefaultProfileURL = "https://graph.instagram.com/me?fields=user_id,username,name"
)

// InstagramAuthHandler owns the Instagram account-linking flow for users who
// are already authenticated into CrossPost.
type InstagramAuthHandler struct {
	clientID     string
	clientSecret string
	redirectURL  string
	db           *gorm.DB
	httpClient   *http.Client
	authURL      string
	tokenURL     string
	profileURL   string
	frontendURL  string
}

// NewInstagramAuthHandler constructs an InstagramAuthHandler from app config.
func NewInstagramAuthHandler(cfg *config.Config, db *gorm.DB) *InstagramAuthHandler {
	return &InstagramAuthHandler{
		clientID:     cfg.InstagramClientID,
		clientSecret: cfg.InstagramClientSecret,
		redirectURL:  cfg.InstagramRedirectURL,
		db:           db,
		httpClient:   &http.Client{Timeout: 10 * time.Second},
		authURL:      instagramDefaultAuthURL,
		tokenURL:     instagramDefaultTokenURL,
		profileURL:   instagramDefaultProfileURL,
		frontendURL:  cfg.FrontendURL,
	}
}

// InstagramLogin returns an Instagram authorization URL for the authenticated user.
func (h *InstagramAuthHandler) InstagramLogin(w http.ResponseWriter, r *http.Request) {
	if !h.configured() {
		http.Error(w, "instagram login is not configured", http.StatusInternalServerError)
		return
	}

	userID := middleware.UserIDFromContext(r.Context())
	state, err := generateState()
	if err != nil {
		http.Error(w, "failed to generate state", http.StatusInternalServerError)
		return
	}

	for _, c := range []*http.Cookie{
		{Name: "instagram_state", Value: state, MaxAge: 300, HttpOnly: true, SameSite: http.SameSiteLaxMode, Path: "/"},
		{Name: "instagram_linking_user_id", Value: strconv.FormatUint(uint64(userID), 10), MaxAge: 300, HttpOnly: true, SameSite: http.SameSiteLaxMode, Path: "/"},
	} {
		http.SetCookie(w, c)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(TwitterAuthorizationURLResponse{
		AuthorizationURL: h.authorizationURL(state),
	})
}

// InstagramCallback exchanges the code for a token, fetches the Instagram
// profile, stores the linked account, and redirects back to the dashboard.
func (h *InstagramAuthHandler) InstagramCallback(w http.ResponseWriter, r *http.Request) {
	stateCookie, err := r.Cookie("instagram_state")
	if err != nil || r.URL.Query().Get("state") != stateCookie.Value {
		http.Error(w, "state mismatch", http.StatusBadRequest)
		return
	}

	userIDCookie, err := r.Cookie("instagram_linking_user_id")
	if err != nil {
		http.Error(w, "missing user id cookie", http.StatusBadRequest)
		return
	}

	for _, name := range []string{"instagram_state", "instagram_linking_user_id"} {
		http.SetCookie(w, &http.Cookie{Name: name, MaxAge: -1, Path: "/"})
	}

	rawUID, err := strconv.ParseUint(userIDCookie.Value, 10, 64)
	if err != nil {
		http.Error(w, "invalid user id cookie", http.StatusBadRequest)
		return
	}
	userID := uint(rawUID)

	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "missing code", http.StatusBadRequest)
		return
	}

	token, expiresAt, err := h.exchangeCode(code)
	if err != nil {
		http.Error(w, "failed to exchange token", http.StatusInternalServerError)
		return
	}

	profile, err := h.fetchInstagramProfile(token)
	if err != nil {
		http.Error(w, "failed to fetch instagram user info", http.StatusInternalServerError)
		return
	}

	displayName := profile.Name
	if displayName == "" {
		displayName = profile.Username
	}

	var account model.SocialAccount
	result := h.db.Where(model.SocialAccount{UserID: userID, Platform: "instagram"}).
		Assign(model.SocialAccount{
			PlatformUserID: profile.UserID,
			DisplayName:    displayName,
			Username:       profile.Username,
			AccessToken:    token,
			TokenExpiry:    expiresAt,
		}).
		FirstOrCreate(&account)
	if result.Error != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, h.frontendURL+"/dashboard?instagram=connected", http.StatusFound)
}

func (h *InstagramAuthHandler) configured() bool {
	return h.clientID != "" && h.clientSecret != "" && h.redirectURL != ""
}

func (h *InstagramAuthHandler) authorizationURL(state string) string {
	u, _ := url.Parse(h.authURL)
	q := u.Query()
	q.Set("client_id", h.clientID)
	q.Set("redirect_uri", h.redirectURL)
	q.Set("response_type", "code")
	q.Set("scope", "instagram_business_basic")
	q.Set("state", state)
	u.RawQuery = q.Encode()
	return u.String()
}

type instagramTokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int64  `json:"expires_in"`
}

func (h *InstagramAuthHandler) exchangeCode(code string) (string, time.Time, error) {
	form := url.Values{}
	form.Set("client_id", h.clientID)
	form.Set("client_secret", h.clientSecret)
	form.Set("grant_type", "authorization_code")
	form.Set("redirect_uri", h.redirectURL)
	form.Set("code", code)

	req, err := http.NewRequest(http.MethodPost, h.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", time.Time{}, fmt.Errorf("failed to build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", time.Time{}, fmt.Errorf("token endpoint returned %d", resp.StatusCode)
	}

	var tokenResp instagramTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", time.Time{}, fmt.Errorf("failed to decode token response: %w", err)
	}
	if tokenResp.AccessToken == "" {
		return "", time.Time{}, fmt.Errorf("instagram returned no access token")
	}

	var expiresAt time.Time
	if tokenResp.ExpiresIn > 0 {
		expiresAt = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	}

	return tokenResp.AccessToken, expiresAt, nil
}

type instagramProfile struct {
	UserID   string `json:"user_id"`
	Username string `json:"username"`
	Name     string `json:"name"`
}

func (h *InstagramAuthHandler) fetchInstagramProfile(accessToken string) (*instagramProfile, error) {
	u, _ := url.Parse(h.profileURL)
	q := u.Query()
	q.Set("access_token", accessToken)
	u.RawQuery = q.Encode()

	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build profile request: %w", err)
	}

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("instagram profile request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("instagram profile returned status %d", resp.StatusCode)
	}

	var profile instagramProfile
	if err := json.NewDecoder(resp.Body).Decode(&profile); err != nil {
		return nil, fmt.Errorf("failed to decode instagram profile: %w", err)
	}
	if profile.UserID == "" || profile.Username == "" {
		return nil, fmt.Errorf("instagram returned incomplete user info")
	}

	return &profile, nil
}
