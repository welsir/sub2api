package service

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type contentModerationTestChunkCache struct {
	contentModerationTestHashCache

	chunkMu       sync.Mutex
	verdicts      map[string]ContentModerationChunkVerdict
	getErr        error
	storeErr      error
	getCalls      int
	storeCalls    int
	requestedHash [][]string
}

func (c *contentModerationTestChunkCache) GetContentModerationChunkVerdicts(
	_ context.Context,
	namespace string,
	chunkHashes []string,
) (map[string]ContentModerationChunkVerdict, error) {
	c.chunkMu.Lock()
	defer c.chunkMu.Unlock()
	c.getCalls++
	c.requestedHash = append(c.requestedHash, append([]string(nil), chunkHashes...))
	if c.getErr != nil {
		return nil, c.getErr
	}
	out := make(map[string]ContentModerationChunkVerdict)
	for _, hash := range chunkHashes {
		if verdict, ok := c.verdicts[namespace+":"+hash]; ok {
			out[hash] = cloneContentModerationChunkVerdict(verdict)
		}
	}
	return out, nil
}

func (c *contentModerationTestChunkCache) StoreContentModerationChunkVerdicts(
	_ context.Context,
	namespace string,
	verdicts map[string]ContentModerationChunkVerdict,
	_ time.Duration,
	_ time.Duration,
) error {
	c.chunkMu.Lock()
	defer c.chunkMu.Unlock()
	c.storeCalls++
	if c.storeErr != nil {
		return c.storeErr
	}
	if c.verdicts == nil {
		c.verdicts = make(map[string]ContentModerationChunkVerdict)
	}
	for hash, verdict := range verdicts {
		c.verdicts[namespace+":"+hash] = cloneContentModerationChunkVerdict(verdict)
	}
	return nil
}

func cloneContentModerationChunkVerdict(verdict ContentModerationChunkVerdict) ContentModerationChunkVerdict {
	out := verdict
	out.CategoryScores = cloneFloatMap(verdict.CategoryScores)
	return out
}

func decodeModerationTestRequest(t *testing.T, request *http.Request) moderationAPIRequest {
	t.Helper()
	reader := io.Reader(request.Body)
	if request.Header.Get("Content-Encoding") == "gzip" {
		gzipReader, err := gzip.NewReader(request.Body)
		require.NoError(t, err)
		defer gzipReader.Close()
		reader = gzipReader
	}
	var payload moderationAPIRequest
	require.NoError(t, json.NewDecoder(reader).Decode(&payload))
	return payload
}

func newIncrementalContentModerationTestService(
	t *testing.T,
	cfg *ContentModerationConfig,
	cache *contentModerationTestChunkCache,
) (*ContentModerationService, *contentModerationTestRepo) {
	return newContentModerationTestServiceWithIncrementalCache(t, cfg, cache, true)
}

func newContentModerationTestServiceWithIncrementalCache(
	t *testing.T,
	cfg *ContentModerationConfig,
	cache *contentModerationTestChunkCache,
	incrementalCacheEnabled bool,
) (*ContentModerationService, *contentModerationTestRepo) {
	t.Helper()
	cfg.Enabled = true
	cfg.Mode = ContentModerationModePreBlock
	cfg.IncrementalCacheEnabled = incrementalCacheEnabled
	cfg.KeywordBlockingMode = ContentModerationKeywordModeAPIOnly
	cfg.APIKeys = []string{"adapter-token"}
	cfg.RetryCount = 0
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	repo := &contentModerationTestRepo{}
	svc := NewContentModerationService(
		&contentModerationTestSettingRepo{values: map[string]string{
			SettingKeyRiskControlEnabled:      "true",
			SettingKeyContentModerationConfig: string(raw),
		}},
		repo,
		cache,
		nil,
		nil,
		nil,
		nil,
	)
	return svc, repo
}

func incrementalOpenAIChatInput(prompt string) ContentModerationCheckInput {
	body, _ := json.Marshal(map[string]any{
		"messages": []map[string]any{{
			"role":    "user",
			"content": prompt,
		}},
	})
	return ContentModerationCheckInput{
		UserID:   1001,
		Endpoint: "/v1/chat/completions",
		Provider: "openai",
		Model:    "gpt-5.5",
		Protocol: ContentModerationProtocolOpenAIChat,
		Body:     body,
	}
}

func TestContentModerationIncrementalCache_ReviewsColdChunksAndOnlyChangedTail(t *testing.T) {
	var requestCount atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		payload := decodeModerationTestRequest(t, r)
		_, ok := payload.Input.(string)
		require.True(t, ok)
		_ = json.NewEncoder(w).Encode(moderationAPIResponse{
			Results: []moderationAPIResult{{
				CategoryScores: map[string]float64{"illicit": 0},
			}},
		})
	}))
	defer server.Close()

	cfg := defaultContentModerationConfig()
	cfg.BaseURL = server.URL
	cfg.TimeoutMS = 2_000
	cache := &contentModerationTestChunkCache{}
	svc, _ := newIncrementalContentModerationTestService(t, cfg, cache)
	base := strings.Repeat("a", contentModerationChunkSizeRunes+2_048)

	first, err := svc.Check(context.Background(), incrementalOpenAIChatInput(base))
	require.NoError(t, err)
	require.True(t, first.Allowed)
	require.Equal(t, int64(2), requestCount.Load())

	second, err := svc.Check(context.Background(), incrementalOpenAIChatInput(base+" appended tail"))
	require.NoError(t, err)
	require.True(t, second.Allowed)
	require.Equal(t, int64(3), requestCount.Load())

	cache.chunkMu.Lock()
	require.Equal(t, 2, cache.getCalls)
	require.Equal(t, 2, cache.storeCalls)
	cache.chunkMu.Unlock()
}

func TestContentModerationIncrementalCache_DangerousHistoryThenContinueUsesCachedBlock(t *testing.T) {
	var requestCount atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		payload := decodeModerationTestRequest(t, r)
		text, ok := payload.Input.(string)
		require.True(t, ok)
		score := 0.0
		if strings.Contains(text, "DANGEROUS_HISTORY") {
			score = 1
		}
		_ = json.NewEncoder(w).Encode(moderationAPIResponse{
			Results: []moderationAPIResult{{
				CategoryScores: map[string]float64{"illicit": score},
			}},
		})
	}))
	defer server.Close()

	cfg := defaultContentModerationConfig()
	cfg.BaseURL = server.URL
	cfg.TimeoutMS = 2_000
	cache := &contentModerationTestChunkCache{}
	svc, _ := newIncrementalContentModerationTestService(t, cfg, cache)
	history := "DANGEROUS_HISTORY " + strings.Repeat("a", contentModerationChunkSizeRunes+2_048)

	first, err := svc.Check(context.Background(), incrementalOpenAIChatInput(history))
	require.NoError(t, err)
	require.True(t, first.Blocked)
	firstCalls := requestCount.Load()
	require.Equal(t, int64(2), firstCalls)

	body, err := json.Marshal(map[string]any{
		"messages": []map[string]any{
			{"role": "user", "content": history},
			{"role": "assistant", "content": "acknowledged"},
			{"role": "user", "content": "Continue"},
		},
	})
	require.NoError(t, err)
	second, err := svc.Check(context.Background(), ContentModerationCheckInput{
		UserID:   1001,
		Model:    "gpt-5.5",
		Protocol: ContentModerationProtocolOpenAIChat,
		Body:     body,
	})
	require.NoError(t, err)
	require.True(t, second.Blocked)
	require.Equal(t, firstCalls, requestCount.Load())
}

func TestContentModerationIncrementalCache_CacheFailuresFailClosed(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		cache     *contentModerationTestChunkCache
		wantCalls int64
	}{
		{
			name:      "read failure",
			cache:     &contentModerationTestChunkCache{getErr: errors.New("redis read failed")},
			wantCalls: 0,
		},
		{
			name:      "write failure",
			cache:     &contentModerationTestChunkCache{storeErr: errors.New("redis write failed")},
			wantCalls: 1,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var requestCount atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requestCount.Add(1)
				_ = json.NewEncoder(w).Encode(moderationAPIResponse{
					Results: []moderationAPIResult{{
						CategoryScores: map[string]float64{"illicit": 0},
					}},
				})
			}))
			defer server.Close()

			cfg := defaultContentModerationConfig()
			cfg.BaseURL = server.URL
			cfg.TimeoutMS = 500
			svc, repo := newIncrementalContentModerationTestService(t, cfg, testCase.cache)

			decision, err := svc.Check(context.Background(), incrementalOpenAIChatInput("safe prompt"))
			require.NoError(t, err)
			require.True(t, decision.Blocked)
			require.Equal(t, ContentModerationActionError, decision.Action)
			require.Equal(t, http.StatusServiceUnavailable, decision.StatusCode)
			require.Equal(t, testCase.wantCalls, requestCount.Load())
			logs := requireContentModerationLogCount(t, repo, 1)
			require.Equal(t, ContentModerationActionError, logs[0].Action)
		})
	}
}

func TestContentModerationIncrementalCache_UsesOneOverallDeadline(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(250 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(moderationAPIResponse{
			Results: []moderationAPIResult{{
				CategoryScores: map[string]float64{"illicit": 0},
			}},
		})
	}))
	defer server.Close()

	cfg := defaultContentModerationConfig()
	cfg.BaseURL = server.URL
	cfg.TimeoutMS = 50
	cache := &contentModerationTestChunkCache{}
	svc, _ := newIncrementalContentModerationTestService(t, cfg, cache)

	started := time.Now()
	decision, err := svc.Check(context.Background(), incrementalOpenAIChatInput("safe prompt"))
	elapsed := time.Since(started)

	require.NoError(t, err)
	require.True(t, decision.Blocked)
	require.Equal(t, ContentModerationActionError, decision.Action)
	require.Less(t, elapsed, 200*time.Millisecond)
}

func TestContentModerationIncrementalCache_StructuredImageFailsClosedWithoutProviderCall(t *testing.T) {
	var requestCount atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		_ = json.NewEncoder(w).Encode(moderationAPIResponse{
			Results: []moderationAPIResult{{
				CategoryScores: map[string]float64{"illicit": 0},
			}},
		})
	}))
	defer server.Close()

	cfg := defaultContentModerationConfig()
	cfg.BaseURL = server.URL
	cache := &contentModerationTestChunkCache{}
	svc, _ := newIncrementalContentModerationTestService(t, cfg, cache)
	body := []byte(`{"messages":[{"role":"user","content":[{"type":"text","text":"safe prompt"},{"type":"image_url","image_url":{"url":"https://example.invalid/image.png"}}]}]}`)

	decision, err := svc.Check(context.Background(), ContentModerationCheckInput{
		UserID:   1001,
		Endpoint: "/v1/chat/completions",
		Provider: "openai",
		Model:    "gpt-5.5",
		Protocol: ContentModerationProtocolOpenAIChat,
		Body:     body,
	})
	require.NoError(t, err)
	require.True(t, decision.Blocked)
	require.Equal(t, ContentModerationActionError, decision.Action)
	require.Zero(t, requestCount.Load())
	require.Zero(t, cache.getCalls)
	require.Zero(t, cache.storeCalls)
}

func TestContentModerationIncrementalCache_DisabledKeepsSingleRequestPath(t *testing.T) {
	var requestCount atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		payload := decodeModerationTestRequest(t, r)
		text, ok := payload.Input.(string)
		require.True(t, ok)
		require.Greater(t, len([]rune(text)), contentModerationChunkSizeRunes)
		_ = json.NewEncoder(w).Encode(moderationAPIResponse{
			Results: []moderationAPIResult{{
				CategoryScores: map[string]float64{"illicit": 0},
			}},
		})
	}))
	defer server.Close()

	cfg := defaultContentModerationConfig()
	cfg.BaseURL = server.URL
	cache := &contentModerationTestChunkCache{}
	svc, _ := newContentModerationTestServiceWithIncrementalCache(t, cfg, cache, false)

	decision, err := svc.Check(
		context.Background(),
		incrementalOpenAIChatInput(strings.Repeat("a", contentModerationChunkSizeRunes+2_048)),
	)
	require.NoError(t, err)
	require.True(t, decision.Allowed)
	require.Equal(t, int64(1), requestCount.Load())
}
