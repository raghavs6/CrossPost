package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/hibiken/asynq"
	"gorm.io/gorm"

	"github.com/raghavs6/CrossPost/internal/model"
)

const TypePublishPost = "post:publish"

type PublishPostPayload struct {
	PostID uint `json:"post_id"`
}

// Scheduler is the small interface PostHandler needs. Tests can provide a fake
// without connecting to Redis.
type Scheduler interface {
	SchedulePublish(ctx context.Context, post model.Post) error
}

type AsynqScheduler struct {
	client *asynq.Client
}

func NewScheduler(redisAddr string) *AsynqScheduler {
	return &AsynqScheduler{
		client: asynq.NewClient(asynq.RedisClientOpt{Addr: redisAddr}),
	}
}

func NewSchedulerFromClient(client *asynq.Client) *AsynqScheduler {
	return &AsynqScheduler{client: client}
}

func (s *AsynqScheduler) Close() error {
	return s.client.Close()
}

func (s *AsynqScheduler) SchedulePublish(ctx context.Context, post model.Post) error {
	task, err := NewPublishPostTask(post.ID)
	if err != nil {
		return err
	}

	_, err = s.client.EnqueueContext(
		ctx,
		task,
		asynq.ProcessAt(post.ScheduledAt),
		asynq.TaskID(fmt.Sprintf("publish-post-%d", post.ID)),
	)
	return err
}

func NewPublishPostTask(postID uint) (*asynq.Task, error) {
	payload, err := json.Marshal(PublishPostPayload{PostID: postID})
	if err != nil {
		return nil, fmt.Errorf("marshal publish post task: %w", err)
	}
	return asynq.NewTask(TypePublishPost, payload), nil
}

type Publisher interface {
	Publish(ctx context.Context, userID uint, post *model.Post) error
}

type PublishProcessor struct {
	db        *gorm.DB
	publisher Publisher
	now       func() time.Time
}

func NewPublishProcessor(db *gorm.DB, publisher Publisher) *PublishProcessor {
	return &PublishProcessor{
		db:        db,
		publisher: publisher,
		now:       time.Now,
	}
}

func (p *PublishProcessor) Register(mux *asynq.ServeMux) {
	mux.HandleFunc(TypePublishPost, p.ProcessPublishPostTask)
}

func (p *PublishProcessor) ProcessPublishPostTask(ctx context.Context, task *asynq.Task) error {
	var payload PublishPostPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		return fmt.Errorf("decode publish post payload: %w", err)
	}

	var post model.Post
	result := p.db.First(&post, payload.PostID)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return nil
	}
	if result.Error != nil {
		return fmt.Errorf("load queued post %d: %w", payload.PostID, result.Error)
	}

	if post.Status != "queued" {
		return nil
	}
	if p.now().Before(post.ScheduledAt) {
		return fmt.Errorf("post %d is not scheduled to publish yet", post.ID)
	}

	if err := p.publisher.Publish(ctx, post.UserID, &post); err != nil {
		post.Status = "failed"
		if saveErr := p.db.Save(&post).Error; saveErr != nil {
			return fmt.Errorf("mark post %d failed after publish error: %w", post.ID, saveErr)
		}
		return nil
	}

	post.Status = "published"
	if err := p.db.Save(&post).Error; err != nil {
		return fmt.Errorf("mark post %d published: %w", post.ID, err)
	}
	return nil
}
