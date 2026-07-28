package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestLogOpenAIRequestPerformance_EmitsUserVisibleTimingFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	logSink, restore := captureHandlerStructuredLog(t)
	defer restore()

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	req = req.WithContext(context.WithValue(req.Context(), ctxkey.RequestID, "trace-123"))
	c.Request = req
	c.Status(http.StatusOK)

	startedAt := time.Now().Add(-100 * time.Millisecond)
	trace := service.NewOpenAIRequestPerformanceTrace(startedAt)
	trace.SetRequest("gpt-5.5", true)
	trace.SetAccount(42)
	trace.BeginAttempt(startedAt.Add(20 * time.Millisecond))
	firstTokenMs := 30
	trace.RecordForwardResult(
		startedAt.Add(20*time.Millisecond),
		startedAt.Add(80*time.Millisecond),
		&service.OpenAIForwardResult{RequestID: "upstream-456", FirstTokenMs: &firstTokenMs},
		nil,
	)

	h := &OpenAIGatewayHandler{}
	h.logOpenAIRequestPerformance(c, trace)

	require.True(t, logSink.ContainsMessageAtLevel("openai.request_performance", "info"))
	require.True(t, logSink.ContainsFieldValue("trace_id", "trace-123"))
	require.True(t, logSink.ContainsFieldValue("model", "gpt-5.5"))
	require.True(t, logSink.ContainsFieldValue("completion_status", service.OpenAIRequestCompletionCompleted))
	require.True(t, logSink.ContainsFieldValue("upstream_request_id", "upstream-456"))
	require.True(t, logSink.ContainsFieldValue("attempt_count", "1"))
	require.True(t, logSink.ContainsFieldValue("e2e_first_output_ms", "50"))
}
