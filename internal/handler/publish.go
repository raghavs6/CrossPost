package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/raghavs6/CrossPost/internal/middleware"
	"github.com/raghavs6/CrossPost/internal/model"
)

// twitterDefaultTweetURL is the X API v2 endpoint that creates a new tweet.
const twitterDefaultTweetURL = "https://api.twitter.com/2/tweets"

// PublishHandler turns a stored draft Post into a real tweet on the X API.
//
// httpClient and tweetURL are injectable so tests can point them at an
// httptest.NewServer instead of the real Twitter API — same pattern as
// TwitterAuthHandler.
type PublishHandler struct {
	db         *gorm.DB
	httpClient *http.Client
	tweetURL   string
}

// NewPublishHandler constructs a PublishHandler with sensible defaults.
func NewPublishHandler(db *gorm.DB) *PublishHandler {
	return &PublishHandler{
		db:         db,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		tweetURL:   twitterDefaultTweetURL,
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

	if !containsPlatform(post.Platforms, "twitter") {
		http.Error(w, "post does not target twitter", http.StatusBadRequest)
		return
	}

	var account model.SocialAccount
	acctResult := h.db.Where("user_id = ? AND platform = ?", userID, "twitter").First(&account)
	if errors.Is(acctResult.Error, gorm.ErrRecordNotFound) {
		http.Error(w, "no linked X account", http.StatusPreconditionFailed)
		return
	}
	if acctResult.Error != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}

	// Treat zero TokenExpiry as "no expiry recorded" and let the request through;
	// the X API will reject it if invalid. Otherwise refuse pre-emptively.
	if !account.TokenExpiry.IsZero() && !account.TokenExpiry.After(time.Now()) {
		http.Error(w, "X token expired, please reconnect", http.StatusUnauthorized)
		return
	}

	body, err := json.Marshal(tweetCreateRequest{Text: post.Content})
	if err != nil {
		http.Error(w, "failed to encode tweet body", http.StatusInternalServerError)
		return
	}

	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, h.tweetURL, bytes.NewReader(body))
	if err != nil {
		http.Error(w, "failed to build tweet request", http.StatusInternalServerError)
		return
	}
	req.Header.Set("Authorization", "Bearer "+account.AccessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := h.httpClient.Do(req)
	if err != nil {
		h.markFailed(&post)
		http.Error(w, "twitter request failed", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		h.markFailed(&post)
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		http.Error(w, fmt.Sprintf("twitter returned %d: %s", resp.StatusCode, strings.TrimSpace(string(errBody))), http.StatusBadGateway)
		return
	}

	post.Status = "published"
	if err := h.db.Save(&post).Error; err != nil {
		http.Error(w, "tweet posted but failed to update post status", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(toResponse(post))
}

// markFailed flips a post's status to "failed" and persists the change.
// Errors are intentionally swallowed — the caller is already returning an
// error response and a logging failure here would obscure the real cause.
func (h *PublishHandler) markFailed(post *model.Post) {
	post.Status = "failed"
	h.db.Save(post)
}

// containsPlatform returns true when name appears in the post's
// comma-separated Platforms column. Whitespace around entries is tolerated
// so "twitter, linkedin" still matches "twitter".
func containsPlatform(csv, name string) bool {
	for _, p := range strings.Split(csv, ",") {
		if strings.TrimSpace(p) == name {
			return true
		}
	}
	return false
}
