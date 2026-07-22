package pool

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

type FailureClass string

const (
	FailureNone      FailureClass = "none"
	FailureTransport FailureClass = "transport"
)

type ProbeResult struct {
	Success    bool
	Reachable  bool
	HTTPStatus int
	Duration   time.Duration
	Failure    FailureClass
	Message    string
	Err        error
}

func ProbeHTTP(ctx context.Context, client *http.Client, target string) ProbeResult {
	started := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, nil)
	if err != nil {
		return failedProbe(started, err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return failedProbe(started, err)
	}
	defer resp.Body.Close()
	_, readErr := io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	if readErr != nil {
		return failedProbe(started, readErr)
	}
	result := ProbeResult{
		Reachable:  true,
		HTTPStatus: resp.StatusCode,
		Duration:   time.Since(started),
		Failure:    FailureNone,
	}
	if resp.StatusCode >= 500 {
		result.Failure = FailureTransport
		result.Message = fmt.Sprintf("upstream returned HTTP %d", resp.StatusCode)
		return result
	}
	result.Success = true
	return result
}

func failedProbe(started time.Time, err error) ProbeResult {
	return ProbeResult{
		Duration: time.Since(started),
		Failure:  FailureTransport,
		Message:  err.Error(),
		Err:      err,
	}
}
