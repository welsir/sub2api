package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

const contentModerationFlaggedHashSetKey = "content_moderation:flagged_hashes"

const contentModerationChunkCacheKeyPrefix = "content_moderation:chunk:"

type contentModerationHashCache struct {
	rdb *redis.Client
}

func NewContentModerationHashCache(rdb *redis.Client) service.ContentModerationHashCache {
	return &contentModerationHashCache{rdb: rdb}
}

func (c *contentModerationHashCache) RecordFlaggedInputHash(ctx context.Context, inputHash string) error {
	inputHash = strings.TrimSpace(inputHash)
	if c == nil || c.rdb == nil || inputHash == "" {
		return nil
	}
	return c.rdb.SAdd(ctx, contentModerationFlaggedHashSetKey, inputHash).Err()
}

func (c *contentModerationHashCache) HasFlaggedInputHash(ctx context.Context, inputHash string) (bool, error) {
	inputHash = strings.TrimSpace(inputHash)
	if c == nil || c.rdb == nil || inputHash == "" {
		return false, nil
	}
	return c.rdb.SIsMember(ctx, contentModerationFlaggedHashSetKey, inputHash).Result()
}

func (c *contentModerationHashCache) DeleteFlaggedInputHash(ctx context.Context, inputHash string) (bool, error) {
	inputHash = strings.TrimSpace(inputHash)
	if c == nil || c.rdb == nil || inputHash == "" {
		return false, nil
	}
	deleted, err := c.rdb.SRem(ctx, contentModerationFlaggedHashSetKey, inputHash).Result()
	if err != nil {
		return false, err
	}
	return deleted > 0, nil
}

func (c *contentModerationHashCache) ClearFlaggedInputHashes(ctx context.Context) (int64, error) {
	if c == nil || c.rdb == nil {
		return 0, nil
	}
	deleted, err := c.rdb.SCard(ctx, contentModerationFlaggedHashSetKey).Result()
	if err != nil {
		return 0, err
	}
	if deleted == 0 {
		return 0, nil
	}
	if err := c.rdb.Del(ctx, contentModerationFlaggedHashSetKey).Err(); err != nil {
		return 0, err
	}
	return deleted, nil
}

func (c *contentModerationHashCache) CountFlaggedInputHashes(ctx context.Context) (int64, error) {
	if c == nil || c.rdb == nil {
		return 0, nil
	}
	return c.rdb.SCard(ctx, contentModerationFlaggedHashSetKey).Result()
}

func (c *contentModerationHashCache) GetContentModerationChunkVerdicts(
	ctx context.Context,
	namespace string,
	chunkHashes []string,
) (map[string]service.ContentModerationChunkVerdict, error) {
	if c == nil || c.rdb == nil {
		return nil, errors.New("content moderation chunk cache is unavailable")
	}
	if !isContentModerationCacheDigest(namespace) {
		return nil, errors.New("invalid content moderation chunk namespace")
	}
	if len(chunkHashes) == 0 {
		return map[string]service.ContentModerationChunkVerdict{}, nil
	}
	uniqueHashes := make([]string, 0, len(chunkHashes))
	seen := make(map[string]struct{}, len(chunkHashes))
	for _, chunkHash := range chunkHashes {
		if !isContentModerationCacheDigest(chunkHash) {
			return nil, fmt.Errorf("invalid content moderation chunk hash")
		}
		if _, ok := seen[chunkHash]; ok {
			continue
		}
		seen[chunkHash] = struct{}{}
		uniqueHashes = append(uniqueHashes, chunkHash)
	}
	keys := make([]string, len(uniqueHashes))
	for index, chunkHash := range uniqueHashes {
		keys[index] = contentModerationChunkCacheKey(namespace, chunkHash)
	}
	values, err := c.rdb.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, err
	}
	verdicts := make(map[string]service.ContentModerationChunkVerdict, len(values))
	for index, raw := range values {
		var encoded string
		switch value := raw.(type) {
		case string:
			encoded = value
		case []byte:
			encoded = string(value)
		default:
			continue
		}
		var verdict service.ContentModerationChunkVerdict
		if err := json.Unmarshal([]byte(encoded), &verdict); err != nil || !validContentModerationChunkVerdict(verdict) {
			continue
		}
		verdicts[uniqueHashes[index]] = verdict
	}
	return verdicts, nil
}

func (c *contentModerationHashCache) StoreContentModerationChunkVerdicts(
	ctx context.Context,
	namespace string,
	verdicts map[string]service.ContentModerationChunkVerdict,
	safeTTL time.Duration,
	blockTTL time.Duration,
) error {
	if c == nil || c.rdb == nil {
		return errors.New("content moderation chunk cache is unavailable")
	}
	if !isContentModerationCacheDigest(namespace) {
		return errors.New("invalid content moderation chunk namespace")
	}
	if safeTTL <= 0 || blockTTL <= 0 {
		return errors.New("content moderation chunk cache TTL must be positive")
	}
	if len(verdicts) == 0 {
		return nil
	}
	type encodedVerdict struct {
		key   string
		value []byte
		ttl   time.Duration
	}
	encoded := make([]encodedVerdict, 0, len(verdicts))
	for chunkHash, verdict := range verdicts {
		if !isContentModerationCacheDigest(chunkHash) {
			return errors.New("invalid content moderation chunk hash")
		}
		if !validContentModerationChunkVerdict(verdict) {
			return errors.New("invalid content moderation chunk verdict")
		}
		value, err := json.Marshal(verdict)
		if err != nil {
			return err
		}
		ttl := safeTTL
		if verdict.Flagged {
			ttl = blockTTL
		}
		encoded = append(encoded, encodedVerdict{
			key:   contentModerationChunkCacheKey(namespace, chunkHash),
			value: value,
			ttl:   ttl,
		})
	}
	_, err := c.rdb.Pipelined(ctx, func(pipe redis.Pipeliner) error {
		for _, item := range encoded {
			pipe.Set(ctx, item.key, item.value, item.ttl)
		}
		return nil
	})
	return err
}

func contentModerationChunkCacheKey(namespace string, chunkHash string) string {
	return contentModerationChunkCacheKeyPrefix + namespace + ":" + chunkHash
}

func isContentModerationCacheDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, char := range value {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}

func validContentModerationChunkVerdict(verdict service.ContentModerationChunkVerdict) bool {
	if len(verdict.CategoryScores) == 0 {
		return false
	}
	for category, score := range verdict.CategoryScores {
		if strings.TrimSpace(category) == "" || math.IsNaN(score) || math.IsInf(score, 0) || score < 0 || score > 1 {
			return false
		}
	}
	return true
}
