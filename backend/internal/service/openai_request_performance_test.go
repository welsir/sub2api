package service

import (
	"context"
	"errors"
	"net/http/httptest"
	"net/http/httptrace"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

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
