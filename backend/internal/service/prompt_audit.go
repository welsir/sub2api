package service

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"
)

const (
	defaultPromptAuditQueueSize   = 256
	defaultPromptAuditWorkerCount = 2
	promptAuditWriteTimeout       = 5 * time.Second
	defaultPromptAuditQueueBytes  = 64 << 20
)

type UpstreamAuditLog struct {
	ID                      int64
	RequestID               string
	AttemptNo               int
	UserID                  int64
	APIKeyID                int64
	GroupID                 *int64
	AccountID               int64
	Endpoint                string
	Protocol                string
	Model                   string
	Transport               string
	Method                  string
	UpstreamURL             string
	RequestHeadersJSON      string
	RequestContentType      string
	RequestSHA256           string
	RequestBytes            int64
	RequestCompressedBytes  int
	RequestBodyZstd         []byte
	ResponseStatus          int
	ResponseHeadersJSON     string
	ResponseContentType     string
	ResponseSHA256          string
	ResponseBytes           int64
	ResponseCompressedBytes int
	ResponseBodyZstd        []byte
	ResponseComplete        bool
	Outcome                 string
	ErrorMessage            string
	StartedAt               time.Time
	CompletedAt             time.Time
	CreatedAt               time.Time
	requestBodyRaw          []byte
	responseBodyRaw         []byte
}

type PromptAuditRepository interface {
	Create(ctx context.Context, log *UpstreamAuditLog) error
}

type PromptAuditService struct {
	repo           PromptAuditRepository
	queue          chan *UpstreamAuditLog
	maxQueuedBytes int64
	queuedBytes    atomic.Int64
}

func NewPromptAuditService(repo PromptAuditRepository) *PromptAuditService {
	return newPromptAuditService(repo, defaultPromptAuditQueueSize, defaultPromptAuditWorkerCount)
}

func newPromptAuditService(repo PromptAuditRepository, queueSize, workerCount int) *PromptAuditService {
	return newPromptAuditServiceWithLimits(repo, queueSize, workerCount, defaultPromptAuditQueueBytes)
}

func newPromptAuditServiceWithLimits(repo PromptAuditRepository, queueSize, workerCount int, maxQueuedBytes int64) *PromptAuditService {
	if queueSize <= 0 {
		queueSize = 1
	}
	if workerCount < 0 {
		workerCount = 0
	}
	if maxQueuedBytes <= 0 {
		maxQueuedBytes = 1
	}
	svc := &PromptAuditService{repo: repo, queue: make(chan *UpstreamAuditLog, queueSize), maxQueuedBytes: maxQueuedBytes}
	for i := 0; i < workerCount; i++ {
		go svc.worker()
	}
	return svc
}

// RecordUpstream queues a completed attempt without blocking the model request.
func (s *PromptAuditService) RecordUpstream(log *UpstreamAuditLog) bool {
	if s == nil || s.repo == nil || log == nil {
		return false
	}
	payloadBytes := auditQueuedPayloadBytes(log)
	if !s.reserveQueueBytes(payloadBytes) {
		slog.Warn("upstream_audit.byte_budget_full", "request_id", log.RequestID, "attempt_no", log.AttemptNo, "payload_bytes", payloadBytes)
		return false
	}
	select {
	case s.queue <- log:
		return true
	default:
		s.queuedBytes.Add(-payloadBytes)
		slog.Warn("upstream_audit.queue_full", "request_id", log.RequestID, "attempt_no", log.AttemptNo)
		return false
	}
}

func (s *PromptAuditService) worker() {
	for log := range s.queue {
		payloadBytes := auditQueuedPayloadBytes(log)
		if err := prepareUpstreamAuditPayloads(log); err != nil {
			s.queuedBytes.Add(-payloadBytes)
			slog.Warn("upstream_audit.compress_failed", "request_id", log.RequestID, "attempt_no", log.AttemptNo, "error", err)
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), promptAuditWriteTimeout)
		err := s.repo.Create(ctx, log)
		cancel()
		s.queuedBytes.Add(-payloadBytes)
		if err != nil {
			slog.Warn("upstream_audit.persist_failed", "request_id", log.RequestID, "attempt_no", log.AttemptNo, "error", err)
		}
	}
}

func auditQueuedPayloadBytes(log *UpstreamAuditLog) int64 {
	if log == nil {
		return 0
	}
	return int64(len(log.requestBodyRaw) + len(log.responseBodyRaw) + len(log.RequestBodyZstd) + len(log.ResponseBodyZstd))
}

func prepareUpstreamAuditPayloads(log *UpstreamAuditLog) error {
	if log == nil {
		return nil
	}
	if log.requestBodyRaw != nil {
		compressed, err := compressAuditBytes(log.requestBodyRaw)
		if err != nil {
			return fmt.Errorf("compress request: %w", err)
		}
		log.RequestBodyZstd = compressed
		log.RequestCompressedBytes = len(compressed)
		log.requestBodyRaw = nil
	}
	if log.responseBodyRaw != nil {
		compressed, err := compressAuditBytes(log.responseBodyRaw)
		if err != nil {
			return fmt.Errorf("compress response: %w", err)
		}
		log.ResponseBodyZstd = compressed
		log.ResponseCompressedBytes = len(compressed)
		log.responseBodyRaw = nil
	}
	return nil
}

func (s *PromptAuditService) reserveQueueBytes(size int64) bool {
	for {
		current := s.queuedBytes.Load()
		if size > s.maxQueuedBytes-current {
			return false
		}
		if s.queuedBytes.CompareAndSwap(current, current+size) {
			return true
		}
	}
}
