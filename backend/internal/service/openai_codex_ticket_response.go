package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

const strictCodexResponseLimit = 32 << 20

var errStrictCodexResponse = errors.New("strict_codex_response_not_verified")

// Strict mode buffers the bounded response before exposing any successful
// response to the client. A collected state cannot attest a later response.
func (s *OpenAIGatewayService) validateStrictCodexResponse(req *http.Request, account *Account, resp *http.Response, upstreamErr error) (*http.Response, error) {
	if codexTicketCompactRequest(req) || upstreamErr != nil || !s.codexTicketUsesHTTPBridge(req.Context(), account) || !s.openAICodexTicketConfig().FailClosed {
		return resp, upstreamErr
	}
	if resp == nil {
		return nil, errStrictCodexResponse
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp, nil
	}
	if resp.Body == nil {
		return nil, errStrictCodexResponse
	}
	defer resp.Body.Close()
	raw, ok := s.openaiCodexTicketBindings.Load(codexTicketDigest(req.Header.Get(openAICodexTurnStateHeader)))
	if !ok {
		return nil, ErrOpenAICodexTicketUnavailable
	}
	ticket := raw.(*openAICodexTicket)
	if ticket.AccountID != account.ID {
		return nil, ErrOpenAICodexTicketUnavailable
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, strictCodexResponseLimit+1))
	if err != nil || len(body) > strictCodexResponseLimit {
		return nil, errStrictCodexResponse
	}
	if strings.Contains(resp.Header.Get("Content-Type"), "application/json") || bytes.HasPrefix(bytes.TrimSpace(body), []byte("{")) {
		var result struct {
			Model  string          `json:"model"`
			Status string          `json:"status"`
			Error  json.RawMessage `json:"error"`
		}
		if json.Unmarshal(body, &result) != nil || result.Model != ticket.Model || result.Status != "completed" || (len(result.Error) > 0 && string(result.Error) != "null") {
			return nil, errStrictCodexResponse
		}
	} else if err := validateCodexTicketStreamLimit(bytes.NewReader(body), ticket.Model, strictCodexResponseLimit); err != nil {
		var probe *codexTicketProbeError
		if errors.As(err, &probe) && probe.status != 0 {
			s.pauseCodexTicket(account, probe.status, codexTicketRetryAfter(resp.Header))
		}
		return nil, errStrictCodexResponse
	}
	// Replace only after validation; callers retain their normal parsing/billing.
	copyResp := *resp
	copyResp.Body = io.NopCloser(bytes.NewReader(body))
	return &copyResp, nil
}
