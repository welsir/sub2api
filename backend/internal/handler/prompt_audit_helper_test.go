package handler

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type promptAuditHandlerTestRepository struct {
	records chan service.PromptAuditLog
	err     error
}

func (r *promptAuditHandlerTestRepository) Create(_ context.Context, log *service.PromptAuditLog) error {
	copyLog := *log
	if log.GroupID != nil {
		groupID := *log.GroupID
		copyLog.GroupID = &groupID
	}
	select {
	case r.records <- copyLog:
	default:
	}
	return r.err
}

func promptAuditTestContext(path, requestID string) *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	req := httptest.NewRequest("POST", path, nil)
	req = req.WithContext(context.WithValue(req.Context(), ctxkey.RequestID, requestID))
	c.Request = req
	return c
}

func receivePromptAuditRecord(t *testing.T, records <-chan service.PromptAuditLog) service.PromptAuditLog {
	t.Helper()
	select {
	case record := <-records:
		return record
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for prompt audit record")
		return service.PromptAuditLog{}
	}
}

func TestOpenAIGatewayCheckContentModerationRecordsPromptAudit(t *testing.T) {
	repo := &promptAuditHandlerTestRepository{records: make(chan service.PromptAuditLog, 1)}
	h := &OpenAIGatewayHandler{promptAuditService: service.NewPromptAuditService(repo)}
	groupID := int64(9)
	apiKey := &service.APIKey{ID: 15, Name: "omni-key", GroupID: &groupID}
	subject := middleware2.AuthSubject{UserID: 11}
	c := promptAuditTestContext("/v1/responses", "req-responses-1")

	decision := h.checkContentModeration(
		c,
		nil,
		apiKey,
		subject,
		service.ContentModerationProtocolOpenAIResponses,
		"gpt-5.6-sol",
		[]byte(`{"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"second prompt"}]}]}`),
	)
	if decision != nil {
		t.Fatalf("moderation decision = %#v, want nil when moderation is not configured", decision)
	}

	record := receivePromptAuditRecord(t, repo.records)
	if record.RequestID != "req-responses-1" || record.UserID != 11 || record.APIKeyID != 15 {
		t.Fatalf("unexpected identity metadata: %#v", record)
	}
	if record.GroupID == nil || *record.GroupID != 9 {
		t.Fatalf("group id = %v, want 9", record.GroupID)
	}
	if record.Endpoint != "/v1/responses" || record.Protocol != service.ContentModerationProtocolOpenAIResponses || record.Model != "gpt-5.6-sol" {
		t.Fatalf("unexpected request metadata: %#v", record)
	}
	if record.PromptText != "second prompt" {
		t.Fatalf("prompt = %q, want second prompt", record.PromptText)
	}
}

func TestGatewayCheckContentModerationRecordsChatPromptAudit(t *testing.T) {
	repo := &promptAuditHandlerTestRepository{records: make(chan service.PromptAuditLog, 1)}
	h := &GatewayHandler{promptAuditService: service.NewPromptAuditService(repo)}
	c := promptAuditTestContext("/v1/chat/completions", "req-chat-1")

	h.checkContentModeration(
		c,
		nil,
		&service.APIKey{ID: 3},
		middleware2.AuthSubject{UserID: 2},
		service.ContentModerationProtocolOpenAIChat,
		"gpt-5.6-sol",
		[]byte(`{"messages":[{"role":"user","content":"first"},{"role":"assistant","content":"answer"},{"role":"user","content":"latest"}]}`),
	)

	record := receivePromptAuditRecord(t, repo.records)
	if record.PromptText != "latest" || record.Endpoint != "/v1/chat/completions" {
		t.Fatalf("unexpected audit record: %#v", record)
	}
}

func TestOpenAIGatewayCheckContentModerationSkipsToolContinuationPromptAudit(t *testing.T) {
	repo := &promptAuditHandlerTestRepository{records: make(chan service.PromptAuditLog, 1)}
	h := &OpenAIGatewayHandler{promptAuditService: service.NewPromptAuditService(repo)}
	c := promptAuditTestContext("/v1/responses", "req-tool-1")

	h.checkContentModeration(
		c,
		nil,
		&service.APIKey{ID: 3},
		middleware2.AuthSubject{UserID: 2},
		service.ContentModerationProtocolOpenAIResponses,
		"gpt-5.6-sol",
		[]byte(`{"input":[{"type":"function_call_output","call_id":"call_1","output":"done"}]}`),
	)

	select {
	case record := <-repo.records:
		t.Fatalf("unexpected audit record for tool continuation: %#v", record)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestOpenAIGatewayPromptAuditRepositoryFailureDoesNotChangeRequestFlow(t *testing.T) {
	repo := &promptAuditHandlerTestRepository{records: make(chan service.PromptAuditLog, 1), err: errors.New("database unavailable")}
	h := &OpenAIGatewayHandler{promptAuditService: service.NewPromptAuditService(repo)}
	c := promptAuditTestContext("/v1/responses", "req-fail-open")

	decision := h.checkContentModeration(
		c,
		nil,
		&service.APIKey{ID: 3},
		middleware2.AuthSubject{UserID: 2},
		service.ContentModerationProtocolOpenAIResponses,
		"gpt-5.6-sol",
		[]byte(`{"input":"keep serving"}`),
	)
	if decision != nil {
		t.Fatalf("moderation decision = %#v, want nil", decision)
	}
	receivePromptAuditRecord(t, repo.records)
}
