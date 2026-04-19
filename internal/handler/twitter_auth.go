package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"golang.org/x/oauth2"
	"gorm.io/gorm"

	"github.com/raghavs6/CrossPost/internal/config"
	"github.com/raghavs6/CrossPost/internal/middleware"
	"github.com/raghavs6/CrossPost/internal/model"
)

const (
	twitterAuthURL        = "https://twitter.com/i/oauth2/authorize"
	twitterTokenURL       = "https://api.twitter.com/2/oauth2/token"
	twitterDefaultInfoURL = "https://api.twitter.com/2/users/me?user.fields=id,name,username"
)

// TwitterAuthHandler handles the X (Twitter) OAuth 2.0 account-linking flow.
// It is separate from AuthHandler because Twitter OAuth is optional — the app
// starts without it and enables the routes only when the Twitter env vars are set.
//
// httpClient and userInfoURL are injectable fields so tests can point them at
// a local mock server instead of the real Twitter API.
type TwitterAuthHandler struct {
	oauthConf   *oauth2.Config
	db          *gorm.DB
	httpClient  *http.Client
	userInfoURL string
	frontendURL string
}

// NewTwitterAuthHandler constructs a TwitterAuthHandler from the app config.
// Callers should check cfg.TwitterEnabled() before registering routes — the
// handler is created unconditionally so ListConnections works even without
// Twitter OAuth configured.
func NewTwitterAuthHandler(cfg *config.Config, db *gorm.DB) *TwitterAuthHandler {
	return &TwitterAuthHandler{
		oauthConf: &oauth2.Config{
			ClientID:     cfg.TwitterClientID,
			ClientSecret: cfg.TwitterClientSecret,
			RedirectURL:  cfg.TwitterRedirectURL,
			// These scopes let us read/write tweets and read the user's profile.
			// offline.access is required to receive a refresh token.
			Scopes: []string{"tweet.read", "tweet.write", "users.read", "offline.access"},
			Endpoint: oauth2.Endpoint{
				AuthURL:  twitterAuthURL,
				TokenURL: twitterTokenURL,
			},
		},
		db:          db,
		httpClient:  &http.Client{},
		userInfoURL: twitterDefaultInfoURL,
		frontendURL: cfg.FrontendURL,
	}
}

// TwitterLogin handles GET /api/auth/twitter (protected — RequireAuth must run first).
// It starts the OAuth 2.0 + PKCE flow:
//  1. Reads the caller's user ID from the JWT context.
//  2. Generates a random state (CSRF protection) and a PKCE verifier.
//  3. Stores state, verifier, and user ID in short-lived HttpOnly cookies.
//  4. Redirects the browser to Twitter's consent page.
func (h *TwitterAuthHandler) TwitterLogin(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())

	state, err := generateState()
	if err != nil {
		http.Error(w, "failed to generate state", http.StatusInternalServerError)
		return
	}

	// PKCE (Proof Key for Code Exchange) prevents authorization code interception.
	// The verifier is a random secret; the challenge is its SHA-256 hash.
	// We send the challenge to Twitter now and the verifier when exchanging the code.
	verifier := oauth2.GenerateVerifier()

	// Store all three values in HttpOnly cookies (5-minute TTL).
	// HttpOnly means JavaScript cannot read them — protecting against XSS.
	// SameSite=Lax allows the cookies to be sent when Twitter redirects back to us.
	for _, c := range []*http.Cookie{
		{Name: "twitter_state", Value: state, MaxAge: 300, HttpOnly: true, SameSite: http.SameSiteLaxMode, Path: "/"},
		{Name: "twitter_pkce_verifier", Value: verifier, MaxAge: 300, HttpOnly: true, SameSite: http.SameSiteLaxMode, Path: "/"},
		{Name: "twitter_linking_user_id", Value: strconv.FormatUint(uint64(userID), 10), MaxAge: 300, HttpOnly: true, SameSite: http.SameSiteLaxMode, Path: "/"},
	} {
		http.SetCookie(w, c)
	}

	url := h.oauthConf.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier))
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

// TwitterCallback handles GET /api/auth/twitter/callback (public — no JWT required).
// Twitter redirects here after the user grants (or denies) access.
// Steps: verify state → exchange code + PKCE verifier → fetch profile → upsert DB row → redirect.
func (h *TwitterAuthHandler) TwitterCallback(w http.ResponseWriter, r *http.Request) {
	// --- Step 1: CSRF check — state cookie must match the URL parameter ---
	stateCookie, err := r.Cookie("twitter_state")
	if err != nil || r.URL.Query().Get("state") != stateCookie.Value {
		http.Error(w, "state mismatch", http.StatusBadRequest)
		return
	}

	verifierCookie, err := r.Cookie("twitter_pkce_verifier")
	if err != nil {
		http.Error(w, "missing pkce verifier", http.StatusBadRequest)
		return
	}

	userIDCookie, err := r.Cookie("twitter_linking_user_id")
	if err != nil {
		http.Error(w, "missing user id cookie", http.StatusBadRequest)
		return
	}

	// Clear all three cookies immediately — they are one-time-use values.
	for _, name := range []string{"twitter_state", "twitter_pkce_verifier", "twitter_linking_user_id"} {
		http.SetCookie(w, &http.Cookie{Name: name, MaxAge: -1, Path: "/"})
	}

	// --- Step 2: Parse the linking user ID from the cookie ---
	rawUID, err := strconv.ParseUint(userIDCookie.Value, 10, 64)
	if err != nil {
		http.Error(w, "invalid user id cookie", http.StatusBadRequest)
		return
	}
	userID := uint(rawUID)

	// --- Step 3: Exchange the one-time code + PKCE verifier for access tokens ---
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "missing code", http.StatusBadRequest)
		return
	}

	// Inject h.httpClient via context so tests can intercept the token exchange.
	ctx := context.WithValue(r.Context(), oauth2.HTTPClient, h.httpClient)
	token, err := h.oauthConf.Exchange(ctx, code, oauth2.VerifierOption(verifierCookie.Value))
	if err != nil {
		http.Error(w, "failed to exchange token", http.StatusInternalServerError)
		return
	}

	// --- Step 4: Fetch the Twitter user's profile ---
	twitterUser, err := h.fetchTwitterUserInfo(token.AccessToken)
	if err != nil {
		http.Error(w, "failed to fetch twitter user info", http.StatusInternalServerError)
		return
	}

	// --- Step 5: Upsert the social_accounts row ---
	// Assign + FirstOrCreate is GORM's upsert pattern:
	//   - If a row exists for (user_id, platform), update it with the new tokens.
	//   - If no row exists, create a new one.
	// This handles re-linking (e.g. refreshed tokens after expiry).
	refreshToken := ""
	if rt, ok := token.Extra("refresh_token").(string); ok {
		refreshToken = rt
	}

	var account model.SocialAccount
	result := h.db.Where(model.SocialAccount{UserID: userID, Platform: "twitter"}).
		Assign(model.SocialAccount{
			PlatformUserID: twitterUser.ID,
			Username:       twitterUser.Username,
			AccessToken:    token.AccessToken,
			RefreshToken:   refreshToken,
			TokenExpiry:    token.Expiry,
		}).
		FirstOrCreate(&account)
	if result.Error != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, h.frontendURL+"/dashboard?twitter=connected", http.StatusFound)
}

// ConnectionResponse is the JSON shape ListConnections returns to the frontend.
type ConnectionResponse struct {
	Platform    string    `json:"platform"`
	Username    string    `json:"username"`
	ConnectedAt time.Time `json:"connected_at"`
}

// ListConnections handles GET /api/connections (protected — RequireAuth must run first).
// It returns all social accounts the requesting user has linked.
func (h *TwitterAuthHandler) ListConnections(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())

	var accounts []model.SocialAccount
	if err := h.db.Where("user_id = ?", userID).Find(&accounts).Error; err != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}

	// Build the response slice.  make(…, 0) ensures JSON encodes as [] not null
	// when there are no connections.
	connections := make([]ConnectionResponse, 0, len(accounts))
	for _, a := range accounts {
		connections = append(connections, ConnectionResponse{
			Platform:    a.Platform,
			Username:    a.Username,
			ConnectedAt: a.CreatedAt,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(connections)
}

// twitterUserInfo holds the fields we care about from Twitter API v2's /users/me.
type twitterUserInfo struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Username string `json:"username"`
}

// twitterUserResponse wraps the "data" envelope in Twitter API v2 responses.
type twitterUserResponse struct {
	Data twitterUserInfo `json:"data"`
}

// fetchTwitterUserInfo calls Twitter API v2's /users/me endpoint using the
// Bearer access token.  It uses h.userInfoURL (not a hardcoded constant) and
// h.httpClient so tests can inject a mock server.
func (h *TwitterAuthHandler) fetchTwitterUserInfo(accessToken string) (*twitterUserInfo, error) {
	req, err := http.NewRequest(http.MethodGet, h.userInfoURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("twitter user info request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("twitter user info returned status %d", resp.StatusCode)
	}

	var result twitterUserResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode twitter user info: %w", err)
	}
	if result.Data.ID == "" || result.Data.Username == "" {
		return nil, fmt.Errorf("twitter returned incomplete user info")
	}
	return &result.Data, nil
}
