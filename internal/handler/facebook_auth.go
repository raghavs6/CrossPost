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
	facebookDefaultAuthURL    = "https://www.facebook.com/v23.0/dialog/oauth"
	facebookDefaultTokenURL   = "https://graph.facebook.com/v23.0/oauth/access_token"
	facebookDefaultProfileURL = "https://graph.facebook.com/me?fields=id,name"
)

// FacebookAuthHandler owns the Facebook Login account-linking flow.
// Unlike Google login, this does not authenticate into CrossPost itself —
// it links a Facebook account to an already-authenticated CrossPost user.
type FacebookAuthHandler struct {
	appID       string
	appSecret   string
	redirectURL string
	db          *gorm.DB
	httpClient  *http.Client
	authURL     string
	tokenURL    string
	profileURL  string
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

// FacebookCallback exchanges the code for a token, fetches the user's profile,
// stores the linked account, and redirects back to the dashboard.
func (h *FacebookAuthHandler) FacebookCallback(w http.ResponseWriter, r *http.Request) {
	stateCookie, err := r.Cookie("facebook_state")
	if err != nil || r.URL.Query().Get("state") != stateCookie.Value {
		http.Error(w, "state mismatch", http.StatusBadRequest)
		return
	}

	userIDCookie, err := r.Cookie("facebook_linking_user_id")
	if err != nil {
		http.Error(w, "missing user id cookie", http.StatusBadRequest)
		return
	}

	for _, name := range []string{"facebook_state", "facebook_linking_user_id"} {
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

	profile, err := h.fetchFacebookProfile(token)
	if err != nil {
		http.Error(w, "failed to fetch facebook user info", http.StatusInternalServerError)
		return
	}

	var account model.SocialAccount
	result := h.db.Where(model.SocialAccount{UserID: userID, Platform: "facebook"}).
		Assign(model.SocialAccount{
			PlatformUserID: profile.ID,
			DisplayName:    profile.Name,
			Username:       "",
			AccessToken:    token,
			TokenExpiry:    expiresAt,
		}).
		FirstOrCreate(&account)
	if result.Error != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, h.frontendURL+"/dashboard?facebook=connected", http.StatusFound)
}

// ConnectionResponse is the JSON shape ListConnections returns to the frontend.
type ConnectionResponse struct {
	Platform    string    `json:"platform"`
	DisplayName string    `json:"display_name"`
	Username    string    `json:"username,omitempty"`
	ConnectedAt time.Time `json:"connected_at"`
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
	q.Set("scope", "public_profile,pages_show_list,pages_read_engagement,pages_manage_posts")
	u.RawQuery = q.Encode()
	return u.String()
}

type facebookTokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int64  `json:"expires_in"`
}

func (h *FacebookAuthHandler) exchangeCode(code string) (string, time.Time, error) {
	u, _ := url.Parse(h.tokenURL)
	q := u.Query()
	q.Set("client_id", h.appID)
	q.Set("client_secret", h.appSecret)
	q.Set("redirect_uri", h.redirectURL)
	q.Set("code", code)
	u.RawQuery = q.Encode()

	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("failed to build token request: %w", err)
	}

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", time.Time{}, fmt.Errorf("token endpoint returned %d", resp.StatusCode)
	}

	var tokenResp facebookTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", time.Time{}, fmt.Errorf("failed to decode token response: %w", err)
	}
	if tokenResp.AccessToken == "" {
		return "", time.Time{}, fmt.Errorf("facebook returned no access token")
	}

	var expiresAt time.Time
	if tokenResp.ExpiresIn > 0 {
		expiresAt = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	}

	return tokenResp.AccessToken, expiresAt, nil
}

type facebookProfile struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (h *FacebookAuthHandler) fetchFacebookProfile(accessToken string) (*facebookProfile, error) {
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
