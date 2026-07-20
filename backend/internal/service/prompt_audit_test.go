package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type promptAuditTestRepository struct {
	mu      sync.Mutex
	records []*PromptAuditLog
	err     error
	called  chan struct{}
}

func (r *promptAuditTestRepository) Create(_ context.Context, log *PromptAuditLog) error {
	r.mu.Lock()
	copyLog := *log
	if log.GroupID != nil {
		groupID := *log.GroupID
		copyLog.GroupID = &groupID
	}
	r.records = append(r.records, &copyLog)
	r.mu.Unlock()
	if r.called != nil {
		select {
		case r.called <- struct{}{}:
		default:
		}
	}
	return r.err
}

func (r *promptAuditTestRepository) snapshot() []*PromptAuditLog {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]*PromptAuditLog(nil), r.records...)
}

func TestPromptAuditServiceRecordsLatestUserPrompt(t *testing.T) {
	repo := &promptAuditTestRepository{called: make(chan struct{}, 1)}
	svc := newPromptAuditService(repo, 2, 1)
	groupID := int64(9)

	accepted := svc.Record(PromptAuditInput{
		RequestID: "req-1",
		UserID:    11,
		APIKeyID:  15,
		GroupID:   &groupID,
		Endpoint:  "/v1/responses",
		Protocol:  ContentModerationProtocolOpenAIResponses,
		Model:     "gpt-5.6-sol",
		Body:      []byte(`{"input":"你好，audit"}`),
	})
	if !accepted {
		t.Fatal("Record() rejected an eligible prompt")
	}

	select {
	case <-repo.called:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for audit persistence")
	}

	records := repo.snapshot()
	if len(records) != 1 {
		t.Fatalf("persisted %d records, want 1", len(records))
	}
	got := records[0]
	if got.PromptText != "你好，audit" || got.PromptChars != 8 {
		t.Fatalf("prompt = %q (%d chars), want %q (8 chars)", got.PromptText, got.PromptChars, "你好，audit")
	}
	if got.RequestID != "req-1" || got.UserID != 11 || got.APIKeyID != 15 || got.GroupID == nil || *got.GroupID != 9 {
		t.Fatalf("unexpected audit metadata: %#v", got)
	}
}

func TestPromptAuditServiceSkipsToolContinuation(t *testing.T) {
	svc := newPromptAuditService(&promptAuditTestRepository{}, 1, 0)
	accepted := svc.Record(PromptAuditInput{
		Protocol: ContentModerationProtocolOpenAIResponses,
		Body:     []byte(`{"input":[{"type":"function_call_output","output":"done"}]}`),
	})
	if accepted {
		t.Fatal("Record() accepted a tool-only continuation")
	}
	if len(svc.queue) != 0 {
		t.Fatalf("queue length = %d, want 0", len(svc.queue))
	}
}

func TestPromptAuditServiceQueueFullFailsOpen(t *testing.T) {
	svc := newPromptAuditService(&promptAuditTestRepository{}, 1, 0)
	input := PromptAuditInput{Protocol: ContentModerationProtocolOpenAIResponses, Body: []byte(`{"input":"prompt"}`)}
	if !svc.Record(input) {
		t.Fatal("first Record() should fit in queue")
	}
	if svc.Record(input) {
		t.Fatal("second Record() should be dropped when queue is full")
	}
}

func TestPromptAuditServiceCopiesRequestMetadataBeforeQueueing(t *testing.T) {
	svc := newPromptAuditService(&promptAuditTestRepository{}, 1, 0)
	groupID := int64(3)
	body := []byte(`{"input":"original"}`)
	input := PromptAuditInput{RequestID: "req-copy", GroupID: &groupID, Protocol: ContentModerationProtocolOpenAIResponses, Body: body}

	if !svc.Record(input) {
		t.Fatal("Record() rejected prompt")
	}
	groupID = 99
	copy(body, []byte(`{"input":"changed!"}`))

	queued := <-svc.queue
	if queued.PromptText != "original" {
		t.Fatalf("queued prompt = %q, want original", queued.PromptText)
	}
	if queued.GroupID == nil || *queued.GroupID != 3 {
		t.Fatalf("queued group = %v, want 3", queued.GroupID)
	}
}

func TestPromptAuditServiceAbsorbsRepositoryFailure(t *testing.T) {
	repo := &promptAuditTestRepository{err: errors.New("database unavailable"), called: make(chan struct{}, 1)}
	svc := newPromptAuditService(repo, 1, 1)
	if !svc.Record(PromptAuditInput{RequestID: "req-fail-open", Protocol: ContentModerationProtocolOpenAIResponses, Body: []byte(`{"input":"prompt"}`)}) {
		t.Fatal("Record() should accept the item before asynchronous repository failure")
	}

	select {
	case <-repo.called:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for failed repository call")
	}
}
