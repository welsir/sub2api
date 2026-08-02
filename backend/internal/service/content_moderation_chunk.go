package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/errgroup"
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

type ContentModerationChunkVerdict struct {
	Flagged        bool               `json:"flagged"`
	CategoryScores map[string]float64 `json:"category_scores"`
}

type ContentModerationChunkCache interface {
	GetContentModerationChunkVerdicts(ctx context.Context, namespace string, chunkHashes []string) (map[string]ContentModerationChunkVerdict, error)
	StoreContentModerationChunkVerdicts(ctx context.Context, namespace string, verdicts map[string]ContentModerationChunkVerdict, safeTTL time.Duration, blockTTL time.Duration) error
}

type contentModerationChunkReviewStats struct {
	TotalChunks    int
	CacheHits      int
	CacheMisses    int
	ReviewedChunks int
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

func (s *ContentModerationService) callModerationIncremental(
	ctx context.Context,
	cfg *ContentModerationConfig,
	text string,
	trackKeyLoad bool,
) (*moderationAPIResult, contentModerationChunkReviewStats, error) {
	stats := contentModerationChunkReviewStats{}
	if s == nil || s.chunkCache == nil {
		return nil, stats, errors.New("content moderation chunk cache is unavailable")
	}
	chunks := splitContentModerationChunks(text)
	stats.TotalChunks = len(chunks)
	if len(chunks) == 0 {
		return nil, stats, errors.New("content moderation chunk input is empty")
	}
	timeout := time.Duration(cfg.TimeoutMS) * time.Millisecond
	reviewCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	namespace := contentModerationChunkPolicyNamespace(cfg)
	hashes := make([]string, len(chunks))
	for index, chunk := range chunks {
		hashes[index] = chunk.Hash
	}
	cached, err := s.chunkCache.GetContentModerationChunkVerdicts(reviewCtx, namespace, hashes)
	if err != nil {
		return nil, stats, err
	}

	mergedScores := make(map[string]float64)
	misses := make([]contentModerationChunk, 0, len(chunks))
	seenMisses := make(map[string]struct{}, len(chunks))
	for _, chunk := range chunks {
		if verdict, ok := cached[chunk.Hash]; ok {
			stats.CacheHits++
			mergeContentModerationCategoryScores(mergedScores, verdict.CategoryScores)
			if verdict.Flagged {
				return &moderationAPIResult{
					Flagged:        true,
					CategoryScores: mergedScores,
				}, stats, nil
			}
			continue
		}
		stats.CacheMisses++
		if _, seen := seenMisses[chunk.Hash]; seen {
			continue
		}
		seenMisses[chunk.Hash] = struct{}{}
		misses = append(misses, chunk)
	}

	reviewed := make(map[string]ContentModerationChunkVerdict, len(misses))
	var reviewedMu sync.Mutex
	var unsafe atomic.Bool
	workerCtx, stopWorkers := context.WithCancel(reviewCtx)
	defer stopWorkers()
	var group errgroup.Group
	group.SetLimit(contentModerationChunkParallelism)
	for _, chunk := range misses {
		if workerCtx.Err() != nil {
			break
		}
		chunk := chunk
		group.Go(func() error {
			result, err := s.callModeration(workerCtx, cfg, chunk.Text, trackKeyLoad)
			if err != nil {
				stopWorkers()
				return err
			}
			thresholdFlagged, _, _ := evaluateModerationScores(result.CategoryScores, cfg.Thresholds)
			flagged := result.Flagged || thresholdFlagged
			verdict := ContentModerationChunkVerdict{
				Flagged:        flagged,
				CategoryScores: cloneFloatMap(result.CategoryScores),
			}
			reviewedMu.Lock()
			reviewed[chunk.Hash] = verdict
			reviewedMu.Unlock()
			if flagged {
				unsafe.Store(true)
				stopWorkers()
			}
			return nil
		})
	}
	groupErr := group.Wait()
	if groupErr != nil && !unsafe.Load() {
		return nil, stats, groupErr
	}
	stats.ReviewedChunks = len(reviewed)
	if err := reviewCtx.Err(); err != nil {
		return nil, stats, err
	}
	if err := s.chunkCache.StoreContentModerationChunkVerdicts(
		reviewCtx,
		namespace,
		reviewed,
		contentModerationChunkSafeTTL,
		contentModerationChunkBlockTTL,
	); err != nil {
		return nil, stats, err
	}

	flagged := false
	for _, verdict := range reviewed {
		mergeContentModerationCategoryScores(mergedScores, verdict.CategoryScores)
		flagged = flagged || verdict.Flagged
	}
	return &moderationAPIResult{
		Flagged:        flagged,
		CategoryScores: mergedScores,
	}, stats, nil
}

func mergeContentModerationCategoryScores(target map[string]float64, scores map[string]float64) {
	for category, score := range scores {
		if current, ok := target[category]; !ok || score > current {
			target[category] = score
		}
	}
}
