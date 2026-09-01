package service

import (
	"context"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/klauspost/compress/zstd"
	"go.uber.org/zap"
)

const defaultOpenAIRequestCompressionMinBytes = 64 * 1024

// maybeCompressOpenAIRequestBody prepares the wire body for the ChatGPT Codex
// HTTP Responses endpoint. Compression is an optional transport optimization:
// failures must never turn an otherwise valid model request into a gateway
// error, so every failure falls back to the original JSON body.
func (s *OpenAIGatewayService) maybeCompressOpenAIRequestBody(
	ctx context.Context,
	account *Account,
	targetURL string,
	body []byte,
	isStream bool,
) ([]byte, bool) {
	if !s.shouldCompressOpenAIRequestBody(account, targetURL, body, isStream) {
		return body, false
	}

	startedAt := time.Now()
	encoder, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedDefault))
	if err != nil {
		logger.FromContext(ctx).Warn("openai request compression initialization failed; sending original body",
			zap.Int64("account_id", account.ID),
			zap.Int("original_bytes", len(body)),
			zap.Error(err),
		)
		return body, false
	}
	compressed := encoder.EncodeAll(body, make([]byte, 0, len(body)/2))
	_ = encoder.Close()

	if len(compressed) >= len(body) {
		logger.FromContext(ctx).Debug("openai request compression skipped because wire body is not smaller",
			zap.Int64("account_id", account.ID),
			zap.Int("original_bytes", len(body)),
			zap.Int("compressed_bytes", len(compressed)),
			zap.Duration("compression_duration", time.Since(startedAt)),
		)
		return body, false
	}

	logger.FromContext(ctx).Info("openai request body compressed with zstd",
		zap.Int64("account_id", account.ID),
		zap.Int("original_bytes", len(body)),
		zap.Int("compressed_bytes", len(compressed)),
		zap.Duration("compression_duration", time.Since(startedAt)),
	)
	return compressed, true
}

func (s *OpenAIGatewayService) shouldCompressOpenAIRequestBody(account *Account, targetURL string, body []byte, isStream bool) bool {
	if s == nil || s.cfg == nil || !s.cfg.Gateway.OpenAIRequestCompressionEnabled {
		return false
	}
	if account == nil || !account.UsesOpenAICodexProtocol() || !isStream || len(body) == 0 {
		return false
	}
	// Only the ChatGPT Codex HTTP /responses endpoint is known to accept zstd.
	// Exact matching intentionally excludes /responses/compact and every custom
	// OpenAI-compatible base URL.
	if strings.TrimSpace(targetURL) != chatgptCodexURL {
		return false
	}

	minBytes := s.cfg.Gateway.OpenAIRequestCompressionMinBytes
	if minBytes <= 0 {
		minBytes = defaultOpenAIRequestCompressionMinBytes
	}
	return len(body) >= minBytes
}
