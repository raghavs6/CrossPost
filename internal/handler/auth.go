package handler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"gorm.io/gorm"

	"github.com/raghavs6/CrossPost/internal/config"
	"github.com/raghavs6/CrossPost/internal/model"
)

// AuthHandler holds everything the OAuth routes need.
// httpClient and userInfoURL are fields (rather than hardcoded) so that
// tests can replace them with a local httptest.Server — keeping tests fast
// and offline per the project's testing policy.
type AuthHandler struct {
	oauthConf   *oauth2.Config
	db          *gorm.DB
	jwtSecret   []byte
	userInfoURL string
	httpClient  *http.Client
	frontendURL string
}

// NewAuthHandler constructs an AuthHandler from the application config.
// Call this once in main.go and register the returned handler's methods as
// chi routes.
func NewAuthHandler(cfg *config.Config, db *gorm.DB) *AuthHandler {
	return &AuthHandler{
		oauthConf: &oauth2.Config{
			ClientID:     cfg.GoogleClientID,
			ClientSecret: cfg.GoogleClientSecret,
			RedirectURL:  cfg.GoogleRedirectURL,
			// These two scopes ask Google for the user's email address and
			// basic profile (name, picture).  Requesting only what you need
			// is an OAuth best practice.
			Scopes:   []string{"email", "profile"},
			Endpoint: google.Endpoint,
		},
		db:          db,
		jwtSecret:   []byte(cfg.JWTSecret),
		userInfoURL: "https://www.googleapis.com/oauth2/v2/userinfo",
		httpClient:  &http.Client{},
		frontendURL: cfg.FrontendURL,
	}
}

// GoogleLogin handles GET /api/auth/google.
// It generates a random state string, stores it in a short-lived cookie,
// then redirects the browser to Google's OAuth consent page.
func (h *AuthHandler) GoogleLogin(w http.ResponseWriter, r *http.Request) {
	if !h.oauthConfigured() {
		http.Error(w, "google oauth is not configured", http.StatusInternalServerError)
		return
	}

	state, err := generateState()
	if err != nil {
		http.Error(w, "failed to generate state", http.StatusInternalServerError)
		return
	}

	// Store the state in a cookie so we can verify it in the callback.
	// HttpOnly means JavaScript cannot read this cookie — a security measure.
	// SameSite=Lax prevents it from being sent by cross-site requests.
	// MaxAge=300 means it expires in 5 minutes (plenty of time to log in).
	http.SetCookie(w, &http.Cookie{
		Name:     "oauth_state",
		Value:    state,
		MaxAge:   300,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Path:     "/",
	})

	// AuthCodeURL builds the full Google redirect URL, including our
	// client ID, requested scopes, redirect URI, and the state parameter.
	url := h.oauthConf.AuthCodeURL(state)
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

// GoogleCallback handles GET /api/auth/google/callback.
// Google redirects here after the user consents (or denies).
// It verifies the state, exchanges the code for a token, fetches the user's
// Google profile, upserts the user in our database, issues a JWT, and
// redirects to the frontend.
func (h *AuthHandler) GoogleCallback(w http.ResponseWriter, r *http.Request) {
	// --- Step 1: Verify state to prevent CSRF ---
	cookie, err := r.Cookie("oauth_state")
	if err != nil {
		http.Error(w, "missing state cookie", http.StatusBadRequest)
		return
	}
	if r.URL.Query().Get("state") != cookie.Value {
		http.Error(w, "state mismatch", http.StatusBadRequest)
		return
	}
	// Clear the cookie immediately — it's a one-time-use value.
	http.SetCookie(w, &http.Cookie{
		Name:   "oauth_state",
		MaxAge: -1,
		Path:   "/",
	})

	// --- Step 2: Exchange the one-time code for an access token ---
	// This is a server-to-server call.  We pass our HTTP client via context
	// so tests can inject a mock server instead of calling Google for real.
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "missing code", http.StatusBadRequest)
		return
	}
	ctx := context.WithValue(r.Context(), oauth2.HTTPClient, h.httpClient)
	token, err := h.oauthConf.Exchange(ctx, code)
	if err != nil {
		http.Error(w, "failed to exchange token", http.StatusInternalServerError)
		return
	}

	// --- Step 3: Ask Google "who is this?" using the access token ---
	userInfo, err := h.fetchGoogleUserInfo(token.AccessToken)
	if err != nil {
		http.Error(w, "failed to fetch user info", http.StatusInternalServerError)
		return
	}

	// --- Step 4: Create a new user or find the existing one ---
	user, err := h.findOrCreateUser(userInfo)
	if err != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}

	// --- Step 5: Sign our own JWT for this user ---
	jwtToken, err := h.issueJWT(user.ID)
	if err != nil {
		http.Error(w, "failed to issue token", http.StatusInternalServerError)
		return
	}

	// --- Step 6: Send the JWT to the frontend via redirect ---
	// The frontend's /auth/callback page reads ?token= from the URL,
	// stores it in localStorage, and navigates to /dashboard.
	http.Redirect(w, r, h.frontendURL+"/auth/callback?token="+jwtToken, http.StatusTemporaryRedirect)
}

// googleUserInfo holds the fields we care about from Google's /userinfo endpoint.
type googleUserInfo struct {
	ID    string `json:"id"`
	Email string `json:"email"`
}

// fetchGoogleUserInfo calls Google's userinfo endpoint with the access token
// and decodes the response.  It uses h.userInfoURL (not a hardcoded URL) so
// tests can point it at a local mock server.
func (h *AuthHandler) fetchGoogleUserInfo(accessToken string) (*googleUserInfo, error) {
	resp, err := h.httpClient.Get(h.userInfoURL + "?access_token=" + accessToken)
	if err != nil {
		return nil, fmt.Errorf("userinfo request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("userinfo returned status %d", resp.StatusCode)
	}

	var info googleUserInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("failed to decode userinfo response: %w", err)
	}
	if info.ID == "" || info.Email == "" {
		return nil, fmt.Errorf("google returned incomplete user info")
	}
	return &info, nil
}

// findOrCreateUser looks up a user by their GoogleID.
// If none exists, it creates a new User row with the Google profile data.
// This "upsert" pattern means returning users are found instantly;
// new users are registered automatically on first login.
func (h *AuthHandler) findOrCreateUser(info *googleUserInfo) (*model.User, error) {
	var user model.User
	result := h.db.Where(model.User{GoogleID: &info.ID}).FirstOrCreate(&user, model.User{
		Email:    info.Email,
		GoogleID: &info.ID,
	})
	if result.Error != nil {
		return nil, fmt.Errorf("database upsert failed: %w", result.Error)
	}
	return &user, nil
}

// jwtClaims defines the payload we embed inside every JWT we issue.
// UserID is what we care about most — every authenticated request will
// include this so handlers know which user is acting.
type jwtClaims struct {
	UserID uint `json:"user_id"`
	jwt.RegisteredClaims
}

// issueJWT signs a new JWT containing the user's ID, expiring in 24 hours.
// We use HS256 (HMAC-SHA256) — a symmetric algorithm where the same secret
// both signs and verifies.  This is fast and appropriate for a single-service app.
func (h *AuthHandler) issueJWT(userID uint) (string, error) {
	claims := jwtClaims{
		UserID: userID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(h.jwtSecret)
}

// generateState creates a cryptographically random 32-character hex string.
// "Cryptographically random" means it's unpredictable — math/rand would be
// predictable and therefore insecure for a security parameter like this.
func generateState() (string, error) {
	b := make([]byte, 16) // 16 bytes → 32 hex characters
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate random state: %w", err)
	}
	return hex.EncodeToString(b), nil
}

func (h *AuthHandler) oauthConfigured() bool {
	if h.oauthConf == nil {
		return false
	}

	return h.oauthConf.ClientID != "" &&
		h.oauthConf.ClientSecret != "" &&
		h.oauthConf.RedirectURL != ""
}
