package service

import (
	"context"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"go.uber.org/zap"
)

func codexTicketReserveKey(model string) string { return openAICodexTicketExtraKey(model) + ":reserve" }

func (s *OpenAIGatewayService) codexTicketPairLock(account *Account, model string) *sync.Mutex {
	raw, _ := s.openaiCodexTicketPairLocks.LoadOrStore(openAICodexTicketKey(account.ID, model), &sync.Mutex{})
	return raw.(*sync.Mutex)
}

func (s *OpenAIGatewayService) codexTicketReserve(account *Account, model string) *openAICodexTicket {
	key := openAICodexTicketKey(account.ID, model)
	if raw, ok := s.openaiCodexTicketReserves.Load(key); ok {
		return raw.(*openAICodexTicket)
	}
	if account.Extra != nil {
		return parseOpenAICodexTicketFromAny(account.ID, model, account.Extra[codexTicketReserveKey(model)])
	}
	return nil
}

func codexTicketNewer(candidate, active *openAICodexTicket) bool {
	if candidate == nil {
		return false
	}
	if active == nil {
		return true
	}
	issued, err := codexTicketIssued(candidate.State)
	old, oldErr := codexTicketIssued(active.State)
	return err == nil && (oldErr != nil || issued.After(old))
}

func (s *OpenAIGatewayService) lookupOpenAICodexTicketContext(ctx context.Context, account *Account, model string) *openAICodexTicket {
	if s == nil || account == nil {
		return nil
	}
	mu := s.codexTicketPairLock(account, model)
	mu.Lock()
	defer mu.Unlock()
	active := s.lookupOpenAICodexTicketRaw(ctx, account, model)
	reserve := s.codexTicketReserve(account, model)
	now := time.Now()
	if s.codexTicketUsableContext(ctx, account, reserve, now) &&
		(!s.codexTicketUsableContext(ctx, account, active, now) || (active.needsRefresh(now, time.Duration(s.openAICodexTicketConfig().RefreshBeforeSeconds)*time.Second) && codexTicketNewer(reserve, active))) {
		s.persistCodexTicketPair(ctx, account, model, reserve, nil)
		return reserve
	}
	return active
}

func (s *OpenAIGatewayService) offerCodexTicket(ctx context.Context, account *Account, ticket *openAICodexTicket) {
	mu := s.codexTicketPairLock(account, ticket.Model)
	mu.Lock()
	defer mu.Unlock()
	now := time.Now()
	if !s.codexTicketUsableContext(ctx, account, ticket, now) {
		return
	}
	active := s.lookupOpenAICodexTicketRaw(ctx, account, ticket.Model)
	reserve := s.codexTicketReserve(account, ticket.Model)
	if !s.codexTicketUsableContext(ctx, account, active, now) {
		s.persistCodexTicketPair(ctx, account, ticket.Model, ticket, nil)
		return
	}
	if !codexTicketNewer(ticket, active) {
		return
	}
	if active.needsRefresh(now, time.Duration(s.openAICodexTicketConfig().RefreshBeforeSeconds)*time.Second) {
		s.persistCodexTicketPair(ctx, account, ticket.Model, ticket, nil)
	} else if !s.codexTicketUsableContext(ctx, account, reserve, now) || codexTicketNewer(ticket, reserve) {
		s.persistCodexTicketPair(ctx, account, ticket.Model, active, ticket)
	}
}

// Persist both slots atomically. A nil reserve is also kept in memory as a
// tombstone so a stale scheduler account snapshot cannot resurrect it.
func (s *OpenAIGatewayService) persistCodexTicketPair(ctx context.Context, account *Account, model string, active, reserve *openAICodexTicket) {
	key := openAICodexTicketKey(account.ID, model)
	s.openaiCodexTickets.Store(key, active)
	s.openaiCodexTicketReserves.Store(key, reserve)
	s.openaiCodexTicketBindings.Store(codexTicketDigest(active.State), active)
	if s.accountRepo == nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := s.accountRepo.UpdateExtra(ctx, account.ID, map[string]any{openAICodexTicketExtraKey(model): active, codexTicketReserveKey(model): reserve}); err != nil {
		logger.L().Warn("openai_codex_ticket pair persist failed", zap.Int64("account_id", account.ID), zap.String("model", model), zap.Error(err))
	}
}
