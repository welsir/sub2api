package service

import (
	"bufio"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

const codexTicketPolicyVersion = 2

// Envelope shape is an empirical admission rule, never a signature or quality
// check. Match Sleep State's personal/Team policy; legacy target_length must
// not turn an observed rejected shape into a successful ticket.
func codexTicketTargetLength(account *Account) int {
	if account != nil {
		switch strings.ToLower(strings.TrimSpace(account.GetCredential("plan_type"))) {
		case "team", "business":
			return 332
		}
	}
	return 292
}

func codexTicketIssued(state string) (time.Time, error) {
	if len(state) > 2048 || strings.ContainsAny(state, "\r\n\t ") {
		return time.Time{}, errors.New("invalid_state_encoding")
	}
	core := strings.TrimRight(state, "=")
	if len(state)-len(core) > 2 {
		return time.Time{}, errors.New("invalid_state_padding")
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(core)
	if err != nil || len(raw) < 73 || raw[0] != 0x80 || (len(raw)-57)%16 != 0 {
		return time.Time{}, errors.New("invalid_state_envelope")
	}
	issued := binary.BigEndian.Uint64(raw[1:9])
	if issued < 1577836800 || issued >= 4102444800 {
		return time.Time{}, errors.New("invalid_state_timestamp")
	}
	return time.Unix(int64(issued), 0), nil
}

func codexTicketDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func codexTicketCredentialKey(account *Account) string {
	if account == nil {
		return ""
	}
	return codexTicketDigest(account.GetCredential("chatgpt_account_id") + "\x00" + account.GetCredential("access_token"))
}

type codexTicketProbeError struct {
	reason     string
	status     int
	retryAfter time.Duration
}

func (e *codexTicketProbeError) Error() string { return e.reason }

// Read the complete bounded SSE, including multiline data fields. A failure
// anywhere wins over a later completed event. Never log upstream bodies/state.
func validateCodexTicketStream(body io.Reader, model string) error {
	return validateCodexTicketStreamLimit(body, model, 1<<20)
}

func validateCodexTicketStreamLimit(body io.Reader, model string, maxBytes int64) error {
	limited := &io.LimitedReader{R: body, N: maxBytes + 1}
	scanner := bufio.NewScanner(limited)
	scanner.Buffer(make([]byte, 4096), int(maxBytes+1))
	var data []string
	var eventName string
	completed := false
	var failure error
	inspect := func() {
		defer func() { data = nil; eventName = "" }()
		if len(data) == 0 || strings.Join(data, "\n") == "[DONE]" {
			return
		}
		var event struct {
			Type  string `json:"type"`
			Code  string `json:"code"`
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
			Response struct {
				Model  string `json:"model"`
				Status string `json:"status"`
				Error  struct {
					Code string `json:"code"`
				} `json:"error"`
			} `json:"response"`
		}
		if json.Unmarshal([]byte(strings.Join(data, "\n")), &event) != nil {
			if failure == nil {
				failure = errors.New("invalid_probe_event")
			}
			return
		}
		kind := event.Type
		if kind == "" {
			kind = eventName
		}
		if got := strings.TrimSpace(event.Response.Model); got != "" && got != model && failure == nil {
			failure = errors.New("probe_model_mismatch")
		}
		switch kind {
		case "response.failed", "response.incomplete", "error":
			code := event.Response.Error.Code
			if code == "" {
				code = event.Error.Code
			}
			if code == "" {
				code = event.Code
			}
			e := &codexTicketProbeError{reason: "probe_response_failed"}
			switch code {
			case "rate_limit_exceeded", "usage_limit_reached", "insufficient_quota":
				e.status, e.reason = http.StatusTooManyRequests, "probe_rate_limited"
			case "invalid_api_key", "token_expired", "authentication_error":
				e.status, e.reason = http.StatusUnauthorized, "probe_authentication_failed"
			}
			if failure == nil || e.status != 0 {
				failure = e
			}
		case "response.completed":
			if event.Response.Model != model || event.Response.Status != "completed" {
				if failure == nil {
					failure = errors.New("probe_completion_not_verified")
				}
			} else {
				completed = true
			}
		}
	}
	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		switch {
		case line == "":
			inspect()
		case strings.HasPrefix(line, "data:"):
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		case strings.HasPrefix(line, "event:"):
			eventName = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		}
	}
	inspect()
	if failure != nil {
		return failure
	}
	if scanner.Err() != nil || limited.N <= 0 {
		return errors.New("probe_stream_read_failed")
	}
	if !completed {
		return errors.New("probe_stream_incomplete")
	}
	return nil
}

// Whitelist diagnostic reasons so transport errors cannot expose proxy URLs.
func codexTicketProbeReason(err error) string {
	var probe *codexTicketProbeError
	if errors.As(err, &probe) {
		return probe.reason
	}
	switch err.Error() {
	case "probe_model_mismatch", "probe_completion_not_verified", "invalid_probe_event", "probe_stream_read_failed", "probe_stream_incomplete", "probe_missing_body":
		return err.Error()
	default:
		return "probe_transport_or_protocol_error"
	}
}
