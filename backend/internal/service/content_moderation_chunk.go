package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
)

const (
	contentModerationChunkSizeRunes      = 32_768
	contentModerationChunkOverlapRunes   = 1_024
	contentModerationChunkParallelism    = 8
	contentModerationChunkPolicyRevision = "incremental-full-context-v2-attachment-text-only"
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

func splitContentModerationChunks(text string, classifierPolicyRevision string) []contentModerationChunk {
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
			Hash: contentModerationChunkHash(chunkText, classifierPolicyRevision),
		})
		if end == len(runes) {
			break
		}
	}
	return chunks
}

func contentModerationChunkHash(text string, classifierPolicyRevision string) string {
	sum := sha256.Sum256([]byte(contentModerationChunkPolicyRevision + "\x00" + strings.TrimSpace(classifierPolicyRevision) + "\x00" + text))
	return hex.EncodeToString(sum[:])
}

func contentModerationChunkPolicyNamespace(cfg *ContentModerationConfig) string {
	var builder strings.Builder
	builder.WriteString(contentModerationChunkPolicyRevision)
	builder.WriteByte('\n')
	if cfg != nil {
		builder.WriteString(strings.TrimSpace(cfg.ClassifierPolicyRevision))
	}
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

type contentModerationAdapterReadiness struct {
	Status                   string `json:"status"`
	ClassifierPolicyRevision string `json:"classifier_policy_revision"`
}

type contentModerationClassifierPolicyState struct {
	generation uint64
	verified   bool
	inFlight   *contentModerationClassifierPolicyCall
}

type contentModerationClassifierPolicyCall struct {
	done chan struct{}
	err  error
}

func contentModerationClassifierVerificationKey(cfg *ContentModerationConfig) string {
	if cfg == nil {
		return ""
	}
	return strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/") + "\x00" + strings.TrimSpace(cfg.ClassifierPolicyRevision)
}

func (s *ContentModerationService) ensureContentModerationClassifierPolicy(
	ctx context.Context,
	cfg *ContentModerationConfig,
) error {
	if s == nil || cfg == nil {
		return errors.New("content moderation classifier policy verification is unavailable")
	}
	expected := strings.TrimSpace(cfg.ClassifierPolicyRevision)
	if expected == "" {
		return errors.New("content moderation classifier policy revision is required")
	}
	key := contentModerationClassifierVerificationKey(cfg)
	for {
		s.classifierPolicyMu.Lock()
		if s.classifierPolicyStates == nil {
			s.classifierPolicyStates = make(map[string]*contentModerationClassifierPolicyState)
		}
		state := s.classifierPolicyStates[key]
		if state == nil {
			state = &contentModerationClassifierPolicyState{}
			s.classifierPolicyStates[key] = state
		}
		if state.verified {
			s.classifierPolicyMu.Unlock()
			return nil
		}
		if call := state.inFlight; call != nil {
			s.classifierPolicyMu.Unlock()
			select {
			case <-call.done:
				if call.err != nil {
					return call.err
				}
				continue
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		call := &contentModerationClassifierPolicyCall{done: make(chan struct{})}
		generation := state.generation
		state.inFlight = call
		s.classifierPolicyMu.Unlock()

		err := s.probeContentModerationClassifierPolicy(ctx, cfg, expected)

		s.classifierPolicyMu.Lock()
		call.err = err
		accepted := err == nil && state.generation == generation && state.inFlight == call
		if state.inFlight == call {
			state.inFlight = nil
		}
		if accepted {
			state.verified = true
		}
		close(call.done)
		s.classifierPolicyMu.Unlock()

		if err != nil {
			return err
		}
		if accepted {
			return nil
		}
	}
}

func (s *ContentModerationService) probeContentModerationClassifierPolicy(
	ctx context.Context,
	cfg *ContentModerationConfig,
	expected string,
) error {
	endpoint, err := url.JoinPath(strings.TrimRight(cfg.BaseURL, "/"), "/readyz")
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	client := s.httpClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("content moderation classifier readiness failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("content moderation classifier readiness status %d", resp.StatusCode)
	}
	var readiness contentModerationAdapterReadiness
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4_096)).Decode(&readiness); err != nil {
		return fmt.Errorf("decode content moderation classifier readiness: %w", err)
	}
	actual := strings.TrimSpace(readiness.ClassifierPolicyRevision)
	if actual == "" {
		return errors.New("content moderation classifier readiness omitted policy revision")
	}
	if actual != expected {
		return fmt.Errorf("content moderation classifier policy revision mismatch: expected %q, got %q", expected, actual)
	}
	return nil
}

func (s *ContentModerationService) invalidateContentModerationClassifierPolicy(cfg *ContentModerationConfig) {
	if s == nil {
		return
	}
	key := contentModerationClassifierVerificationKey(cfg)
	s.classifierPolicyMu.Lock()
	if state := s.classifierPolicyStates[key]; state != nil {
		state.generation++
		state.verified = false
	}
	s.classifierPolicyMu.Unlock()
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
	timeout := time.Duration(cfg.TimeoutMS) * time.Millisecond
	reviewCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := s.ensureContentModerationClassifierPolicy(reviewCtx, cfg); err != nil {
		return nil, stats, err
	}

	chunks := splitContentModerationChunks(text, cfg.ClassifierPolicyRevision)
	stats.TotalChunks = len(chunks)
	if len(chunks) == 0 {
		return nil, stats, errors.New("content moderation chunk input is empty")
	}

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
	var outcomeMu sync.Mutex
	var hardErr error
	unsafe := false
	workerCtx, stopWorkers := context.WithCancel(reviewCtx)
	defer stopWorkers()
	recordHardError := func(err error) {
		if err == nil {
			return
		}
		outcomeMu.Lock()
		if hardErr == nil && !(errors.Is(err, context.Canceled) && unsafe) {
			hardErr = err
		}
		outcomeMu.Unlock()
		stopWorkers()
	}
	recordUnsafe := func() {
		outcomeMu.Lock()
		unsafe = true
		outcomeMu.Unlock()
		stopWorkers()
	}
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
				recordHardError(err)
				return nil
			}
			if strings.TrimSpace(result.ClassifierPolicyRevision) != strings.TrimSpace(cfg.ClassifierPolicyRevision) {
				s.invalidateContentModerationClassifierPolicy(cfg)
				recordHardError(fmt.Errorf(
					"content moderation response classifier policy revision mismatch: expected %q, got %q",
					strings.TrimSpace(cfg.ClassifierPolicyRevision),
					strings.TrimSpace(result.ClassifierPolicyRevision),
				))
				return nil
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
				recordUnsafe()
			}
			return nil
		})
	}
	_ = group.Wait()
	outcomeMu.Lock()
	workerHardErr := hardErr
	outcomeMu.Unlock()
	if workerHardErr != nil {
		return nil, stats, workerHardErr
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
