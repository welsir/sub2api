package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type upstreamAuditTestRepository struct {
	records chan UpstreamAuditLog
	err     error
}

func (r *upstreamAuditTestRepository) Create(_ context.Context, log *UpstreamAuditLog) error {
	copyLog := *log
	copyLog.RequestBodyZstd = append([]byte(nil), log.RequestBodyZstd...)
	copyLog.ResponseBodyZstd = append([]byte(nil), log.ResponseBodyZstd...)
	r.records <- copyLog
	return r.err
}

func receiveUpstreamAudit(t *testing.T, records <-chan UpstreamAuditLog) UpstreamAuditLog {
	t.Helper()
	select {
	case record := <-records:
		return record
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for upstream audit")
		return UpstreamAuditLog{}
	}
}

func mustDecompressAuditBytes(t *testing.T, compressed []byte) []byte {
	t.Helper()
	decoded, err := decompressAuditBytes(compressed)
	if err != nil {
		t.Fatal(err)
	}
	return decoded
}

func auditContextForTest(t *testing.T, svc *PromptAuditService) context.Context {
	t.Helper()
	groupID := int64(9)
	return WithUpstreamAuditContext(context.Background(), svc, UpstreamAuditMetadata{
		RequestID: "req-1",
		UserID:    11,
		APIKeyID:  15,
		GroupID:   &groupID,
		Endpoint:  "/v1/responses",
		Protocol:  ContentModerationProtocolOpenAIResponses,
		Model:     "gpt-5.6-sol",
	})
}

func TestHTTPUpstreamAuditCapturesExactRequestAndRawSSEResponse(t *testing.T) {
	repo := &upstreamAuditTestRepository{records: make(chan UpstreamAuditLog, 1)}
	svc := newPromptAuditService(repo, 4, 1)
	requestBody := []byte(`{"model":"gpt-5.6-sol","input":"final upstream payload"}`)
	responseBody := []byte("event: response.output_text.delta\ndata: {\"delta\":\"hello\"}\n\ndata: [DONE]\n\n")
	req, err := http.NewRequestWithContext(
		auditContextForTest(t, svc),
		http.MethodPost,
		"https://upstream.example/v1/responses?api_key=must-not-persist",
		bytes.NewReader(requestBody),
	)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer must-not-persist")

	capture := StartHTTPUpstreamAudit(req, 42)
	if capture == nil {
		t.Fatal("expected audit capture")
	}
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"text/event-stream"},
			"Set-Cookie":   []string{"secret=cookie"},
			"X-Request-Id": []string{"upstream-req-1"},
		},
		Body: io.NopCloser(bytes.NewReader(responseBody)),
	}
	resp = capture.WrapResponse(resp)
	gotBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(responseBody, gotBody) {
		t.Fatalf("forwarded body changed: %q", gotBody)
	}

	record := receiveUpstreamAudit(t, repo.records)
	if record.AttemptNo != 1 || record.AccountID != 42 || record.Transport != "http" {
		t.Fatalf("unexpected attempt metadata: %#v", record)
	}
	if record.UpstreamURL != "https://upstream.example/v1/responses" {
		t.Fatalf("upstream URL = %q", record.UpstreamURL)
	}
	if record.RequestHeadersJSON != `{"content-type":["application/json"]}` {
		t.Fatalf("request headers = %s", record.RequestHeadersJSON)
	}
	if record.ResponseHeadersJSON != `{"content-type":["text/event-stream"],"x-request-id":["upstream-req-1"]}` {
		t.Fatalf("response headers = %s", record.ResponseHeadersJSON)
	}
	if got := mustDecompressAuditBytes(t, record.RequestBodyZstd); !bytes.Equal(got, requestBody) {
		t.Fatalf("request round trip = %q", got)
	}
	if got := mustDecompressAuditBytes(t, record.ResponseBodyZstd); !bytes.Equal(got, responseBody) {
		t.Fatalf("response round trip = %q", got)
	}
	if record.RequestSHA256 != auditSHA256(requestBody) || record.ResponseSHA256 != auditSHA256(responseBody) {
		t.Fatalf("unexpected hashes: %#v", record)
	}
	if record.RequestBytes != int64(len(requestBody)) || record.ResponseBytes != int64(len(responseBody)) {
		t.Fatalf("unexpected raw sizes: %#v", record)
	}
	if record.RequestCompressedBytes != len(record.RequestBodyZstd) || record.ResponseCompressedBytes != len(record.ResponseBodyZstd) {
		t.Fatalf("unexpected compressed sizes: %#v", record)
	}
	if record.Outcome != UpstreamAuditOutcomeCompleted || !record.ResponseComplete {
		t.Fatalf("unexpected outcome: %#v", record)
	}
}

func TestHTTPUpstreamAuditRecordsIndependentTransportFailureAttempts(t *testing.T) {
	repo := &upstreamAuditTestRepository{records: make(chan UpstreamAuditLog, 2)}
	svc := newPromptAuditService(repo, 4, 1)
	ctx := auditContextForTest(t, svc)

	for i := 0; i < 2; i++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://upstream.example/v1/responses", bytes.NewReader([]byte(`{"input":"retry"}`)))
		if err != nil {
			t.Fatal(err)
		}
		capture := StartHTTPUpstreamAudit(req, int64(50+i))
		capture.RecordTransportError(errors.New("upstream EOF"))
	}

	first := receiveUpstreamAudit(t, repo.records)
	second := receiveUpstreamAudit(t, repo.records)
	if first.AttemptNo != 1 || second.AttemptNo != 2 {
		t.Fatalf("attempts = %d, %d", first.AttemptNo, second.AttemptNo)
	}
	if first.Outcome != UpstreamAuditOutcomeTransportError || first.ErrorMessage != "upstream EOF" {
		t.Fatalf("unexpected failure record: %#v", first)
	}
}

func TestHTTPUpstreamAuditRedactsSecretsFromTransportErrors(t *testing.T) {
	repo := &upstreamAuditTestRepository{records: make(chan UpstreamAuditLog, 1)}
	svc := newPromptAuditService(repo, 2, 1)
	req, _ := http.NewRequestWithContext(auditContextForTest(t, svc), http.MethodPost, "https://upstream.example/v1/responses", bytes.NewReader([]byte(`{}`)))
	capture := StartHTTPUpstreamAudit(req, 7)
	capture.RecordTransportError(errors.New(`Post "https://upstream.example/v1/responses?api_key=secret&sig=also-secret": EOF token=third-secret`))

	record := receiveUpstreamAudit(t, repo.records)
	if strings.Contains(record.ErrorMessage, "secret") || strings.Contains(record.ErrorMessage, "api_key") || strings.Contains(record.ErrorMessage, "sig=") {
		t.Fatalf("transport error leaked credentials: %q", record.ErrorMessage)
	}
	if !strings.Contains(record.ErrorMessage, "EOF") {
		t.Fatalf("transport error lost useful cause: %q", record.ErrorMessage)
	}
}

func TestHTTPUpstreamAuditCloseBeforeEOFPersistsReadPrefixAsIncomplete(t *testing.T) {
	repo := &upstreamAuditTestRepository{records: make(chan UpstreamAuditLog, 1)}
	svc := newPromptAuditService(repo, 2, 1)
	req, _ := http.NewRequestWithContext(auditContextForTest(t, svc), http.MethodPost, "https://upstream.example/v1/responses", bytes.NewReader([]byte(`{}`)))
	capture := StartHTTPUpstreamAudit(req, 7)
	resp := capture.WrapResponse(&http.Response{StatusCode: 500, Body: io.NopCloser(bytes.NewReader([]byte("complete-error-body")))})
	prefix := make([]byte, 8)
	_, _ = resp.Body.Read(prefix)
	_ = resp.Body.Close()

	record := receiveUpstreamAudit(t, repo.records)
	if record.Outcome != UpstreamAuditOutcomeClosed || record.ResponseComplete {
		t.Fatalf("unexpected close outcome: %#v", record)
	}
	if got := mustDecompressAuditBytes(t, record.ResponseBodyZstd); !bytes.Equal(got, prefix) {
		t.Fatalf("captured prefix = %q, want %q", got, prefix)
	}
}

func TestHTTPUpstreamAuditDoesNotConsumeNonReplayableRequestBody(t *testing.T) {
	repo := &upstreamAuditTestRepository{records: make(chan UpstreamAuditLog, 1)}
	svc := newPromptAuditService(repo, 2, 1)
	body := io.NopCloser(strings.NewReader("non-replayable payload"))
	req, _ := http.NewRequestWithContext(auditContextForTest(t, svc), http.MethodPost, "https://upstream.example/v1/responses", body)
	req.GetBody = nil
	capture := StartHTTPUpstreamAudit(req, 7)
	forwarded, err := io.ReadAll(req.Body)
	if err != nil || string(forwarded) != "non-replayable payload" {
		t.Fatalf("audit changed request body: body=%q err=%v", forwarded, err)
	}
	capture.RecordTransportError(errors.New("upstream unavailable"))
	record := receiveUpstreamAudit(t, repo.records)
	if record.Outcome != "audit_error" || record.ErrorMessage != "request body is not replayable" {
		t.Fatalf("unexpected fail-open record: %#v", record)
	}
}

func TestWebSocketUpstreamAuditPreservesFinalPayloadAndRawMessageBoundaries(t *testing.T) {
	repo := &upstreamAuditTestRepository{records: make(chan UpstreamAuditLog, 1)}
	svc := newPromptAuditService(repo, 2, 1)
	ctx := auditContextForTest(t, svc)
	requestPayload := []byte(`{"type":"response.create","input":"final"}`)
	first := []byte(`{"type":"response.output_text.delta","delta":"hello"}`)
	second := []byte("{\n  \"type\": \"response.completed\"\n}")

	capture := StartWebSocketUpstreamAudit(ctx, 88, "wss://chatgpt.example/backend-api/codex/responses?token=secret", requestPayload)
	if capture == nil {
		t.Fatal("expected websocket capture")
	}
	capture.AppendWebSocketMessage(first)
	capture.AppendWebSocketMessage(second)
	capture.Finish(UpstreamAuditOutcomeCompleted, true, nil)

	record := receiveUpstreamAudit(t, repo.records)
	if record.Transport != "websocket" || record.AccountID != 88 || record.UpstreamURL != "wss://chatgpt.example/backend-api/codex/responses" {
		t.Fatalf("unexpected websocket metadata: %#v", record)
	}
	if got := mustDecompressAuditBytes(t, record.RequestBodyZstd); !bytes.Equal(got, requestPayload) {
		t.Fatalf("request = %q", got)
	}
	wantFrames := append([]byte(`"{\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}"`+"\n"), []byte(`"{\n  \"type\": \"response.completed\"\n}"`+"\n")...)
	if got := mustDecompressAuditBytes(t, record.ResponseBodyZstd); !bytes.Equal(got, wantFrames) {
		t.Fatalf("frames = %q, want %q", got, wantFrames)
	}
}

func TestUpstreamAuditActiveCaptureBudgetRejectsExcessWithoutBlocking(t *testing.T) {
	upstreamAuditActiveBytes.Store(maxUpstreamAuditActiveBytes - 4)
	t.Cleanup(func() { upstreamAuditActiveBytes.Store(0) })
	if tryReserveUpstreamAuditBytes(5) {
		t.Fatal("reservation exceeding global capture budget must fail open")
	}
	if !tryReserveUpstreamAuditBytes(4) {
		t.Fatal("reservation at remaining budget should succeed")
	}
	releaseUpstreamAuditBytes(4)
}
