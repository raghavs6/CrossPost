package publisher

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/raghavs6/CrossPost/internal/model"
)

const (
	twitterDefaultTweetURL     = "https://api.twitter.com/2/tweets"
	facebookDefaultFeedBaseURL = "https://graph.facebook.com/v24.0"
)

// Error describes a publishing failure in a way HTTP handlers and workers can
// both understand without parsing strings.
type Error struct {
	StatusCode int
	Message    string
	MarkFailed bool
}

func (e *Error) Error() string {
	return e.Message
}

// Service publishes stored posts to supported social platforms.
type Service struct {
	db                  *gorm.DB
	httpClient          *http.Client
	tweetURL            string
	facebookFeedBaseURL string
	now                 func() time.Time
}

// New constructs a publisher with production API endpoints.
func New(db *gorm.DB) *Service {
	return &Service{
		db:                  db,
		httpClient:          &http.Client{Timeout: 10 * time.Second},
		tweetURL:            twitterDefaultTweetURL,
		facebookFeedBaseURL: facebookDefaultFeedBaseURL,
		now:                 time.Now,
	}
}

// NewForTest constructs a publisher with injectable endpoints and client.
func NewForTest(db *gorm.DB, httpClient *http.Client, tweetURL, facebookFeedBaseURL string) *Service {
	s := New(db)
	if httpClient != nil {
		s.httpClient = httpClient
	}
	if tweetURL != "" {
		s.tweetURL = tweetURL
	}
	if facebookFeedBaseURL != "" {
		s.facebookFeedBaseURL = facebookFeedBaseURL
	}
	return s
}

// Publish sends the post content to every supported platform in the post.
func (s *Service) Publish(ctx context.Context, userID uint, post *model.Post) error {
	targets := ParsePlatforms(post.Platforms)
	if len(targets) == 0 {
		return &Error{StatusCode: http.StatusBadRequest, Message: "post does not target a supported platform"}
	}

	for _, target := range targets {
		if err := s.publishToPlatform(ctx, userID, target, post.Content); err != nil {
			return err
		}
	}
	return nil
}

// SupportedPlatform reports whether CrossPost can publish to platform in v1.
func SupportedPlatform(platform string) bool {
	switch platform {
	case "twitter", "facebook":
		return true
	default:
		return false
	}
}

func (s *Service) publishToPlatform(ctx context.Context, userID uint, platform string, content string) error {
	switch platform {
	case "twitter":
		return s.publishTwitter(ctx, userID, content)
	case "facebook":
		return s.publishFacebook(ctx, userID, content)
	default:
		return &Error{StatusCode: http.StatusBadRequest, Message: "post targets an unsupported platform: " + platform}
	}
}

type tweetCreateRequest struct {
	Text string `json:"text"`
}

func (s *Service) publishTwitter(ctx context.Context, userID uint, content string) error {
	account, err := s.loadSocialAccount(userID, "twitter", "X")
	if err != nil {
		return err
	}

	body, err := json.Marshal(tweetCreateRequest{Text: content})
	if err != nil {
		return &Error{StatusCode: http.StatusInternalServerError, Message: "failed to encode tweet body"}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.tweetURL, bytes.NewReader(body))
	if err != nil {
		return &Error{StatusCode: http.StatusInternalServerError, Message: "failed to build tweet request"}
	}
	req.Header.Set("Authorization", "Bearer "+account.AccessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return &Error{StatusCode: http.StatusBadGateway, Message: "twitter request failed", MarkFailed: true}
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return &Error{
			StatusCode: http.StatusBadGateway,
			Message:    fmt.Sprintf("twitter returned %d: %s", resp.StatusCode, strings.TrimSpace(string(errBody))),
			MarkFailed: true,
		}
	}

	return nil
}

func (s *Service) publishFacebook(ctx context.Context, userID uint, content string) error {
	account, err := s.loadSocialAccount(userID, "facebook", "Facebook")
	if err != nil {
		return err
	}
	if account.PlatformAccountID == "" {
		return &Error{StatusCode: http.StatusPreconditionFailed, Message: "facebook page is missing, please reconnect"}
	}

	form := url.Values{}
	form.Set("message", content)

	feedURL := strings.TrimRight(s.facebookFeedBaseURL, "/") + "/" + account.PlatformAccountID + "/feed"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, feedURL, strings.NewReader(form.Encode()))
	if err != nil {
		return &Error{StatusCode: http.StatusInternalServerError, Message: "failed to build facebook post request"}
	}
	req.Header.Set("Authorization", "Bearer "+account.AccessToken)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return &Error{StatusCode: http.StatusBadGateway, Message: "facebook request failed", MarkFailed: true}
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return &Error{
			StatusCode: http.StatusBadGateway,
			Message:    fmt.Sprintf("facebook returned %d: %s", resp.StatusCode, strings.TrimSpace(string(errBody))),
			MarkFailed: true,
		}
	}

	return nil
}

func (s *Service) loadSocialAccount(userID uint, platform string, label string) (model.SocialAccount, error) {
	var account model.SocialAccount
	acctResult := s.db.Where("user_id = ? AND platform = ?", userID, platform).First(&account)
	if errors.Is(acctResult.Error, gorm.ErrRecordNotFound) {
		return model.SocialAccount{}, &Error{StatusCode: http.StatusPreconditionFailed, Message: "no linked " + label + " account"}
	}
	if acctResult.Error != nil {
		return model.SocialAccount{}, &Error{StatusCode: http.StatusInternalServerError, Message: "database error"}
	}

	if !account.TokenExpiry.IsZero() && !account.TokenExpiry.After(s.now()) {
		return model.SocialAccount{}, &Error{StatusCode: http.StatusUnauthorized, Message: label + " token expired, please reconnect"}
	}

	return account, nil
}

// ParsePlatforms converts the comma-separated storage value into clean platform
// names. Storage stays simple for v1; callers use this helper to avoid repeats.
func ParsePlatforms(csv string) []string {
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
