package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"hash"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/klauspost/compress/zstd"
)

const (
	UpstreamAuditOutcomeCompleted       = "completed"
	UpstreamAuditOutcomeTransportError  = "transport_error"
	UpstreamAuditOutcomeReadError       = "read_error"
	UpstreamAuditOutcomeClosed          = "closed"
	UpstreamAuditOutcomeCaptureOverflow = "capture_overflow"
	UpstreamAuditOutcomeUpstreamError   = "upstream_error"
	defaultUpstreamAuditMaxBodyBytes    = 16 << 20
	maxUpstreamAuditActiveBytes         = 64 << 20
	maxUpstreamAuditErrorBytes          = 2048
)

var upstreamAuditActiveBytes atomic.Int64

var (
	auditErrorURLPattern    = regexp.MustCompile(`(?i)(?:https?|wss?)://[^\s"']+`)
	auditErrorSecretPattern = regexp.MustCompile(`(?i)(authorization|cookie|api[-_]?key|access[-_]?token|refresh[-_]?token|token|key)(\s*[:=]\s*)([^\s,;&]+)`)
)

type UpstreamAuditMetadata struct {
	RequestID string
	UserID    int64
	APIKeyID  int64
	GroupID   *int64
	Endpoint  string
	Protocol  string
	Model     string
}

type upstreamAuditContext struct {
	service  *PromptAuditService
	metadata UpstreamAuditMetadata
	attempts atomic.Int64
}

type upstreamAuditContextKey struct{}

func WithUpstreamAuditContext(ctx context.Context, svc *PromptAuditService, metadata UpstreamAuditMetadata) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if metadata.GroupID != nil {
		groupID := *metadata.GroupID
		metadata.GroupID = &groupID
	}
	return context.WithValue(ctx, upstreamAuditContextKey{}, &upstreamAuditContext{service: svc, metadata: metadata})
}

type UpstreamAuditCapture struct {
	service          *PromptAuditService
	log              *UpstreamAuditLog
	response         bytes.Buffer
	responseHash     hash.Hash
	overflow         bool
	requestReserved  int64
	responseReserved int64
	finalized        atomic.Bool
	mu               sync.Mutex
}

func StartHTTPUpstreamAudit(req *http.Request, accountID int64) *UpstreamAuditCapture {
	if req == nil {
		return nil
	}
	state, _ := req.Context().Value(upstreamAuditContextKey{}).(*upstreamAuditContext)
	if state == nil || state.service == nil {
		return nil
	}
	startedAt := time.Now()
	metadata := state.metadata
	log := &UpstreamAuditLog{
		RequestID:          metadata.RequestID,
		AttemptNo:          int(state.attempts.Add(1)),
		UserID:             metadata.UserID,
		APIKeyID:           metadata.APIKeyID,
		GroupID:            copyAuditGroupID(metadata.GroupID),
		AccountID:          accountID,
		Endpoint:           metadata.Endpoint,
		Protocol:           metadata.Protocol,
		Model:              metadata.Model,
		Transport:          "http",
		Method:             req.Method,
		UpstreamURL:        sanitizeAuditURL(req.URL),
		RequestHeadersJSON: sanitizedAuditHeaders(req.Header, true),
		RequestContentType: req.Header.Get("Content-Type"),
		StartedAt:          startedAt,
	}
	captureRequestBody(req, log)
	capture := &UpstreamAuditCapture{service: state.service, log: log, responseHash: sha256.New()}
	capture.reserveRequestPayload()
	return capture
}

func StartWebSocketUpstreamAudit(ctx context.Context, accountID int64, upstreamURL string, requestPayload []byte) *UpstreamAuditCapture {
	if ctx == nil {
		return nil
	}
	state, _ := ctx.Value(upstreamAuditContextKey{}).(*upstreamAuditContext)
	if state == nil || state.service == nil {
		return nil
	}
	metadata := state.metadata
	startedAt := time.Now()
	parsedURL, _ := url.Parse(upstreamURL)
	log := &UpstreamAuditLog{
		RequestID:           metadata.RequestID,
		AttemptNo:           int(state.attempts.Add(1)),
		UserID:              metadata.UserID,
		APIKeyID:            metadata.APIKeyID,
		GroupID:             copyAuditGroupID(metadata.GroupID),
		AccountID:           accountID,
		Endpoint:            metadata.Endpoint,
		Protocol:            metadata.Protocol,
		Model:               metadata.Model,
		Transport:           "websocket",
		Method:              "WS",
		UpstreamURL:         sanitizeAuditURL(parsedURL),
		RequestHeadersJSON:  "{}",
		RequestContentType:  "application/json",
		ResponseHeadersJSON: "{}",
		ResponseContentType: "application/x-sub2api-ws-jsonl",
		ResponseStatus:      http.StatusSwitchingProtocols,
		StartedAt:           startedAt,
	}
	captureRequestBytes(requestPayload, log)
	capture := &UpstreamAuditCapture{service: state.service, log: log, responseHash: sha256.New()}
	capture.reserveRequestPayload()
	return capture
}

func (c *UpstreamAuditCapture) AppendWebSocketMessage(message []byte) {
	if c == nil || c.finalized.Load() {
		return
	}
	encoded, err := json.Marshal(string(message))
	if err != nil {
		c.Finish("audit_error", false, err)
		return
	}
	encoded = append(encoded, '\n')
	c.appendResponse(encoded)
}

func (c *UpstreamAuditCapture) Finish(outcome string, complete bool, err error) {
	if c == nil {
		return
	}
	c.finalize(outcome, complete, err)
}

func (c *UpstreamAuditCapture) CloseIfPending() {
	if c == nil {
		return
	}
	c.finalize(UpstreamAuditOutcomeClosed, false, nil)
}

func (c *UpstreamAuditCapture) RecordTransportError(err error) {
	if c == nil {
		return
	}
	c.finalize(UpstreamAuditOutcomeTransportError, false, err)
}

func (c *UpstreamAuditCapture) WrapResponse(resp *http.Response) *http.Response {
	if c == nil || resp == nil || resp.Body == nil {
		return resp
	}
	c.log.ResponseStatus = resp.StatusCode
	c.log.ResponseHeadersJSON = sanitizedAuditHeaders(resp.Header, false)
	c.log.ResponseContentType = resp.Header.Get("Content-Type")
	resp.Body = &upstreamAuditBody{ReadCloser: resp.Body, capture: c}
	return resp
}

func (c *UpstreamAuditCapture) appendResponse(p []byte) {
	if c == nil || len(p) == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.finalized.Load() {
		return
	}
	c.log.ResponseBytes += int64(len(p))
	_, _ = c.responseHash.Write(p)
	if c.overflow {
		return
	}
	if c.response.Len()+len(p) > defaultUpstreamAuditMaxBodyBytes {
		c.overflow = true
		releaseUpstreamAuditBytes(c.responseReserved)
		c.responseReserved = 0
		c.response.Reset()
		return
	}
	if !tryReserveUpstreamAuditBytes(int64(len(p))) {
		c.overflow = true
		releaseUpstreamAuditBytes(c.responseReserved)
		c.responseReserved = 0
		c.response.Reset()
		return
	}
	c.responseReserved += int64(len(p))
	_, _ = c.response.Write(p)
}

func (c *UpstreamAuditCapture) finalize(outcome string, complete bool, err error) {
	if c == nil || !c.finalized.CompareAndSwap(false, true) {
		return
	}
	c.mu.Lock()
	if c.log.Outcome == UpstreamAuditOutcomeCaptureOverflow || c.log.Outcome == "audit_error" {
		outcome = c.log.Outcome
	}
	if c.log.ResponseBytes > 0 {
		c.log.ResponseSHA256 = hex.EncodeToString(c.responseHash.Sum(nil))
	}
	if c.overflow {
		outcome = UpstreamAuditOutcomeCaptureOverflow
	} else if c.response.Len() > 0 {
		c.log.responseBodyRaw = append([]byte(nil), c.response.Bytes()...)
	}
	c.log.ResponseComplete = complete
	c.log.Outcome = outcome
	if err != nil && c.log.ErrorMessage == "" {
		c.log.ErrorMessage = boundedAuditError(err)
	}
	c.log.CompletedAt = time.Now()
	releaseUpstreamAuditBytes(c.requestReserved + c.responseReserved)
	c.requestReserved = 0
	c.responseReserved = 0
	c.response.Reset()
	c.mu.Unlock()
	c.service.RecordUpstream(c.log)
}

func (c *UpstreamAuditCapture) reserveRequestPayload() {
	if c == nil || c.log == nil || len(c.log.requestBodyRaw) == 0 {
		return
	}
	size := int64(len(c.log.requestBodyRaw))
	if tryReserveUpstreamAuditBytes(size) {
		c.requestReserved = size
		return
	}
	c.log.requestBodyRaw = nil
	c.log.Outcome = UpstreamAuditOutcomeCaptureOverflow
}

func tryReserveUpstreamAuditBytes(size int64) bool {
	if size <= 0 {
		return true
	}
	for {
		current := upstreamAuditActiveBytes.Load()
		if size > maxUpstreamAuditActiveBytes-current {
			return false
		}
		if upstreamAuditActiveBytes.CompareAndSwap(current, current+size) {
			return true
		}
	}
}

func releaseUpstreamAuditBytes(size int64) {
	if size > 0 {
		upstreamAuditActiveBytes.Add(-size)
	}
}

type upstreamAuditBody struct {
	io.ReadCloser
	capture *UpstreamAuditCapture
}

func (b *upstreamAuditBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if n > 0 {
		b.capture.appendResponse(p[:n])
	}
	if err == io.EOF {
		b.capture.finalize(UpstreamAuditOutcomeCompleted, true, nil)
	} else if err != nil {
		b.capture.finalize(UpstreamAuditOutcomeReadError, false, err)
	}
	return n, err
}

func (b *upstreamAuditBody) Close() error {
	err := b.ReadCloser.Close()
	b.capture.finalize(UpstreamAuditOutcomeClosed, false, err)
	return err
}

func captureRequestBody(req *http.Request, log *UpstreamAuditLog) {
	if req == nil || log == nil || req.Body == nil {
		return
	}
	// Never pre-consume a non-replayable body: fail-open auditing must not alter
	// the upstream reader's blocking, partial-read, or error behavior.
	if req.GetBody == nil {
		log.Outcome = "audit_error"
		log.ErrorMessage = "request body is not replayable"
		return
	}
	reader, err := req.GetBody()
	if err != nil {
		log.Outcome = "audit_error"
		log.ErrorMessage = boundedAuditError(err)
		return
	}
	limited, err := io.ReadAll(io.LimitReader(reader, defaultUpstreamAuditMaxBodyBytes+1))
	_ = reader.Close()
	if err != nil {
		log.Outcome = "audit_error"
		log.ErrorMessage = boundedAuditError(err)
		return
	}
	captureRequestBytes(limited, log)
}

func captureRequestBytes(raw []byte, log *UpstreamAuditLog) {
	if log == nil {
		return
	}
	log.RequestBytes = int64(len(raw))
	if len(raw) > defaultUpstreamAuditMaxBodyBytes {
		log.Outcome = UpstreamAuditOutcomeCaptureOverflow
		return
	}
	log.RequestSHA256 = auditSHA256(raw)
	log.requestBodyRaw = append([]byte(nil), raw...)
}

func compressAuditBytes(raw []byte) ([]byte, error) {
	encoder, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedDefault))
	if err != nil {
		return nil, err
	}
	defer encoder.Close()
	return encoder.EncodeAll(raw, nil), nil
}

func decompressAuditBytes(compressed []byte) ([]byte, error) {
	decoder, err := zstd.NewReader(nil)
	if err != nil {
		return nil, err
	}
	defer decoder.Close()
	return decoder.DecodeAll(compressed, nil)
}

func auditSHA256(raw []byte) string {
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

func sanitizeAuditURL(value *url.URL) string {
	if value == nil {
		return ""
	}
	return (&url.URL{Scheme: value.Scheme, Host: value.Host, Path: value.Path, RawPath: value.RawPath}).String()
}

func sanitizedAuditHeaders(headers http.Header, request bool) string {
	allowed := map[string]bool{
		"content-type": true, "accept": true, "anthropic-version": true,
		"anthropic-beta": true, "openai-beta": true, "user-agent": true,
		"x-request-id": true, "request-id": true, "openai-request-id": true,
	}
	result := make(map[string][]string)
	for key, values := range headers {
		lower := strings.ToLower(strings.TrimSpace(key))
		if !allowed[lower] {
			continue
		}
		if request && (lower == "x-request-id" || lower == "request-id" || lower == "openai-request-id") {
			continue
		}
		result[lower] = append([]string(nil), values...)
	}
	encoded, _ := json.Marshal(result)
	return string(encoded)
}

func boundedAuditError(err error) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	message = auditErrorURLPattern.ReplaceAllStringFunc(message, func(rawURL string) string {
		parsed, parseErr := url.Parse(rawURL)
		if parseErr != nil {
			return "[redacted-url]"
		}
		return sanitizeAuditURL(parsed)
	})
	message = auditErrorSecretPattern.ReplaceAllString(message, "$1$2[redacted]")
	if len(message) > maxUpstreamAuditErrorBytes {
		message = message[:maxUpstreamAuditErrorBytes]
	}
	return message
}

func copyAuditGroupID(groupID *int64) *int64 {
	if groupID == nil {
		return nil
	}
	copyID := *groupID
	return &copyID
}
