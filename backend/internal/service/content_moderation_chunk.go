package service

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	contentModerationChunkSizeRunes      = 32_768
	contentModerationChunkOverlapRunes   = 1_024
	contentModerationChunkParallelism    = 8
	contentModerationChunkPolicyRevision = "incremental-full-context-v1"
	contentModerationChunkSafeTTL        = 24 * time.Hour
	contentModerationChunkBlockTTL       = 30 * 24 * time.Hour
)

type contentModerationChunk struct {
	Text string
	Hash string
}

func splitContentModerationChunks(text string) []contentModerationChunk {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	runes := []rune(text)
	step := contentModerationChunkSizeRunes - contentModerationChunkOverlapRunes
	chunks := make([]contentModerationChunk, 0, (len(runes)+step-1)/step)
	for start := 0; start < len(runes); start += step {
		end := start + contentModerationChunkSizeRunes
		if end > len(runes) {
			end = len(runes)
		}
		chunkText := string(runes[start:end])
		chunks = append(chunks, contentModerationChunk{
			Text: chunkText,
			Hash: contentModerationChunkHash(chunkText),
		})
		if end == len(runes) {
			break
		}
	}
	return chunks
}

func contentModerationChunkHash(text string) string {
	sum := sha256.Sum256([]byte(contentModerationChunkPolicyRevision + "\x00" + text))
	return hex.EncodeToString(sum[:])
}

func contentModerationChunkPolicyNamespace(cfg *ContentModerationConfig) string {
	var builder strings.Builder
	builder.WriteString(contentModerationChunkPolicyRevision)
	builder.WriteByte('\n')
	builder.WriteString(strconv.Itoa(contentModerationChunkSizeRunes))
	builder.WriteByte(':')
	builder.WriteString(strconv.Itoa(contentModerationChunkOverlapRunes))
	if cfg != nil {
		builder.WriteByte('\n')
		builder.WriteString(strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/"))
		builder.WriteByte('\n')
		builder.WriteString(strings.TrimSpace(cfg.Model))
		keys := make([]string, 0, len(cfg.Thresholds))
		for category := range cfg.Thresholds {
			keys = append(keys, category)
		}
		sort.Strings(keys)
		for _, category := range keys {
			builder.WriteByte('\n')
			builder.WriteString(category)
			builder.WriteByte('=')
			builder.WriteString(strconv.FormatFloat(cfg.Thresholds[category], 'g', -1, 64))
		}
	}
	sum := sha256.Sum256([]byte(builder.String()))
	return hex.EncodeToString(sum[:])
}
