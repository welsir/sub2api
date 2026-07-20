package service

import (
	"context"
	"log/slog"
	"time"
	"unicode/utf8"
)

const (
	defaultPromptAuditQueueSize   = 4096
	defaultPromptAuditWorkerCount = 2
	promptAuditWriteTimeout       = 5 * time.Second
)

type PromptAuditLog struct {
	ID          int64
	RequestID   string
	UserID      int64
	APIKeyID    int64
	GroupID     *int64
	Endpoint    string
	Protocol    string
	Model       string
	PromptText  string
	PromptChars int
	CreatedAt   time.Time
}

type PromptAuditRepository interface {
	Create(ctx context.Context, log *PromptAuditLog) error
}

type PromptAuditInput struct {
	RequestID string
	UserID    int64
	APIKeyID  int64
	GroupID   *int64
	Endpoint  string
	Protocol  string
	Model     string
	Body      []byte
}

type PromptAuditService struct {
	repo  PromptAuditRepository
	queue chan *PromptAuditLog
}

func NewPromptAuditService(repo PromptAuditRepository) *PromptAuditService {
	return newPromptAuditService(repo, defaultPromptAuditQueueSize, defaultPromptAuditWorkerCount)
}

func newPromptAuditService(repo PromptAuditRepository, queueSize, workerCount int) *PromptAuditService {
	if queueSize <= 0 {
		queueSize = 1
	}
	if workerCount < 0 {
		workerCount = 0
	}

	svc := &PromptAuditService{
		repo:  repo,
		queue: make(chan *PromptAuditLog, queueSize),
	}
	for i := 0; i < workerCount; i++ {
		go svc.worker()
	}
	return svc
}

// Record extracts and queues a Prompt audit without blocking the user request.
// False means the request had no eligible user Prompt or the queue was full.
func (s *PromptAuditService) Record(input PromptAuditInput) bool {
	if s == nil || s.repo == nil {
		return false
	}
	prompt := ExtractLatestUserPrompt(input.Protocol, input.Body)
	if prompt == "" {
		return false
	}

	log := &PromptAuditLog{
		RequestID:   input.RequestID,
		UserID:      input.UserID,
		APIKeyID:    input.APIKeyID,
		GroupID:     copyPromptAuditGroupID(input.GroupID),
		Endpoint:    input.Endpoint,
		Protocol:    input.Protocol,
		Model:       input.Model,
		PromptText:  prompt,
		PromptChars: utf8.RuneCountInString(prompt),
	}

	select {
	case s.queue <- log:
		return true
	default:
		slog.Warn("prompt_audit.queue_full", "request_id", input.RequestID)
		return false
	}
}

func (s *PromptAuditService) worker() {
	for log := range s.queue {
		ctx, cancel := context.WithTimeout(context.Background(), promptAuditWriteTimeout)
		err := s.repo.Create(ctx, log)
		cancel()
		if err != nil {
			slog.Warn("prompt_audit.persist_failed", "request_id", log.RequestID, "error", err)
		}
	}
}

func copyPromptAuditGroupID(groupID *int64) *int64 {
	if groupID == nil {
		return nil
	}
	copyID := *groupID
	return &copyID
}
