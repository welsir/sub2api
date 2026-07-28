package handler

import (
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func beginOpenAIRequestPerformance(c *gin.Context, handlerStartedAt time.Time) *service.OpenAIRequestPerformanceTrace {
	requestStartedAt := handlerStartedAt
	if c != nil && c.Request != nil {
		if startedAt, ok := middleware2.RequestStartedAt(c.Request.Context()); ok {
			requestStartedAt = startedAt
		}
	}
	trace := service.NewOpenAIRequestPerformanceTrace(requestStartedAt)
	trace.SetAuthDuration(handlerStartedAt.Sub(requestStartedAt))
	service.BindOpenAIRequestPerformanceTrace(c, trace)
	return trace
}

func (h *OpenAIGatewayHandler) logOpenAIRequestPerformance(c *gin.Context, trace *service.OpenAIRequestPerformanceTrace) {
	if c == nil || trace == nil {
		return
	}
	var requestErr error
	if c.Request != nil {
		requestErr = c.Request.Context().Err()
	}
	trace.Finalize(c.Writer.Status(), requestErr)
	snapshot := trace.Snapshot(time.Now())

	fields := []zap.Field{
		zap.String("trace_id", openAIRequestTraceID(c)),
		zap.String("model", snapshot.Model),
		zap.Bool("stream", snapshot.Stream),
		zap.Int("status_code", c.Writer.Status()),
		zap.String("completion_status", snapshot.CompletionStatus),
		zap.Int("attempt_count", snapshot.AttemptCount),
		zap.Int64("auth_ms", snapshot.AuthMs),
		zap.Int64("prepare_ms", snapshot.PrepareMs),
		zap.Int64("user_queue_ms", snapshot.UserQueueMs),
		zap.Int64("billing_ms", snapshot.BillingMs),
		zap.Int64("scheduler_ms", snapshot.SchedulerMs),
		zap.Int64("account_queue_ms", snapshot.AccountQueueMs),
		zap.Int64("upstream_header_ms", snapshot.UpstreamHeaderMs),
		zap.Int64("upstream_body_wait_ms", snapshot.UpstreamBodyWaitMs),
		zap.Int64("stream_ms", snapshot.StreamMs),
		zap.Int64("retry_wait_ms", snapshot.RetryWaitMs),
		zap.Int64("unattributed_ms", snapshot.UnattributedMs),
		zap.Int64("e2e_first_output_ms", snapshot.E2EFirstOutputMs),
		zap.Int64("e2e_first_text_ms", snapshot.E2EFirstTextMs),
		zap.Int64("total_ms", snapshot.TotalMs),
		zap.Int64("connect_ms", snapshot.ConnectMs),
	}
	if snapshot.AccountID > 0 {
		fields = append(fields, zap.Int64("account_id", snapshot.AccountID))
	}
	if snapshot.UpstreamRequestID != "" {
		fields = append(fields, zap.String("upstream_request_id", snapshot.UpstreamRequestID))
	}
	if snapshot.ConnReused != nil {
		fields = append(fields, zap.Bool("conn_reused", *snapshot.ConnReused))
	}
	requestLogger(c, "handler.openai_gateway.responses").Info("openai.request_performance", fields...)
}

func openAIRequestTraceID(c *gin.Context) string {
	if c == nil {
		return ""
	}
	if c.Request != nil {
		if requestID, ok := c.Request.Context().Value(ctxkey.RequestID).(string); ok {
			if requestID = strings.TrimSpace(requestID); requestID != "" {
				return requestID
			}
		}
		if requestID := strings.TrimSpace(c.GetHeader("X-Request-ID")); requestID != "" {
			return requestID
		}
	}
	return strings.TrimSpace(c.Writer.Header().Get("X-Request-ID"))
}
