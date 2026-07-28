package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type firstTextFlushRecorder struct {
	*httptest.ResponseRecorder
	visibleText         string
	firstVisibleFlushAt time.Time
}

func (r *firstTextFlushRecorder) Flush() {
	r.ResponseRecorder.Flush()
	if r.firstVisibleFlushAt.IsZero() && strings.Contains(r.Body.String(), r.visibleText) {
		r.firstVisibleFlushAt = time.Now()
	}
}

func TestOpenAIRequestPerformanceTrace_RecordsMutuallyExclusivePhases(t *testing.T) {
	startedAt := time.Unix(1_000, 0)
	trace := NewOpenAIRequestPerformanceTrace(startedAt)
	trace.SetAuthDuration(5 * time.Millisecond)
	trace.AddPrepareDuration(10 * time.Millisecond)
	trace.AddUserQueueDuration(20 * time.Millisecond)
	trace.AddBillingDuration(5 * time.Millisecond)
	trace.AddPrepareDuration(5 * time.Millisecond)
	trace.AddSchedulerDuration(10 * time.Millisecond)
	trace.AddAccountQueueDuration(10 * time.Millisecond)

	attemptStartedAt := startedAt.Add(65 * time.Millisecond)
	trace.BeginAttempt(attemptStartedAt)
	trace.BeginUpstreamHeader(attemptStartedAt.Add(5 * time.Millisecond))
	trace.EndUpstreamHeader(attemptStartedAt.Add(15 * time.Millisecond))
	trace.MarkFirstOutput(startedAt.Add(100 * time.Millisecond))
	trace.MarkFirstText(startedAt.Add(110 * time.Millisecond))
	trace.EndAttempt(startedAt.Add(130 * time.Millisecond))
	trace.MarkCompleted()

	snapshot := trace.Snapshot(startedAt.Add(135 * time.Millisecond))
	require.Equal(t, int64(5), snapshot.AuthMs)
	require.Equal(t, int64(20), snapshot.PrepareMs)
	require.Equal(t, int64(20), snapshot.UserQueueMs)
	require.Equal(t, int64(5), snapshot.BillingMs)
	require.Equal(t, int64(10), snapshot.SchedulerMs)
	require.Equal(t, int64(10), snapshot.AccountQueueMs)
	require.Equal(t, int64(10), snapshot.UpstreamHeaderMs)
	require.Equal(t, int64(20), snapshot.UpstreamBodyWaitMs)
	require.Equal(t, int64(30), snapshot.StreamMs)
	require.Equal(t, int64(100), snapshot.E2EFirstOutputMs)
	require.Equal(t, int64(110), snapshot.E2EFirstTextMs)
	require.Equal(t, int64(135), snapshot.TotalMs)
	require.Equal(t, int64(5), snapshot.UnattributedMs)
	require.Equal(t, 1, snapshot.AttemptCount)
	require.Equal(t, OpenAIRequestCompletionCompleted, snapshot.CompletionStatus)

	phaseTotal := snapshot.AuthMs + snapshot.PrepareMs + snapshot.UserQueueMs +
		snapshot.BillingMs + snapshot.SchedulerMs + snapshot.AccountQueueMs +
		snapshot.UpstreamHeaderMs + snapshot.UpstreamBodyWaitMs + snapshot.StreamMs +
		snapshot.RetryWaitMs + snapshot.UnattributedMs
	require.Equal(t, snapshot.TotalMs, phaseTotal)
}

func TestOpenAIRequestPerformanceTrace_RecordsConnectionReuseAndDisconnect(t *testing.T) {
	startedAt := time.Unix(2_000, 0)
	trace := NewOpenAIRequestPerformanceTrace(startedAt)
	trace.SetRequest("gpt-5.5", true)
	trace.SetAccount(42)
	trace.BeginAttempt(startedAt.Add(10 * time.Millisecond))
	trace.ClientTrace().GotConn(httptrace.GotConnInfo{Reused: true})
	trace.MarkFirstOutput(startedAt.Add(25 * time.Millisecond))

	firstTokenMs := 15
	trace.RecordForwardResult(
		startedAt.Add(10*time.Millisecond),
		startedAt.Add(40*time.Millisecond),
		&OpenAIForwardResult{
			RequestID:        "upstream-rid",
			FirstTokenMs:     &firstTokenMs,
			ClientDisconnect: true,
		},
		nil,
	)

	snapshot := trace.Snapshot(startedAt.Add(40 * time.Millisecond))
	require.Equal(t, "gpt-5.5", snapshot.Model)
	require.True(t, snapshot.Stream)
	require.Equal(t, int64(42), snapshot.AccountID)
	require.NotNil(t, snapshot.ConnReused)
	require.True(t, *snapshot.ConnReused)
	require.Equal(t, "upstream-rid", snapshot.UpstreamRequestID)
	require.Equal(t, int64(25), snapshot.E2EFirstOutputMs)
	require.Equal(t, OpenAIRequestCompletionClientDisconnected, snapshot.CompletionStatus)
}

func TestOpenAIRequestPerformanceTrace_ClassifiesMissingTerminalAndCanceled(t *testing.T) {
	startedAt := time.Unix(3_000, 0)

	missingTerminal := NewOpenAIRequestPerformanceTrace(startedAt)
	missingTerminal.BeginAttempt(startedAt)
	missingTerminal.MarkMissingTerminal()
	missingTerminal.RecordForwardResult(startedAt, startedAt.Add(time.Millisecond), nil, errors.New("stream usage incomplete"))
	require.Equal(t, OpenAIRequestCompletionMissingTerminal, missingTerminal.Snapshot(startedAt.Add(time.Millisecond)).CompletionStatus)

	canceled := NewOpenAIRequestPerformanceTrace(startedAt)
	canceled.BeginAttempt(startedAt)
	canceled.RecordForwardResult(startedAt, startedAt.Add(time.Millisecond), nil, context.Canceled)
	require.Equal(t, OpenAIRequestCompletionCanceled, canceled.Snapshot(startedAt.Add(time.Millisecond)).CompletionStatus)
}

func TestOpenAIRequestPerformanceTrace_BindsToGinContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	trace := NewOpenAIRequestPerformanceTrace(time.Unix(4_000, 0))

	BindOpenAIRequestPerformanceTrace(c, trace)

	require.Same(t, trace, OpenAIRequestPerformanceTraceFromGin(c))
}

func TestHandleStreamingResponse_MarksFirstTextOnlyAfterFlush(t *testing.T) {
	previousMaxProcs := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(previousMaxProcs)

	gin.SetMode(gin.TestMode)
	recorder := &firstTextFlushRecorder{
		ResponseRecorder: httptest.NewRecorder(),
		visibleText:      `"delta":"hello"`,
	}
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/", nil)

	startedAt := time.Now()
	trace := NewOpenAIRequestPerformanceTrace(startedAt)
	trace.BeginAttempt(startedAt)
	BindOpenAIRequestPerformanceTrace(c, trace)

	lines := []string{
		`data: {"type":"response.output_item.added","item":{"type":"message"}}`,
		`data: {"type":"response.output_text.delta","delta":"hello"}`,
	}
	for range 32 {
		lines = append(lines, ": queued")
	}
	lines = append(lines,
		`data: {"type":"response.completed","response":{"id":"resp_1","status":"completed"}}`,
		"",
	)

	svc := &OpenAIGatewayService{
		cfg: &config.Config{
			Gateway: config.GatewayConfig{
				StreamKeepaliveInterval: 1,
				MaxLineSize:             defaultMaxLineSize,
			},
		},
	}
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(strings.Join(lines, "\n"))),
		Header:     http.Header{},
	}

	_, err := svc.handleStreamingResponse(
		c.Request.Context(),
		resp,
		c,
		&Account{ID: 1},
		startedAt,
		"gpt-5.6-luna",
		"gpt-5.6-luna",
	)

	require.NoError(t, err)
	require.False(t, recorder.firstVisibleFlushAt.IsZero())
	require.False(t, trace.firstTextAt.IsZero())
	require.False(t, trace.firstTextAt.Before(recorder.firstVisibleFlushAt))
}

func TestOpenAIStreamDataContainsVisibleText(t *testing.T) {
	tests := []struct {
		name      string
		eventType string
		payload   string
		want      bool
	}{
		{
			name:      "output item is not visible text",
			eventType: "response.output_item.added",
			payload:   `{"type":"response.output_item.added","item":{"type":"message"}}`,
			want:      false,
		},
		{
			name:      "empty text delta is ignored",
			eventType: "response.output_text.delta",
			payload:   `{"type":"response.output_text.delta","delta":""}`,
			want:      false,
		},
		{
			name:      "output text delta is visible",
			eventType: "response.output_text.delta",
			payload:   `{"type":"response.output_text.delta","delta":"hello"}`,
			want:      true,
		},
		{
			name:      "reasoning summary delta is visible",
			eventType: "response.reasoning_summary_text.delta",
			payload:   `{"type":"response.reasoning_summary_text.delta","delta":"thinking"}`,
			want:      true,
		},
		{
			name:      "function arguments are not user text",
			eventType: "response.function_call_arguments.delta",
			payload:   `{"type":"response.function_call_arguments.delta","delta":"{\"path\":"}`,
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, openAIStreamDataContainsVisibleText([]byte(tt.payload), tt.eventType))
		})
	}
}
