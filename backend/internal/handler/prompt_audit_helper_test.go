package handler

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type promptAuditHandlerTestRepository struct {
	records chan service.UpstreamAuditLog
}

func (r *promptAuditHandlerTestRepository) Create(_ context.Context, log *service.UpstreamAuditLog) error {
	r.records <- *log
	return nil
}

func promptAuditTestContext(path, requestID string) *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	req := httptest.NewRequest("POST", path, nil)
	req = req.WithContext(context.WithValue(req.Context(), ctxkey.RequestID, requestID))
	c.Request = req
	return c
}

func TestOpenAIGatewayCheckContentModerationAttachesUpstreamAuditMetadata(t *testing.T) {
	repo := &promptAuditHandlerTestRepository{records: make(chan service.UpstreamAuditLog, 1)}
	h := &OpenAIGatewayHandler{promptAuditService: service.NewPromptAuditService(repo)}
	groupID := int64(9)
	apiKey := &service.APIKey{ID: 15, Name: "omni-key", GroupID: &groupID}
	subject := middleware2.AuthSubject{UserID: 11}
	c := promptAuditTestContext("/v1/responses", "req-responses-1")

	decision := h.checkContentModeration(c, nil, apiKey, subject, service.ContentModerationProtocolOpenAIResponses, "gpt-5.6-sol", []byte(`{"input":"inbound payload"}`))
	if decision != nil {
		t.Fatalf("moderation decision = %#v, want nil", decision)
	}

	finalBody := []byte(`{"model":"mapped-model","input":"final upstream payload"}`)
	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, "https://upstream.example/v1/responses", bytes.NewReader(finalBody))
	if err != nil {
		t.Fatal(err)
	}
	capture := service.StartHTTPUpstreamAudit(req, 42)
	if capture == nil {
		t.Fatal("audit context was not attached")
	}
	resp := capture.WrapResponse(&http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader([]byte(`{"id":"resp_1"}`)))})
	_, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()

	select {
	case record := <-repo.records:
		if record.RequestID != "req-responses-1" || record.UserID != 11 || record.APIKeyID != 15 || record.AccountID != 42 {
			t.Fatalf("unexpected identity metadata: %#v", record)
		}
		if record.GroupID == nil || *record.GroupID != 9 || record.Endpoint != "/v1/responses" || record.Protocol != service.ContentModerationProtocolOpenAIResponses || record.Model != "gpt-5.6-sol" {
			t.Fatalf("unexpected route metadata: %#v", record)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for upstream audit")
	}
}

func TestOpenAIGatewayToolContinuationStillGetsUpstreamAuditContext(t *testing.T) {
	repo := &promptAuditHandlerTestRepository{records: make(chan service.UpstreamAuditLog, 1)}
	h := &OpenAIGatewayHandler{promptAuditService: service.NewPromptAuditService(repo)}
	c := promptAuditTestContext("/v1/responses", "req-tool-1")
	h.checkContentModeration(c, nil, &service.APIKey{ID: 3}, middleware2.AuthSubject{UserID: 2}, service.ContentModerationProtocolOpenAIResponses, "gpt-5.6-sol", []byte(`{"input":[{"type":"function_call_output","output":"done"}]}`))
	req, _ := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, "https://upstream.example/v1/responses", bytes.NewReader([]byte(`{"input":[{"type":"function_call_output","output":"done"}]}`)))
	if service.StartHTTPUpstreamAudit(req, 7) == nil {
		t.Fatal("tool continuation must retain complete upstream audit context")
	}
}

func TestContentModerationFailureUsesLocalUnavailableResponse(t *testing.T) {
	decision := service.ContentModerationFailureDecision()

	if !decision.Blocked || decision.Allowed {
		t.Fatalf("failure decision = %#v, want blocked local response", decision)
	}
	if got := contentModerationStatus(decision); got != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", got, http.StatusServiceUnavailable)
	}
	if got := contentModerationErrorCode(decision); got != "content_moderation_unavailable" {
		t.Fatalf("error code = %q, want content_moderation_unavailable", got)
	}
}
