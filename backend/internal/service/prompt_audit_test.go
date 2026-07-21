package service

import (
	"context"
	"errors"
	"testing"
	"time"
)

type promptAuditQueueTestRepository struct {
	records chan UpstreamAuditLog
	err     error
}

func (r *promptAuditQueueTestRepository) Create(_ context.Context, log *UpstreamAuditLog) error {
	if r.records != nil {
		r.records <- *log
	}
	return r.err
}

func TestPromptAuditServiceQueueFullFailsOpen(t *testing.T) {
	svc := newPromptAuditService(&promptAuditQueueTestRepository{}, 1, 0)
	if !svc.RecordUpstream(&UpstreamAuditLog{RequestID: "first"}) {
		t.Fatal("first record should fit in queue")
	}
	if svc.RecordUpstream(&UpstreamAuditLog{RequestID: "second"}) {
		t.Fatal("second record should be dropped without blocking")
	}
}

func TestPromptAuditServiceAbsorbsRepositoryFailure(t *testing.T) {
	repo := &promptAuditQueueTestRepository{records: make(chan UpstreamAuditLog, 1), err: errors.New("database unavailable")}
	svc := newPromptAuditService(repo, 1, 1)
	if !svc.RecordUpstream(&UpstreamAuditLog{RequestID: "req-fail-open"}) {
		t.Fatal("record should enter queue before repository failure")
	}
	select {
	case <-repo.records:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for failed repository write")
	}
}

func TestPromptAuditServiceRawByteBudgetFailsOpen(t *testing.T) {
	svc := newPromptAuditServiceWithLimits(&promptAuditQueueTestRepository{}, 4, 0, 16)
	first := &UpstreamAuditLog{RequestID: "first", requestBodyRaw: make([]byte, 10)}
	second := &UpstreamAuditLog{RequestID: "second", responseBodyRaw: make([]byte, 10)}
	if !svc.RecordUpstream(first) {
		t.Fatal("first record should fit byte budget")
	}
	if svc.RecordUpstream(second) {
		t.Fatal("second record should be dropped when raw byte budget is exhausted")
	}
}

func TestPromptAuditServiceCompressesOnlyInWorker(t *testing.T) {
	repo := &promptAuditQueueTestRepository{records: make(chan UpstreamAuditLog, 1)}
	svc := newPromptAuditService(repo, 2, 1)
	raw := []byte("payload compressed asynchronously")
	log := &UpstreamAuditLog{RequestID: "async", requestBodyRaw: append([]byte(nil), raw...)}
	if !svc.RecordUpstream(log) {
		t.Fatal("record should enter queue")
	}
	select {
	case record := <-repo.records:
		decoded, err := decompressAuditBytes(record.RequestBodyZstd)
		if err != nil || string(decoded) != string(raw) {
			t.Fatalf("worker compression round trip failed: decoded=%q err=%v", decoded, err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for worker compression")
	}
}
