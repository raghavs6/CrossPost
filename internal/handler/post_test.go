package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"gorm.io/gorm"

	"github.com/raghavs6/CrossPost/internal/middleware"
	"github.com/raghavs6/CrossPost/internal/model"
)

// testSecret is the JWT signing key shared by all post handler tests.
// It must match the value passed to middleware.RequireAuth in each test.
const testSecret = "test-jwt-secret"

// signToken creates a signed JWT for the given userID.  This lets us inject
// a real (but test-scoped) auth token instead of bypassing the middleware.
func signToken(t *testing.T, userID uint) string {
	t.Helper()
	claims := jwtClaims{
		UserID: userID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	str, err := token.SignedString([]byte(testSecret))
	if err != nil {
		t.Fatalf("signToken: failed to sign: %v", err)
	}
	return str
}

// newPostRequest builds an HTTP request with:
//   - a JSON body (nil → no body)
//   - an Authorization: Bearer header carrying a JWT for userID
//   - optional chi URL param "id" (set via chiID, empty string = no param)
//
// All post handler tests go through the real RequireAuth middleware, so these
// requests must include a valid Authorization header.
func newPostRequest(t *testing.T, method, target string, body any, userID uint, chiID string) *http.Request {
	t.Helper()

	var req *http.Request
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("newPostRequest: failed to marshal body: %v", err)
		}
		req = httptest.NewRequest(method, target, bytes.NewReader(encoded))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, target, nil)
	}

	req.Header.Set("Authorization", "Bearer "+signToken(t, userID))

	// Inject a chi route context if the handler needs an {id} URL parameter.
	// chi.URLParam reads from this context — without it, URLParam returns "".
	if chiID != "" {
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", chiID)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	}

	return req
}

// runWithAuth wraps handlerFn in the real RequireAuth middleware, records the
// response, and returns the recorder so tests can inspect status and body.
func runWithAuth(handlerFn http.HandlerFunc, req *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	middleware.RequireAuth(testSecret)(handlerFn).ServeHTTP(w, req)
	return w
}

// seedPost inserts a post into the test database and returns it.
// Useful for setting up the "given a post exists" precondition.
func seedPost(t *testing.T, db *gorm.DB, userID uint, content string) model.Post {
	t.Helper()
	post := model.Post{
		UserID:      userID,
		Content:     content,
		Platforms:   "twitter",
		ScheduledAt: time.Now().Add(24 * time.Hour),
		Status:      "draft",
	}
	if err := db.Create(&post).Error; err != nil {
		t.Fatalf("seedPost: failed to insert post: %v", err)
	}
	return post
}

// ---------------------------------------------------------------------------
// Create
// ---------------------------------------------------------------------------

func TestPostCreate(t *testing.T) {
	tests := []struct {
		name           string
		body           any
		expectedStatus int
	}{
		{
			name: "success",
			body: CreatePostRequest{
				Content:     "Hello world!",
				Platforms:   []string{"twitter", "linkedin"},
				ScheduledAt: time.Now().Add(1 * time.Hour),
			},
			expectedStatus: http.StatusCreated,
		},
		{
			name: "missing content",
			body: CreatePostRequest{
				Content:     "",
				Platforms:   []string{"twitter"},
				ScheduledAt: time.Now().Add(1 * time.Hour),
			},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "whitespace only content",
			body: CreatePostRequest{
				Content:     "   ",
				Platforms:   []string{"twitter"},
				ScheduledAt: time.Now().Add(1 * time.Hour),
			},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "missing platforms",
			body: CreatePostRequest{
				Content:     "No platform specified",
				Platforms:   []string{},
				ScheduledAt: time.Now().Add(1 * time.Hour),
			},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "invalid json body",
			body:           nil, // we'll send a raw invalid string below
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db := setupTestDB(t)
			h := NewPostHandler(db)

			var req *http.Request
			if tc.name == "invalid json body" {
				// Manually craft a bad JSON body — json.Marshal can't produce this.
				req = httptest.NewRequest(http.MethodPost, "/api/posts", bytes.NewBufferString("{bad json}"))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Authorization", "Bearer "+signToken(t, 1))
			} else {
				req = newPostRequest(t, http.MethodPost, "/api/posts", tc.body, 1, "")
			}

			w := runWithAuth(h.Create, req)

			if w.Code != tc.expectedStatus {
				t.Errorf("expected status %d, got %d (body: %s)", tc.expectedStatus, w.Code, w.Body.String())
			}

			// On success, verify the response body contains the created post.
			if tc.expectedStatus == http.StatusCreated {
				var resp PostResponse
				if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
					t.Fatalf("failed to decode response: %v", err)
				}
				if resp.ID == 0 {
					t.Error("expected non-zero ID in created post response")
				}
				if resp.Status != "draft" {
					t.Errorf("expected status=draft, got %q", resp.Status)
				}
				if len(resp.Platforms) != 2 {
					t.Errorf("expected 2 platforms, got %d", len(resp.Platforms))
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// List
// ---------------------------------------------------------------------------

func TestPostList(t *testing.T) {
	t.Run("returns only the requesting user's posts", func(t *testing.T) {
		db := setupTestDB(t)
		h := NewPostHandler(db)

		// Seed two posts for user 1 and one for user 2.
		seedPost(t, db, 1, "User 1 post A")
		seedPost(t, db, 1, "User 1 post B")
		seedPost(t, db, 2, "User 2 post")

		req := newPostRequest(t, http.MethodGet, "/api/posts", nil, 1, "")
		w := runWithAuth(h.List, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d (body: %s)", w.Code, w.Body.String())
		}

		var resp []PostResponse
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if len(resp) != 2 {
			t.Errorf("expected 2 posts for user 1, got %d", len(resp))
		}
	})

	t.Run("returns empty array when user has no posts", func(t *testing.T) {
		db := setupTestDB(t)
		h := NewPostHandler(db)

		req := newPostRequest(t, http.MethodGet, "/api/posts", nil, 99, "")
		w := runWithAuth(h.List, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}

		var resp []PostResponse
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if len(resp) != 0 {
			t.Errorf("expected 0 posts, got %d", len(resp))
		}
	})
}

// ---------------------------------------------------------------------------
// GetByID
// ---------------------------------------------------------------------------

func TestPostGetByID(t *testing.T) {
	tests := []struct {
		name           string
		requesterID    uint
		expectedStatus int
	}{
		{"owner can read their post", 1, http.StatusOK},
		{"wrong user gets 404 (not 403, no existence leak)", 2, http.StatusNotFound},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db := setupTestDB(t)
			h := NewPostHandler(db)
			post := seedPost(t, db, 1, "Owner's post")

			req := newPostRequest(t, http.MethodGet, fmt.Sprintf("/api/posts/%d", post.ID), nil, tc.requesterID, fmt.Sprintf("%d", post.ID))
			w := runWithAuth(h.GetByID, req)

			if w.Code != tc.expectedStatus {
				t.Errorf("expected %d, got %d (body: %s)", tc.expectedStatus, w.Code, w.Body.String())
			}
		})
	}

	t.Run("non-existent post returns 404", func(t *testing.T) {
		db := setupTestDB(t)
		h := NewPostHandler(db)

		req := newPostRequest(t, http.MethodGet, "/api/posts/9999", nil, 1, "9999")
		w := runWithAuth(h.GetByID, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d", w.Code)
		}
	})

	t.Run("invalid id returns 400", func(t *testing.T) {
		db := setupTestDB(t)
		h := NewPostHandler(db)

		req := newPostRequest(t, http.MethodGet, "/api/posts/abc", nil, 1, "abc")
		w := runWithAuth(h.GetByID, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", w.Code)
		}
	})
}

// ---------------------------------------------------------------------------
// Update
// ---------------------------------------------------------------------------

func TestPostUpdate(t *testing.T) {
	validBody := UpdatePostRequest{
		Content:     "Updated content",
		Platforms:   []string{"linkedin"},
		ScheduledAt: time.Now().Add(48 * time.Hour),
	}

	tests := []struct {
		name           string
		requesterID    uint
		body           any
		expectedStatus int
	}{
		{"owner can update their post", 1, validBody, http.StatusOK},
		{"wrong user gets 404", 2, validBody, http.StatusNotFound},
		{"missing content returns 400", 1, UpdatePostRequest{Content: "", Platforms: []string{"twitter"}, ScheduledAt: time.Now()}, http.StatusBadRequest},
		{"missing platforms returns 400", 1, UpdatePostRequest{Content: "Hi", Platforms: []string{}, ScheduledAt: time.Now()}, http.StatusBadRequest},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db := setupTestDB(t)
			h := NewPostHandler(db)
			post := seedPost(t, db, 1, "Original content")

			req := newPostRequest(t, http.MethodPut, fmt.Sprintf("/api/posts/%d", post.ID), tc.body, tc.requesterID, fmt.Sprintf("%d", post.ID))
			w := runWithAuth(h.Update, req)

			if w.Code != tc.expectedStatus {
				t.Errorf("expected %d, got %d (body: %s)", tc.expectedStatus, w.Code, w.Body.String())
			}

			// On success, verify the response reflects the updated content.
			if tc.expectedStatus == http.StatusOK {
				var resp PostResponse
				if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
					t.Fatalf("failed to decode response: %v", err)
				}
				if resp.Content != "Updated content" {
					t.Errorf("expected updated content, got %q", resp.Content)
				}
			}
		})
	}

	t.Run("non-existent post returns 404", func(t *testing.T) {
		db := setupTestDB(t)
		h := NewPostHandler(db)

		req := newPostRequest(t, http.MethodPut, "/api/posts/9999", validBody, 1, "9999")
		w := runWithAuth(h.Update, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d", w.Code)
		}
	})
}

// ---------------------------------------------------------------------------
// Delete
// ---------------------------------------------------------------------------

func TestPostDelete(t *testing.T) {
	tests := []struct {
		name           string
		requesterID    uint
		expectedStatus int
	}{
		{"owner can delete their post", 1, http.StatusNoContent},
		{"wrong user gets 404", 2, http.StatusNotFound},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db := setupTestDB(t)
			h := NewPostHandler(db)
			post := seedPost(t, db, 1, "Post to maybe delete")

			req := newPostRequest(t, http.MethodDelete, fmt.Sprintf("/api/posts/%d", post.ID), nil, tc.requesterID, fmt.Sprintf("%d", post.ID))
			w := runWithAuth(h.Delete, req)

			if w.Code != tc.expectedStatus {
				t.Errorf("expected %d, got %d", tc.expectedStatus, w.Code)
			}

			// After a successful delete, the post should be soft-deleted (not visible).
			if tc.expectedStatus == http.StatusNoContent {
				var count int64
				db.Model(&model.Post{}).Where("id = ?", post.ID).Count(&count)
				if count != 0 {
					t.Errorf("expected post to be soft-deleted, but it still appears in queries")
				}
			}
		})
	}

	t.Run("non-existent post returns 404", func(t *testing.T) {
		db := setupTestDB(t)
		h := NewPostHandler(db)

		req := newPostRequest(t, http.MethodDelete, "/api/posts/9999", nil, 1, "9999")
		w := runWithAuth(h.Delete, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d", w.Code)
		}
	})
}
