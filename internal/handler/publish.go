package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"gorm.io/gorm"

	"github.com/raghavs6/CrossPost/internal/middleware"
	"github.com/raghavs6/CrossPost/internal/model"
	"github.com/raghavs6/CrossPost/internal/publisher"
)

// PublishHandler turns a stored draft Post into a real tweet on the X API.
type PublishHandler struct {
	db        *gorm.DB
	publisher *publisher.Service
}

// NewPublishHandler constructs a PublishHandler with sensible defaults.
func NewPublishHandler(db *gorm.DB) *PublishHandler {
	return &PublishHandler{
		db:        db,
		publisher: publisher.New(db),
	}
}

func NewPublishHandlerWithPublisher(db *gorm.DB, publishService *publisher.Service) *PublishHandler {
	return &PublishHandler{db: db, publisher: publishService}
}

// Publish handles POST /api/posts/{id}/publish (protected — RequireAuth must run first).
//
// This is a manual/dev publish path. Scheduled posts use the worker, so this
// endpoint only accepts drafts and does not bypass queued scheduled jobs.
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

	if err := h.publisher.Publish(r.Context(), userID, &post); err != nil {
		var publishErr *publisher.Error
		if errors.As(err, &publishErr) {
			if publishErr.MarkFailed {
				h.markFailed(&post)
			}
			http.Error(w, publishErr.Message, publishErr.StatusCode)
			return
		}
		http.Error(w, "failed to publish post", http.StatusInternalServerError)
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
