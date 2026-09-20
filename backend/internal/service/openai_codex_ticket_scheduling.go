package service

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

type codexTicketProbeBudget struct {
	mu     sync.Mutex
	next   time.Time
	starts []time.Time
	models map[string][]time.Time
	busy   bool
}

// The upstream account identity survives token refresh and duplicate local rows.
// This is process-local admission; independent installations cannot share it.
func codexTicketAccountBudgetKey(account *Account) string {
	if id := strings.TrimSpace(account.GetCredential("chatgpt_account_id")); id != "" {
		return "account:" + codexTicketDigest(id)
	}
	return openAICodexTicketKey(account.ID, "probe-budget")
}

func (s *OpenAIGatewayService) codexTicketBudget(account *Account) *codexTicketProbeBudget {
	b, _ := s.openaiCodexTicketBudgets.LoadOrStore(codexTicketAccountBudgetKey(account), &codexTicketProbeBudget{})
	return b.(*codexTicketProbeBudget)
}

func (s *OpenAIGatewayService) takeCodexTicketProbeSlot(account *Account, model string, now time.Time) bool {
	b := s.codexTicketBudget(account)
	b.mu.Lock()
	defer b.mu.Unlock()
	n := s.openAICodexTicketConfig().AccountProbesPerMinute
	keep := b.starts[:0]
	for _, started := range b.starts {
		if now.Sub(started) < time.Minute {
			keep = append(keep, started)
		}
	}
	b.starts = keep
	if b.models == nil {
		b.models = make(map[string][]time.Time)
	}
	modelStarts := b.models[model][:0]
	for _, started := range b.models[model] {
		if now.Sub(started) < time.Minute {
			modelStarts = append(modelStarts, started)
		}
	}
	b.models[model] = modelStarts
	modelLimit := s.openAICodexTicketConfig().ModelProbesPerMinute
	modelTooSoon := len(modelStarts) > 0 && now.Before(modelStarts[len(modelStarts)-1].Add(time.Minute/time.Duration(modelLimit)))
	if b.busy || now.Before(b.next) || len(b.starts) >= n || len(modelStarts) >= modelLimit || modelTooSoon {
		return false
	}
	b.starts = append(b.starts, now)
	b.models[model] = append(modelStarts, now)
	b.next = now.Add(time.Minute / time.Duration(n))
	b.busy = true
	return true
}

func (s *OpenAIGatewayService) releaseCodexTicketProbeSlot(account *Account) {
	b := s.codexTicketBudget(account)
	b.mu.Lock()
	b.busy = false
	b.mu.Unlock()
}

func (s *OpenAIGatewayService) codexTicketCanWait(model string) bool {
	cfg := s.openAICodexTicketConfig()
	if cfg.ColdWaitSeconds == 0 || s.accountRepo == nil {
		return false
	}
	for _, configured := range cfg.Models {
		if strings.TrimSpace(configured) == model {
			return true
		}
	}
	return false
}

// Waiters only observe the shared harvester. A burst of client requests never
// starts a burst of probes, and cancellation never cancels other clients' work.
func (s *OpenAIGatewayService) waitForCodexTicket(ctx context.Context, account *Account, model string) *openAICodexTicket {
	if !s.codexTicketCanWait(model) {
		return nil
	}
	waitCtx, cancel := context.WithTimeout(ctx, time.Duration(s.openAICodexTicketConfig().ColdWaitSeconds)*time.Second)
	defer cancel()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		if waitCtx.Err() != nil || s.codexTicketPaused(account) || !s.openAICodexTicketEnabledContext(waitCtx) {
			return nil
		}
		ticket := s.lookupOpenAICodexTicketContext(waitCtx, account, model)
		if s.codexTicketUsableContext(waitCtx, account, ticket, time.Now()) {
			return ticket
		}
		select {
		case <-waitCtx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

type codexTicketCompactKey struct{}

var errCodexCompactRoute = errors.New("codex compact requires a configured stable compact_proxy_url")

func codexTicketCompactRequest(req *http.Request) bool {
	compact, _ := req.Context().Value(codexTicketCompactKey{}).(bool)
	return compact || strings.HasSuffix(req.URL.Path, "/responses/compact")
}

// Compaction has its own response contract and never consumes a generation ticket.
func (s *OpenAIGatewayService) prepareCodexTicketRequest(ctx context.Context, c *gin.Context, account *Account, body []byte, req *http.Request) error {
	if s.codexTicketUsesHTTPBridge(ctx, account) && isExplicitOpenAICompactRequest(c, body) {
		*req = *req.WithContext(context.WithValue(req.Context(), codexTicketCompactKey{}, true))
		req.Header.Del(openAICodexTurnStateHeader)
		return nil
	}
	return s.applyOpenAICodexTicket(ctx, account, extractOpenAICodexTicketModel(body), req.Header)
}
