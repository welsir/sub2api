package repository

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func newContentModerationChunkCacheTest(t *testing.T) (*contentModerationHashCache, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() {
		require.NoError(t, rdb.Close())
	})
	return &contentModerationHashCache{rdb: rdb}, mr
}

func TestContentModerationChunkCache_RoundTripsVerdictsWithSeparateTTLs(t *testing.T) {
	cache, mr := newContentModerationChunkCacheTest(t)
	ctx := context.Background()
	namespace := strings.Repeat("a", 64)
	safeHash := strings.Repeat("b", 64)
	blockHash := strings.Repeat("c", 64)
	safeTTL := 24 * time.Hour
	blockTTL := 30 * 24 * time.Hour

	misses, err := cache.GetContentModerationChunkVerdicts(ctx, namespace, []string{safeHash, blockHash})
	require.NoError(t, err)
	require.Empty(t, misses)

	err = cache.StoreContentModerationChunkVerdicts(
		ctx,
		namespace,
		map[string]service.ContentModerationChunkVerdict{
			safeHash: {
				Flagged: false,
				CategoryScores: map[string]float64{
					"illicit": 0,
				},
			},
			blockHash: {
				Flagged: true,
				CategoryScores: map[string]float64{
					"illicit": 1,
				},
			},
		},
		safeTTL,
		blockTTL,
	)
	require.NoError(t, err)

	got, err := cache.GetContentModerationChunkVerdicts(ctx, namespace, []string{safeHash, blockHash})
	require.NoError(t, err)
	require.Equal(t, false, got[safeHash].Flagged)
	require.Equal(t, map[string]float64{"illicit": 0}, got[safeHash].CategoryScores)
	require.Equal(t, true, got[blockHash].Flagged)
	require.Equal(t, map[string]float64{"illicit": 1}, got[blockHash].CategoryScores)
	require.Equal(t, safeTTL, mr.TTL(contentModerationChunkCacheKey(namespace, safeHash)))
	require.Equal(t, blockTTL, mr.TTL(contentModerationChunkCacheKey(namespace, blockHash)))
}

func TestContentModerationChunkCache_IsolatesNamespacesAndTreatsCorruptionAsMiss(t *testing.T) {
	cache, mr := newContentModerationChunkCacheTest(t)
	ctx := context.Background()
	firstNamespace := strings.Repeat("a", 64)
	secondNamespace := strings.Repeat("d", 64)
	hash := strings.Repeat("b", 64)

	require.NoError(t, cache.StoreContentModerationChunkVerdicts(
		ctx,
		firstNamespace,
		map[string]service.ContentModerationChunkVerdict{
			hash: {Flagged: false, CategoryScores: map[string]float64{"illicit": 0}},
		},
		time.Hour,
		24*time.Hour,
	))

	got, err := cache.GetContentModerationChunkVerdicts(ctx, secondNamespace, []string{hash})
	require.NoError(t, err)
	require.Empty(t, got)

	mr.Set(contentModerationChunkCacheKey(firstNamespace, hash), "{not-json")
	got, err = cache.GetContentModerationChunkVerdicts(ctx, firstNamespace, []string{hash})
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestContentModerationChunkCache_PropagatesRedisAndIdentifierErrors(t *testing.T) {
	cache, mr := newContentModerationChunkCacheTest(t)
	ctx := context.Background()
	namespace := strings.Repeat("a", 64)
	hash := strings.Repeat("b", 64)

	_, err := cache.GetContentModerationChunkVerdicts(ctx, "bad", []string{hash})
	require.Error(t, err)
	err = cache.StoreContentModerationChunkVerdicts(
		ctx,
		namespace,
		map[string]service.ContentModerationChunkVerdict{"bad": {Flagged: false}},
		time.Hour,
		24*time.Hour,
	)
	require.Error(t, err)

	mr.Close()
	_, err = cache.GetContentModerationChunkVerdicts(ctx, namespace, []string{hash})
	require.Error(t, err)
}
