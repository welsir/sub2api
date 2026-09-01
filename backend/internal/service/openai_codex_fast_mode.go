package service

import "context"

type openAICodexForceFastContextKey struct{}

// shouldForceCodexFast keeps the team-only experiment narrowly scoped to
// official Codex traffic backed by ChatGPT/Codex subscription credentials.
func (s *OpenAIGatewayService) shouldForceCodexFast(account *Account, isCodexClient, compact bool) bool {
	return s != nil &&
		s.cfg != nil &&
		s.cfg.Gateway.CodexForceFastEnabled &&
		isCodexClient &&
		!compact &&
		account != nil &&
		account.IsOpenAIOAuthLike()
}

func withOpenAICodexForceFast(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, openAICodexForceFastContextKey{}, true)
}

func openAICodexForceFastFromContext(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	enabled, _ := ctx.Value(openAICodexForceFastContextKey{}).(bool)
	return enabled
}
