package queue

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/raghavs6/CrossPost/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type fakePublisher struct {
	calls int
	err   error
}

func (f *fakePublisher) Publish(_ context.Context, _ uint, _ *model.Post) error {
	f.calls++
	return f.err
}

func setupQueueTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.Post{}); err != nil {
		t.Fatalf("migrate post: %v", err)
	}
	return db
}

func seedQueuedPost(t *testing.T, db *gorm.DB, scheduledAt time.Time) model.Post {
	t.Helper()
	post := model.Post{
		UserID:      1,
		Content:     "queued content",
		Platforms:   "twitter",
		ScheduledAt: scheduledAt,
		Status:      "queued",
	}
	if err := db.Create(&post).Error; err != nil {
		t.Fatalf("create queued post: %v", err)
	}
	return post
}

func TestNewPublishPostTask(t *testing.T) {
	task, err := NewPublishPostTask(42)
	if err != nil {
		t.Fatalf("NewPublishPostTask: %v", err)
	}
	if task.Type() != TypePublishPost {
		t.Fatalf("expected task type %q, got %q", TypePublishPost, task.Type())
	}
	if len(task.Payload()) == 0 {
		t.Fatal("expected JSON payload")
	}
}

func TestPublishProcessor_RejectsEarlyJob(t *testing.T) {
	db := setupQueueTestDB(t)
	post := seedQueuedPost(t, db, time.Now().Add(1*time.Hour))
	publisher := &fakePublisher{}
	processor := NewPublishProcessor(db, publisher)

	task, err := NewPublishPostTask(post.ID)
	if err != nil {
		t.Fatalf("NewPublishPostTask: %v", err)
	}
	if err := processor.ProcessPublishPostTask(context.Background(), task); err == nil {
		t.Fatal("expected early scheduled task to return an error")
	}
	if publisher.calls != 0 {
		t.Fatalf("expected publisher not to run, got %d calls", publisher.calls)
	}
}

func TestPublishProcessor_IgnoresMissingPost(t *testing.T) {
	db := setupQueueTestDB(t)
	publisher := &fakePublisher{}
	processor := NewPublishProcessor(db, publisher)

	task, err := NewPublishPostTask(999)
	if err != nil {
		t.Fatalf("NewPublishPostTask: %v", err)
	}
	if err := processor.ProcessPublishPostTask(context.Background(), task); err != nil {
		t.Fatalf("expected missing post to be ignored, got %v", err)
	}
	if publisher.calls != 0 {
		t.Fatalf("expected publisher not to run, got %d calls", publisher.calls)
	}
}

func TestPublishProcessor_IgnoresNonQueuedPost(t *testing.T) {
	db := setupQueueTestDB(t)
	post := seedQueuedPost(t, db, time.Now().Add(-1*time.Minute))
	post.Status = "published"
	if err := db.Save(&post).Error; err != nil {
		t.Fatalf("update post status: %v", err)
	}
	publisher := &fakePublisher{}
	processor := NewPublishProcessor(db, publisher)

	task, err := NewPublishPostTask(post.ID)
	if err != nil {
		t.Fatalf("NewPublishPostTask: %v", err)
	}
	if err := processor.ProcessPublishPostTask(context.Background(), task); err != nil {
		t.Fatalf("expected non-queued post to be ignored, got %v", err)
	}
	if publisher.calls != 0 {
		t.Fatalf("expected publisher not to run, got %d calls", publisher.calls)
	}
}

func TestPublishProcessor_MarksPublishedOnSuccess(t *testing.T) {
	db := setupQueueTestDB(t)
	post := seedQueuedPost(t, db, time.Now().Add(-1*time.Minute))
	publisher := &fakePublisher{}
	processor := NewPublishProcessor(db, publisher)

	task, err := NewPublishPostTask(post.ID)
	if err != nil {
		t.Fatalf("NewPublishPostTask: %v", err)
	}
	if err := processor.ProcessPublishPostTask(context.Background(), task); err != nil {
		t.Fatalf("ProcessPublishPostTask: %v", err)
	}

	var reloaded model.Post
	if err := db.First(&reloaded, post.ID).Error; err != nil {
		t.Fatalf("reload post: %v", err)
	}
	if reloaded.Status != "published" {
		t.Fatalf("expected published status, got %q", reloaded.Status)
	}
}

func TestPublishProcessor_MarksFailedOnPublishError(t *testing.T) {
	db := setupQueueTestDB(t)
	post := seedQueuedPost(t, db, time.Now().Add(-1*time.Minute))
	publisher := &fakePublisher{err: errors.New("upstream failed")}
	processor := NewPublishProcessor(db, publisher)

	task, err := NewPublishPostTask(post.ID)
	if err != nil {
		t.Fatalf("NewPublishPostTask: %v", err)
	}
	if err := processor.ProcessPublishPostTask(context.Background(), task); err != nil {
		t.Fatalf("ProcessPublishPostTask: %v", err)
	}

	var reloaded model.Post
	if err := db.First(&reloaded, post.ID).Error; err != nil {
		t.Fatalf("reload post: %v", err)
	}
	if reloaded.Status != "failed" {
		t.Fatalf("expected failed status, got %q", reloaded.Status)
	}
}
