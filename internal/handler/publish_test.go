package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/raghavs6/CrossPost/internal/model"
)

// seedTwitterAccount inserts a SocialAccount row so the publish handler can
// find a linked X account for the user. expiry controls TokenExpiry — pass a
// future time for the happy path, a past time to exercise the expired branch.
func seedTwitterAccount(t *testing.T, db *gorm.DB, userID uint, accessToken string, expiry time.Time) model.SocialAccount {
	t.Helper()
	account := model.SocialAccount{
		UserID:         userID,
		Platform:       "twitter",
		PlatformUserID: "999",
		DisplayName:    "Tester",
		Username:       "tester",
		AccessToken:    accessToken,
		TokenExpiry:    expiry,
	}
	if err := db.Create(&account).Error; err != nil {
		t.Fatalf("seedTwitterAccount: %v", err)
	}
	return account
}

// newPublishHandler builds a PublishHandler whose tweetURL points at ts so the
// test can assert what the handler sent without hitting the real X API.
func newPublishHandler(db *gorm.DB, ts *httptest.Server) *PublishHandler {
	h := NewPublishHandler(db)
	if ts != nil {
		h.tweetURL = ts.URL
	}
	return h
}

func TestPublishHappyPath(t *testing.T) {
	db := setupTestDB(t)
	const userID uint = 1
	post := seedPost(t, db, userID, "Hello from CrossPost!")
	seedTwitterAccount(t, db, userID, "valid-token", time.Now().Add(1*time.Hour))

	var gotAuth, gotBody string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"data":{"id":"1","text":"Hello from CrossPost!"}}`))
	}))
	defer ts.Close()

	h := newPublishHandler(db, ts)
	req := newPostRequest(t, http.MethodPost, "/api/posts/1/publish", nil, userID, "1")
	w := runWithAuth(h.Publish, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", w.Code, w.Body.String())
	}
	if gotAuth != "Bearer valid-token" {
		t.Errorf("expected bearer header, got %q", gotAuth)
	}
	if !strings.Contains(gotBody, "Hello from CrossPost!") {
		t.Errorf("expected tweet text in body, got %q", gotBody)
	}

	var resp PostResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Status != "published" {
		t.Errorf("expected response status 'published', got %q", resp.Status)
	}

	var reloaded model.Post
	if err := db.First(&reloaded, post.ID).Error; err != nil {
		t.Fatalf("reload post: %v", err)
	}
	if reloaded.Status != "published" {
		t.Errorf("expected DB status 'published', got %q", reloaded.Status)
	}
}

func TestPublishWrongOwner(t *testing.T) {
	db := setupTestDB(t)
	const owner uint = 1
	const other uint = 2
	post := seedPost(t, db, owner, "owned by user 1")
	seedTwitterAccount(t, db, other, "tok", time.Now().Add(1*time.Hour))

	h := newPublishHandler(db, nil) // no http server — request must not get that far
	req := newPostRequest(t, http.MethodPost, "/api/posts/1/publish", nil, other, "1")
	w := runWithAuth(h.Publish, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for non-owner, got %d", w.Code)
	}

	var reloaded model.Post
	if err := db.First(&reloaded, post.ID).Error; err != nil {
		t.Fatalf("reload post: %v", err)
	}
	if reloaded.Status != "draft" {
		t.Errorf("status should remain draft, got %q", reloaded.Status)
	}
}

func TestPublishAlreadyPublished(t *testing.T) {
	db := setupTestDB(t)
	const userID uint = 1
	post := seedPost(t, db, userID, "already done")
	post.Status = "published"
	if err := db.Save(&post).Error; err != nil {
		t.Fatalf("update post: %v", err)
	}
	seedTwitterAccount(t, db, userID, "tok", time.Now().Add(1*time.Hour))

	h := newPublishHandler(db, nil)
	req := newPostRequest(t, http.MethodPost, "/api/posts/1/publish", nil, userID, "1")
	w := runWithAuth(h.Publish, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for non-draft, got %d", w.Code)
	}
}

func TestPublishWrongPlatform(t *testing.T) {
	db := setupTestDB(t)
	const userID uint = 1
	post := model.Post{
		UserID:      userID,
		Content:     "linkedin only",
		Platforms:   "linkedin",
		ScheduledAt: time.Now().Add(1 * time.Hour),
		Status:      "draft",
	}
	if err := db.Create(&post).Error; err != nil {
		t.Fatalf("create post: %v", err)
	}
	seedTwitterAccount(t, db, userID, "tok", time.Now().Add(1*time.Hour))

	h := newPublishHandler(db, nil)
	req := newPostRequest(t, http.MethodPost, "/api/posts/1/publish", nil, userID, "1")
	w := runWithAuth(h.Publish, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for wrong platform, got %d", w.Code)
	}
}

func TestPublishNoLinkedAccount(t *testing.T) {
	db := setupTestDB(t)
	const userID uint = 1
	seedPost(t, db, userID, "no linked account")

	h := newPublishHandler(db, nil)
	req := newPostRequest(t, http.MethodPost, "/api/posts/1/publish", nil, userID, "1")
	w := runWithAuth(h.Publish, req)

	if w.Code != http.StatusPreconditionFailed {
		t.Fatalf("expected 412, got %d", w.Code)
	}
}

func TestPublishExpiredToken(t *testing.T) {
	db := setupTestDB(t)
	const userID uint = 1
	post := seedPost(t, db, userID, "expired token")
	seedTwitterAccount(t, db, userID, "stale", time.Now().Add(-1*time.Hour))

	h := newPublishHandler(db, nil)
	req := newPostRequest(t, http.MethodPost, "/api/posts/1/publish", nil, userID, "1")
	w := runWithAuth(h.Publish, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}

	var reloaded model.Post
	if err := db.First(&reloaded, post.ID).Error; err != nil {
		t.Fatalf("reload post: %v", err)
	}
	if reloaded.Status != "draft" {
		t.Errorf("expected status to remain draft, got %q", reloaded.Status)
	}
}

func TestPublishTwitterAPIError(t *testing.T) {
	db := setupTestDB(t)
	const userID uint = 1
	post := seedPost(t, db, userID, "will fail")
	seedTwitterAccount(t, db, userID, "tok", time.Now().Add(1*time.Hour))

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"upstream boom"}`))
	}))
	defer ts.Close()

	h := newPublishHandler(db, ts)
	req := newPostRequest(t, http.MethodPost, "/api/posts/1/publish", nil, userID, "1")
	w := runWithAuth(h.Publish, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d (body: %s)", w.Code, w.Body.String())
	}

	var reloaded model.Post
	if err := db.First(&reloaded, post.ID).Error; err != nil {
		t.Fatalf("reload post: %v", err)
	}
	if reloaded.Status != "failed" {
		t.Errorf("expected DB status 'failed', got %q", reloaded.Status)
	}
}
