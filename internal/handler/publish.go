package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/raghavs6/CrossPost/internal/middleware"
	"github.com/raghavs6/CrossPost/internal/model"
)

// twitterDefaultTweetURL is the X API v2 endpoint that creates a new tweet.
const (
	twitterDefaultTweetURL     = "https://api.twitter.com/2/tweets"
	facebookDefaultFeedBaseURL = "https://graph.facebook.com/" + facebookGraphVersion
)

// PublishHandler turns a stored draft Post into a real tweet on the X API.
//
// httpClient and tweetURL are injectable so tests can point them at an
// httptest.NewServer instead of the real Twitter API — same pattern as
// TwitterAuthHandler.
type PublishHandler struct {
	db                  *gorm.DB
	httpClient          *http.Client
	tweetURL            string
	facebookFeedBaseURL string
}

// NewPublishHandler constructs a PublishHandler with sensible defaults.
func NewPublishHandler(db *gorm.DB) *PublishHandler {
	return &PublishHandler{
		db:                  db,
		httpClient:          &http.Client{Timeout: 10 * time.Second},
		tweetURL:            twitterDefaultTweetURL,
		facebookFeedBaseURL: facebookDefaultFeedBaseURL,
	}
}

// tweetCreateRequest is the JSON body shape for POST /2/tweets.
type tweetCreateRequest struct {
	Text string `json:"text"`
}

// Publish handles POST /api/posts/{id}/publish (protected — RequireAuth must run first).
//
// Synchronous flow: load the user's draft post, look up their linked X account,
// POST the tweet, then update the post's status in the database. There is no
// queue or worker yet — that refactor comes later.
func (h *PublishHandler) Publish(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())

	id, err := parsePostID(r)
	if err != nil {
		http.Error(w, "invalid post id", http.StatusBadRequest)
		return
	}

	var post model.Post
	result := h.db.Where("id = ? AND user_id = ?", id, userID).First(&post)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		http.Error(w, "post not found", http.StatusNotFound)
		return
	}
	if result.Error != nil {
		http.Error(w, "failed to fetch post", http.StatusInternalServerError)
		return
	}

	if post.Status != "draft" {
		http.Error(w, "post is not a draft", http.StatusBadRequest)
		return
	}

	targets := parsePlatforms(post.Platforms)
	if len(targets) == 0 {
		http.Error(w, "post does not target a supported platform", http.StatusBadRequest)
		return
	}

	for _, target := range targets {
		status, msg, shouldMarkFailed := h.publishToPlatform(r, userID, target, post.Content)
		if status != http.StatusOK {
			if shouldMarkFailed {
				h.markFailed(&post)
			}
			http.Error(w, msg, status)
			return
		}
	}

	post.Status = "published"
	if err := h.db.Save(&post).Error; err != nil {
		http.Error(w, "tweet posted but failed to update post status", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(toResponse(post))
}

func (h *PublishHandler) publishToPlatform(r *http.Request, userID uint, platform string, content string) (int, string, bool) {
	switch platform {
	case "twitter":
		return h.publishTwitter(r, userID, content)
	case "facebook":
		return h.publishFacebook(r, userID, content)
	default:
		return http.StatusBadRequest, "post targets an unsupported platform: " + platform, false
	}
}

func (h *PublishHandler) publishTwitter(r *http.Request, userID uint, content string) (int, string, bool) {
	account, status, msg := h.loadSocialAccount(userID, "twitter", "X")
	if status != http.StatusOK {
		return status, msg, false
	}

	body, err := json.Marshal(tweetCreateRequest{Text: content})
	if err != nil {
		return http.StatusInternalServerError, "failed to encode tweet body", false
	}

	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, h.tweetURL, bytes.NewReader(body))
	if err != nil {
		return http.StatusInternalServerError, "failed to build tweet request", false
	}
	req.Header.Set("Authorization", "Bearer "+account.AccessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return http.StatusBadGateway, "twitter request failed", true
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return http.StatusBadGateway, fmt.Sprintf("twitter returned %d: %s", resp.StatusCode, strings.TrimSpace(string(errBody))), true
	}

	return http.StatusOK, "", false
}

func (h *PublishHandler) publishFacebook(r *http.Request, userID uint, content string) (int, string, bool) {
	account, status, msg := h.loadSocialAccount(userID, "facebook", "Facebook")
	if status != http.StatusOK {
		return status, msg, false
	}
	if account.PlatformAccountID == "" {
		return http.StatusPreconditionFailed, "facebook page is missing, please reconnect", false
	}

	form := url.Values{}
	form.Set("message", content)

	feedURL := strings.TrimRight(h.facebookFeedBaseURL, "/") + "/" + account.PlatformAccountID + "/feed"
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, feedURL, strings.NewReader(form.Encode()))
	if err != nil {
		return http.StatusInternalServerError, "failed to build facebook post request", false
	}
	req.Header.Set("Authorization", "Bearer "+account.AccessToken)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return http.StatusBadGateway, "facebook request failed", true
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return http.StatusBadGateway, fmt.Sprintf("facebook returned %d: %s", resp.StatusCode, strings.TrimSpace(string(errBody))), true
	}

	return http.StatusOK, "", false
}

func (h *PublishHandler) loadSocialAccount(userID uint, platform string, label string) (model.SocialAccount, int, string) {
	var account model.SocialAccount
	acctResult := h.db.Where("user_id = ? AND platform = ?", userID, platform).First(&account)
	if errors.Is(acctResult.Error, gorm.ErrRecordNotFound) {
		return model.SocialAccount{}, http.StatusPreconditionFailed, "no linked " + label + " account"
	}
	if acctResult.Error != nil {
		return model.SocialAccount{}, http.StatusInternalServerError, "database error"
	}

	if !account.TokenExpiry.IsZero() && !account.TokenExpiry.After(time.Now()) {
		return model.SocialAccount{}, http.StatusUnauthorized, label + " token expired, please reconnect"
	}

	return account, http.StatusOK, ""
}

// markFailed flips a post's status to "failed" and persists the change.
// Errors are intentionally swallowed — the caller is already returning an
// error response and a logging failure here would obscure the real cause.
func (h *PublishHandler) markFailed(post *model.Post) {
	post.Status = "failed"
	h.db.Save(post)
}

func parsePlatforms(csv string) []string {
	platforms := []string{}
	for _, p := range strings.Split(csv, ",") {
		trimmed := strings.TrimSpace(p)
		if trimmed == "" {
			continue
		}
		platforms = append(platforms, trimmed)
	}
	return platforms
}
