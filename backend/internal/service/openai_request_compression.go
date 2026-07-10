package service

import (
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/klauspost/compress/zstd"
)

func (s *OpenAIGatewayService) maybeCompressOpenAIRequestBody(
	account *Account,
	targetURL string,
	body []byte,
	isStream bool,
) ([]byte, bool, error) {
	if !s.shouldCompressOpenAIRequestBody(account, targetURL, body, isStream) {
		return body, false, nil
	}

	encoder, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedDefault))
	if err != nil {
		return nil, false, fmt.Errorf("create zstd encoder: %w", err)
	}
	compressed := encoder.EncodeAll(body, make([]byte, 0, len(body)/2))
	_ = encoder.Close()
	logger.LegacyPrintf(
		"service.openai_gateway",
		"[OpenAI request compression] zstd compressed request body: pre_bytes=%d post_bytes=%d target=%s",
		len(body),
		len(compressed),
		targetURL,
	)
	return compressed, true, nil
}

func (s *OpenAIGatewayService) shouldCompressOpenAIRequestBody(
	account *Account,
	targetURL string,
	body []byte,
	isStream bool,
) bool {
	if s == nil || s.cfg == nil || !s.cfg.Gateway.OpenAIRequestCompressionEnabled {
		return false
	}
	if account == nil || account.Type != AccountTypeOAuth {
		return false
	}
	if !isStream || len(body) == 0 {
		return false
	}
	minBytes := s.cfg.Gateway.OpenAIRequestCompressionMinBytes
	if minBytes <= 0 {
		minBytes = 64 * 1024
	}
	if len(body) < minBytes {
		return false
	}
	return targetURL == chatgptCodexURL
}
