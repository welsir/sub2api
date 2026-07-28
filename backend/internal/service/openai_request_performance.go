package service

import (
	"context"
	"errors"
	"net/http/httptrace"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	OpenAIRequestCompletionCompleted          = "completed"
	OpenAIRequestCompletionClientDisconnected = "client_disconnected"
	OpenAIRequestCompletionUpstreamFailed     = "upstream_failed"
	OpenAIRequestCompletionMissingTerminal    = "missing_terminal"
	OpenAIRequestCompletionCanceled           = "canceled"
	OpenAIRequestCompletionRejected           = "rejected"
)

const openAIRequestPerformanceTraceKey = "openai_request_performance_trace"

type OpenAIRequestPerformanceSnapshot struct {
	Model              string
	Stream             bool
	AccountID          int64
	UpstreamRequestID  string
	AuthMs             int64
	PrepareMs          int64
	UserQueueMs        int64
	BillingMs          int64
	SchedulerMs        int64
	AccountQueueMs     int64
	UpstreamHeaderMs   int64
	UpstreamBodyWaitMs int64
	StreamMs           int64
	RetryWaitMs        int64
	UnattributedMs     int64
	E2EFirstOutputMs   int64
	TotalMs            int64
	ConnectMs          int64
	AttemptCount       int
	CompletionStatus   string
	ConnReused         *bool
}

type OpenAIRequestPerformanceTrace struct {
	mu sync.Mutex

	startedAt time.Time

	model             string
	stream            bool
	accountID         int64
	upstreamRequestID string

	authMs             int64
	prepareMs          int64
	userQueueMs        int64
	billingMs          int64
	schedulerMs        int64
	accountQueueMs     int64
	upstreamHeaderMs   int64
	upstreamBodyWaitMs int64
	streamMs           int64
	retryWaitMs        int64
	connectMs          int64

	attemptCount            int
	attemptStartedAt        time.Time
	attemptReachedUpstream  bool
	upstreamHeaderStartedAt time.Time
	upstreamBodyStartedAt   time.Time
	firstOutputAt           time.Time
	connectStartedAt        time.Time
	completionStatus        string
	missingTerminal         bool
	connReused              *bool
}

func NewOpenAIRequestPerformanceTrace(startedAt time.Time) *OpenAIRequestPerformanceTrace {
	if startedAt.IsZero() {
		startedAt = time.Now()
	}
	return &OpenAIRequestPerformanceTrace{startedAt: startedAt}
}

func BindOpenAIRequestPerformanceTrace(c *gin.Context, trace *OpenAIRequestPerformanceTrace) {
	if c == nil || trace == nil {
		return
	}
	c.Set(openAIRequestPerformanceTraceKey, trace)
}

func OpenAIRequestPerformanceTraceFromGin(c *gin.Context) *OpenAIRequestPerformanceTrace {
	if c == nil {
		return nil
	}
	value, ok := c.Get(openAIRequestPerformanceTraceKey)
	if !ok {
		return nil
	}
	trace, _ := value.(*OpenAIRequestPerformanceTrace)
	return trace
}

func (t *OpenAIRequestPerformanceTrace) SetRequest(model string, stream bool) {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.model = strings.TrimSpace(model)
	t.stream = stream
	t.mu.Unlock()
}

func (t *OpenAIRequestPerformanceTrace) SetAccount(accountID int64) {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.accountID = accountID
	t.mu.Unlock()
}

func durationMilliseconds(duration time.Duration) int64 {
	if duration <= 0 {
		return 0
	}
	return duration.Milliseconds()
}

func (t *OpenAIRequestPerformanceTrace) addDuration(target *int64, duration time.Duration) {
	if t == nil || target == nil {
		return
	}
	*target += durationMilliseconds(duration)
}

func (t *OpenAIRequestPerformanceTrace) SetAuthDuration(duration time.Duration) {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.authMs = durationMilliseconds(duration)
	t.mu.Unlock()
}

func (t *OpenAIRequestPerformanceTrace) AddPrepareDuration(duration time.Duration) {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.addDuration(&t.prepareMs, duration)
	t.mu.Unlock()
}

func (t *OpenAIRequestPerformanceTrace) AddUserQueueDuration(duration time.Duration) {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.addDuration(&t.userQueueMs, duration)
	t.mu.Unlock()
}

func (t *OpenAIRequestPerformanceTrace) AddBillingDuration(duration time.Duration) {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.addDuration(&t.billingMs, duration)
	t.mu.Unlock()
}

func (t *OpenAIRequestPerformanceTrace) AddSchedulerDuration(duration time.Duration) {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.addDuration(&t.schedulerMs, duration)
	t.mu.Unlock()
}

func (t *OpenAIRequestPerformanceTrace) AddAccountQueueDuration(duration time.Duration) {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.addDuration(&t.accountQueueMs, duration)
	t.mu.Unlock()
}

func (t *OpenAIRequestPerformanceTrace) AddRetryWaitDuration(duration time.Duration) {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.addDuration(&t.retryWaitMs, duration)
	t.mu.Unlock()
}

func (t *OpenAIRequestPerformanceTrace) BeginAttempt(startedAt time.Time) {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.attemptCount++
	t.attemptStartedAt = startedAt
	t.attemptReachedUpstream = false
	t.upstreamHeaderStartedAt = time.Time{}
	t.upstreamBodyStartedAt = time.Time{}
	t.firstOutputAt = time.Time{}
	t.connectStartedAt = time.Time{}
	t.completionStatus = ""
	t.missingTerminal = false
	t.connReused = nil
	t.mu.Unlock()
}

func (t *OpenAIRequestPerformanceTrace) BeginUpstreamHeader(startedAt time.Time) {
	if t == nil {
		return
	}
	t.mu.Lock()
	switch {
	case !t.upstreamBodyStartedAt.IsZero():
		t.addDuration(&t.upstreamBodyWaitMs, startedAt.Sub(t.upstreamBodyStartedAt))
		t.upstreamBodyStartedAt = time.Time{}
	case !t.attemptStartedAt.IsZero():
		t.addDuration(&t.prepareMs, startedAt.Sub(t.attemptStartedAt))
	}
	t.attemptReachedUpstream = true
	t.upstreamHeaderStartedAt = startedAt
	t.mu.Unlock()
}

func (t *OpenAIRequestPerformanceTrace) EndUpstreamHeader(endedAt time.Time) {
	if t == nil {
		return
	}
	t.mu.Lock()
	if !t.upstreamHeaderStartedAt.IsZero() {
		t.addDuration(&t.upstreamHeaderMs, endedAt.Sub(t.upstreamHeaderStartedAt))
		t.upstreamHeaderStartedAt = time.Time{}
	}
	t.upstreamBodyStartedAt = endedAt
	t.mu.Unlock()
}

func (t *OpenAIRequestPerformanceTrace) EndUpstreamHeaderWithoutBody(endedAt time.Time) {
	if t == nil {
		return
	}
	t.mu.Lock()
	if !t.upstreamHeaderStartedAt.IsZero() {
		t.addDuration(&t.upstreamHeaderMs, endedAt.Sub(t.upstreamHeaderStartedAt))
		t.upstreamHeaderStartedAt = time.Time{}
	}
	t.upstreamBodyStartedAt = time.Time{}
	t.mu.Unlock()
}

func (t *OpenAIRequestPerformanceTrace) MarkFirstOutput(at time.Time) {
	if t == nil {
		return
	}
	t.mu.Lock()
	if t.firstOutputAt.IsZero() {
		if !t.upstreamBodyStartedAt.IsZero() {
			t.addDuration(&t.upstreamBodyWaitMs, at.Sub(t.upstreamBodyStartedAt))
			t.upstreamBodyStartedAt = time.Time{}
		}
		t.firstOutputAt = at
	}
	t.mu.Unlock()
}

func (t *OpenAIRequestPerformanceTrace) EndAttempt(endedAt time.Time) {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.endAttemptLocked(endedAt)
	t.mu.Unlock()
}

func (t *OpenAIRequestPerformanceTrace) endAttemptLocked(endedAt time.Time) {
	if !t.attemptStartedAt.IsZero() && !t.attemptReachedUpstream {
		t.addDuration(&t.prepareMs, endedAt.Sub(t.attemptStartedAt))
	}
	if !t.upstreamHeaderStartedAt.IsZero() {
		t.addDuration(&t.upstreamHeaderMs, endedAt.Sub(t.upstreamHeaderStartedAt))
		t.upstreamHeaderStartedAt = time.Time{}
	}
	if !t.firstOutputAt.IsZero() {
		t.addDuration(&t.streamMs, endedAt.Sub(t.firstOutputAt))
	} else if !t.upstreamBodyStartedAt.IsZero() {
		t.addDuration(&t.upstreamBodyWaitMs, endedAt.Sub(t.upstreamBodyStartedAt))
	}
	t.upstreamBodyStartedAt = time.Time{}
	t.attemptStartedAt = time.Time{}
	t.attemptReachedUpstream = false
}

func (t *OpenAIRequestPerformanceTrace) MarkCompleted() {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.completionStatus = OpenAIRequestCompletionCompleted
	t.mu.Unlock()
}

func (t *OpenAIRequestPerformanceTrace) MarkMissingTerminal() {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.missingTerminal = true
	t.completionStatus = OpenAIRequestCompletionMissingTerminal
	t.mu.Unlock()
}

func (t *OpenAIRequestPerformanceTrace) RecordForwardResult(startedAt, endedAt time.Time, result *OpenAIForwardResult, err error) {
	if t == nil {
		return
	}

	t.mu.Lock()
	if result != nil {
		t.upstreamRequestID = strings.TrimSpace(result.RequestID)
		if t.firstOutputAt.IsZero() && result.FirstTokenMs != nil && !result.ClientDisconnect {
			firstOutputAt := startedAt.Add(time.Duration(*result.FirstTokenMs) * time.Millisecond)
			if firstOutputAt.After(endedAt) {
				firstOutputAt = endedAt
			}
			if !t.upstreamBodyStartedAt.IsZero() {
				t.addDuration(&t.upstreamBodyWaitMs, firstOutputAt.Sub(t.upstreamBodyStartedAt))
				t.upstreamBodyStartedAt = time.Time{}
			}
			t.firstOutputAt = firstOutputAt
		}
	}
	t.endAttemptLocked(endedAt)

	switch {
	case result != nil && result.ClientDisconnect:
		t.completionStatus = OpenAIRequestCompletionClientDisconnected
	case err == nil:
		t.completionStatus = OpenAIRequestCompletionCompleted
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		t.completionStatus = OpenAIRequestCompletionCanceled
	case t.missingTerminal:
		t.completionStatus = OpenAIRequestCompletionMissingTerminal
	default:
		t.completionStatus = OpenAIRequestCompletionUpstreamFailed
	}
	t.mu.Unlock()
}

func (t *OpenAIRequestPerformanceTrace) Finalize(statusCode int, requestErr error) {
	if t == nil {
		return
	}
	t.mu.Lock()
	if errors.Is(requestErr, context.Canceled) || errors.Is(requestErr, context.DeadlineExceeded) {
		if t.completionStatus != OpenAIRequestCompletionClientDisconnected {
			t.completionStatus = OpenAIRequestCompletionCanceled
		}
		t.mu.Unlock()
		return
	}
	if t.completionStatus == "" {
		switch {
		case t.attemptCount == 0 || statusCode >= 400:
			t.completionStatus = OpenAIRequestCompletionRejected
		default:
			t.completionStatus = OpenAIRequestCompletionCompleted
		}
	}
	t.mu.Unlock()
}

func (t *OpenAIRequestPerformanceTrace) ClientTrace() *httptrace.ClientTrace {
	if t == nil {
		return nil
	}
	return &httptrace.ClientTrace{
		ConnectStart: func(_, _ string) {
			t.mu.Lock()
			t.connectStartedAt = time.Now()
			t.mu.Unlock()
		},
		ConnectDone: func(_, _ string, _ error) {
			now := time.Now()
			t.mu.Lock()
			if !t.connectStartedAt.IsZero() {
				t.addDuration(&t.connectMs, now.Sub(t.connectStartedAt))
				t.connectStartedAt = time.Time{}
			}
			t.mu.Unlock()
		},
		GotConn: func(info httptrace.GotConnInfo) {
			reused := info.Reused
			t.mu.Lock()
			t.connReused = &reused
			t.mu.Unlock()
		},
	}
}

func (t *OpenAIRequestPerformanceTrace) Snapshot(endedAt time.Time) OpenAIRequestPerformanceSnapshot {
	if t == nil {
		return OpenAIRequestPerformanceSnapshot{}
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	totalMs := durationMilliseconds(endedAt.Sub(t.startedAt))
	e2eFirstOutputMs := int64(0)
	if !t.firstOutputAt.IsZero() {
		e2eFirstOutputMs = durationMilliseconds(t.firstOutputAt.Sub(t.startedAt))
	}
	phaseTotal := t.authMs + t.prepareMs + t.userQueueMs + t.billingMs +
		t.schedulerMs + t.accountQueueMs + t.upstreamHeaderMs +
		t.upstreamBodyWaitMs + t.streamMs + t.retryWaitMs
	unattributedMs := totalMs - phaseTotal
	if unattributedMs < 0 {
		unattributedMs = 0
	}

	var connReused *bool
	if t.connReused != nil {
		value := *t.connReused
		connReused = &value
	}
	return OpenAIRequestPerformanceSnapshot{
		Model:              t.model,
		Stream:             t.stream,
		AccountID:          t.accountID,
		UpstreamRequestID:  t.upstreamRequestID,
		AuthMs:             t.authMs,
		PrepareMs:          t.prepareMs,
		UserQueueMs:        t.userQueueMs,
		BillingMs:          t.billingMs,
		SchedulerMs:        t.schedulerMs,
		AccountQueueMs:     t.accountQueueMs,
		UpstreamHeaderMs:   t.upstreamHeaderMs,
		UpstreamBodyWaitMs: t.upstreamBodyWaitMs,
		StreamMs:           t.streamMs,
		RetryWaitMs:        t.retryWaitMs,
		UnattributedMs:     unattributedMs,
		E2EFirstOutputMs:   e2eFirstOutputMs,
		TotalMs:            totalMs,
		ConnectMs:          t.connectMs,
		AttemptCount:       t.attemptCount,
		CompletionStatus:   t.completionStatus,
		ConnReused:         connReused,
	}
}
