package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"

	"github.com/raghavs6/CrossPost/internal/middleware"
	"github.com/raghavs6/CrossPost/internal/model"
)

// PostHandler holds the database connection used by all post routes.
// This mirrors the AuthHandler pattern: one struct, one constructor, methods
// registered individually as chi route handlers.
type PostHandler struct {
	db *gorm.DB
}

// NewPostHandler constructs a PostHandler.  Call once in main.go and pass the
// returned value's methods to chi route registration.
func NewPostHandler(db *gorm.DB) *PostHandler {
	return &PostHandler{db: db}
}

// CreatePostRequest is the typed struct we decode the request body into.
// Using []string for Platforms because that's the natural JSON representation
// on the client side; we convert to a comma-separated string for storage.
type CreatePostRequest struct {
	Content     string    `json:"content"`
	Platforms   []string  `json:"platforms"`
	ScheduledAt time.Time `json:"scheduled_at"`
}

// UpdatePostRequest holds the fields the client may change on an existing post.
// It is intentionally separate from CreatePostRequest so each operation has an
// explicit, typed contract.
type UpdatePostRequest struct {
	Content     string    `json:"content"`
	Platforms   []string  `json:"platforms"`
	ScheduledAt time.Time `json:"scheduled_at"`
}

// PostResponse is what we send back to the client — never the raw model.Post,
// because that would expose internal fields like DeletedAt.
// Platforms is []string here (the friendly JSON array) even though it is stored
// as a comma-separated string internally.
type PostResponse struct {
	ID          uint      `json:"id"`
	Content     string    `json:"content"`
	Platforms   []string  `json:"platforms"`
	ScheduledAt time.Time `json:"scheduled_at"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
}

// toResponse converts an internal model.Post to the client-facing PostResponse.
// This is the single place where the platforms string is split back into a slice.
func toResponse(p model.Post) PostResponse {
	platforms := []string{}
	if p.Platforms != "" {
		platforms = strings.Split(p.Platforms, ",")
	}
	return PostResponse{
		ID:          p.ID,
		Content:     p.Content,
		Platforms:   platforms,
		ScheduledAt: p.ScheduledAt,
		Status:      p.Status,
		CreatedAt:   p.CreatedAt,
	}
}

// Create handles POST /api/posts.
// Decodes the request body, validates required fields, inserts a new post with
// Status="draft", and returns 201 Created with the created post.
func (h *PostHandler) Create(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())

	var req CreatePostRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if strings.TrimSpace(req.Content) == "" {
		http.Error(w, "content is required", http.StatusBadRequest)
		return
	}
	if len(req.Platforms) == 0 {
		http.Error(w, "at least one platform is required", http.StatusBadRequest)
		return
	}

	post := model.Post{
		UserID:      userID,
		Content:     req.Content,
		Platforms:   strings.Join(req.Platforms, ","),
		ScheduledAt: req.ScheduledAt,
		Status:      "draft",
	}

	if err := h.db.Create(&post).Error; err != nil {
		http.Error(w, "failed to create post", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(toResponse(post))
}

// List handles GET /api/posts.
// Returns all posts that belong to the logged-in user, ordered newest first.
// Ownership is enforced by the WHERE user_id = ? clause — a user can never
// see another user's posts.
func (h *PostHandler) List(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())

	var posts []model.Post
	if err := h.db.Where("user_id = ?", userID).Order("created_at desc").Find(&posts).Error; err != nil {
		http.Error(w, "failed to fetch posts", http.StatusInternalServerError)
		return
	}

	resp := make([]PostResponse, len(posts))
	for i, p := range posts {
		resp[i] = toResponse(p)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// GetByID handles GET /api/posts/{id}.
// Returns 404 for both "not found" and "belongs to a different user" — we do
// not leak the existence of other users' posts.
func (h *PostHandler) GetByID(w http.ResponseWriter, r *http.Request) {
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

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(toResponse(post))
}

// Update handles PUT /api/posts/{id}.
// Allows changing content, platforms, and scheduled_at.  Status transitions
// (e.g. draft → queued) will be handled separately by the job queue in a
// later phase.  Returns 404 for missing or wrong-user posts.
func (h *PostHandler) Update(w http.ResponseWriter, r *http.Request) {
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

	var req UpdatePostRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if strings.TrimSpace(req.Content) == "" {
		http.Error(w, "content is required", http.StatusBadRequest)
		return
	}
	if len(req.Platforms) == 0 {
		http.Error(w, "at least one platform is required", http.StatusBadRequest)
		return
	}

	post.Content = req.Content
	post.Platforms = strings.Join(req.Platforms, ",")
	post.ScheduledAt = req.ScheduledAt

	if err := h.db.Save(&post).Error; err != nil {
		http.Error(w, "failed to update post", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(toResponse(post))
}

// Delete handles DELETE /api/posts/{id}.
// Uses GORM's soft-delete (sets deleted_at) so the post row is preserved for
// audit purposes.  Returns 404 for missing or wrong-user posts, 204 on success.
func (h *PostHandler) Delete(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	id, err := parsePostID(r)
	if err != nil {
		http.Error(w, "invalid post id", http.StatusBadRequest)
		return
	}

	// First confirm the post exists AND belongs to this user.
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

	if err := h.db.Delete(&post).Error; err != nil {
		http.Error(w, "failed to delete post", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// parsePostID extracts the {id} URL parameter from the chi router context
// and converts it to a uint.  Returns an error if the parameter is missing or
// not a valid positive integer.
func parsePostID(r *http.Request) (uint, error) {
	raw := chi.URLParam(r, "id")
	n, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return 0, err
	}
	return uint(n), nil
}
