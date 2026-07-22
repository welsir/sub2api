package pool

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProbeTreatsAnonymous401AsReachable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"missing token"}`))
	}))
	defer server.Close()

	result := ProbeHTTP(context.Background(), server.Client(), server.URL)
	if !result.Reachable || !result.Success || result.HTTPStatus != http.StatusUnauthorized {
		t.Fatalf("result = %+v", result)
	}
}

func TestProbeTreatsGatewayErrorsAsTransportFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()

	result := ProbeHTTP(context.Background(), server.Client(), server.URL)
	if result.Success || result.Failure != FailureTransport {
		t.Fatalf("result = %+v", result)
	}
}

func TestProbeTreatsEOFDuringRequestAsTransportFailure(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, io.ErrUnexpectedEOF
	})}
	result := ProbeHTTP(context.Background(), client, "https://chatgpt.com/backend-api/codex/responses")
	if result.Success || result.Failure != FailureTransport || !strings.Contains(result.Message, "EOF") {
		t.Fatalf("result = %+v", result)
	}
}

func TestProbeTreatsContextTimeoutAsTransportFailure(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, context.DeadlineExceeded
	})}
	result := ProbeHTTP(context.Background(), client, "https://chatgpt.com/backend-api/codex/responses")
	if result.Success || result.Failure != FailureTransport || !errors.Is(result.Err, context.DeadlineExceeded) {
		t.Fatalf("result = %+v", result)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return fn(req) }
