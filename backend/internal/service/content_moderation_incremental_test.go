package service

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

type contentModerationTestRoundTripper func(*http.Request) (*http.Response, error)

func (fn contentModerationTestRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
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
	if cfg.ClassifierPolicyRevision == "" {
		cfg.ClassifierPolicyRevision = contentModerationTestClassifierPolicyRevision
	}
	cfg.KeywordBlockingMode = ContentModerationKeywordModeAPIOnly
	if len(cfg.APIKeys) == 0 {
		cfg.APIKeys = []string{"adapter-token"}
	}
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
	markContentModerationClassifierPolicyVerified(svc, cfg)
	return svc, repo
}

func markContentModerationClassifierPolicyVerified(svc *ContentModerationService, cfg *ContentModerationConfig) {
	svc.classifierPolicyMu.Lock()
	defer svc.classifierPolicyMu.Unlock()
	if svc.classifierPolicyStates == nil {
		svc.classifierPolicyStates = make(map[string]*contentModerationClassifierPolicyState)
	}
	svc.classifierPolicyStates[contentModerationClassifierVerificationKey(cfg)] = &contentModerationClassifierPolicyState{verified: true}
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

func writeContentModerationRevisionTestResponse(
	t *testing.T,
	w http.ResponseWriter,
	revision string,
	score float64,
) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
		"classifier_policy_revision": revision,
		"results": []map[string]any{{
			"flagged":         false,
			"category_scores": map[string]float64{"illicit": score},
		}},
	}))
}

func TestContentModerationIncrementalCache_RequiresExpectedClassifierRevisionBeforeCacheUse(t *testing.T) {
	const expectedRevision = "minimax-strict-policy-v3"
	tests := []struct {
		name              string
		expectedRevision  string
		readinessRevision string
		wantReadiness     int64
	}{
		{name: "missing expected revision"},
		{
			name:              "adapter revision mismatch",
			expectedRevision:  expectedRevision,
			readinessRevision: "minimax-strict-policy-v2",
			wantReadiness:     1,
		},
		{
			name:             "adapter revision missing",
			expectedRevision: expectedRevision,
			wantReadiness:    1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var readinessCalls atomic.Int64
			var moderationCalls atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet && r.URL.Path == "/readyz" {
					readinessCalls.Add(1)
					_ = json.NewEncoder(w).Encode(map[string]string{
						"status":                     "ready",
						"classifier_policy_revision": test.readinessRevision,
					})
					return
				}
				moderationCalls.Add(1)
				writeContentModerationRevisionTestResponse(t, w, test.readinessRevision, 0)
			}))
			defer server.Close()

			cfg := defaultContentModerationConfig()
			cfg.BaseURL = server.URL
			cfg.TimeoutMS = 2_000
			cfg.ClassifierPolicyRevision = test.expectedRevision
			cfg.APIKeys = []string{"adapter-token"}
			cache := &contentModerationTestChunkCache{}
			svc := &ContentModerationService{chunkCache: cache, httpClient: server.Client()}

			result, _, err := svc.callModerationIncremental(
				context.Background(), cfg, "safe prompt", false,
			)

			require.Error(t, err)
			require.Nil(t, result)
			require.Equal(t, test.wantReadiness, readinessCalls.Load())
			require.Zero(t, moderationCalls.Load())
			cache.chunkMu.Lock()
			require.Zero(t, cache.getCalls)
			require.Zero(t, cache.storeCalls)
			cache.chunkMu.Unlock()
		})
	}
}

func TestContentModerationIncrementalCache_RejectsResponseRevisionMismatchWithoutCacheWrite(t *testing.T) {
	const expectedRevision = "minimax-strict-policy-v3"
	var readinessCalls atomic.Int64
	var moderationCalls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/readyz" {
			readinessCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"status":                     "ready",
				"classifier_policy_revision": expectedRevision,
			})
			return
		}
		moderationCalls.Add(1)
		writeContentModerationRevisionTestResponse(t, w, "minimax-strict-policy-v2", 0)
	}))
	defer server.Close()

	cfg := defaultContentModerationConfig()
	cfg.BaseURL = server.URL
	cfg.TimeoutMS = 2_000
	cfg.ClassifierPolicyRevision = expectedRevision
	cfg.APIKeys = []string{"adapter-token"}
	cache := &contentModerationTestChunkCache{}
	svc := &ContentModerationService{chunkCache: cache, httpClient: server.Client()}

	result, _, err := svc.callModerationIncremental(
		context.Background(), cfg, "safe prompt", false,
	)

	require.Error(t, err)
	require.Nil(t, result)
	require.Equal(t, int64(1), readinessCalls.Load())
	require.Equal(t, int64(1), moderationCalls.Load())
	cache.chunkMu.Lock()
	require.Equal(t, 1, cache.getCalls)
	require.Zero(t, cache.storeCalls)
	cache.chunkMu.Unlock()
}

func TestContentModerationIncrementalCache_MatchingRevisionDeduplicatesIdenticalChunks(t *testing.T) {
	const expectedRevision = "minimax-strict-policy-v3"
	var readinessCalls atomic.Int64
	var moderationCalls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/readyz" {
			readinessCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"status":                     "ready",
				"classifier_policy_revision": expectedRevision,
			})
			return
		}
		moderationCalls.Add(1)
		writeContentModerationRevisionTestResponse(t, w, expectedRevision, 0)
	}))
	defer server.Close()

	cfg := defaultContentModerationConfig()
	cfg.BaseURL = server.URL
	cfg.TimeoutMS = 2_000
	cfg.ClassifierPolicyRevision = expectedRevision
	cfg.APIKeys = []string{"adapter-token"}
	cache := &contentModerationTestChunkCache{}
	svc := &ContentModerationService{chunkCache: cache, httpClient: server.Client()}
	duplicateChunks := strings.Repeat(
		"a",
		contentModerationChunkSizeRunes+(contentModerationChunkSizeRunes-contentModerationChunkOverlapRunes),
	)

	first, firstStats, err := svc.callModerationIncremental(
		context.Background(), cfg, duplicateChunks, false,
	)
	require.NoError(t, err)
	require.False(t, first.Flagged)
	require.Equal(t, 2, firstStats.TotalChunks)
	require.Equal(t, 2, firstStats.CacheMisses)
	require.Equal(t, 1, firstStats.ReviewedChunks)

	second, secondStats, err := svc.callModerationIncremental(
		context.Background(), cfg, duplicateChunks, false,
	)
	require.NoError(t, err)
	require.False(t, second.Flagged)
	require.Equal(t, 2, secondStats.CacheHits)
	require.Equal(t, int64(1), readinessCalls.Load())
	require.Equal(t, int64(1), moderationCalls.Load())
}

func TestContentModerationIncrementalCache_RevisionMismatchWinsUnsafeRaceWithoutCacheWrite(t *testing.T) {
	cache := &contentModerationTestChunkCache{}
	cfg := defaultContentModerationConfig()
	cfg.ClassifierPolicyRevision = contentModerationTestClassifierPolicyRevision
	cfg.APIKeys = []string{"adapter-token"}
	cfg.RetryCount = 0

	var calls atomic.Int64
	bothEntered := make(chan struct{})
	transport := contentModerationTestRoundTripper(func(_ *http.Request) (*http.Response, error) {
		call := calls.Add(1)
		if call == 2 {
			close(bothEntered)
		}
		<-bothEntered
		if call == 1 {
			return contentModerationStaticResponse(http.StatusOK, "minimax-strict-policy-v2", 0), nil
		}
		return contentModerationStaticResponse(http.StatusOK, contentModerationTestClassifierPolicyRevision, 1), nil
	})
	svc := &ContentModerationService{
		chunkCache: cache,
		httpClient: &http.Client{Transport: transport},
	}
	markContentModerationClassifierPolicyVerified(svc, cfg)
	input := strings.Repeat("a", contentModerationChunkSizeRunes) + strings.Repeat("b", 2_048)

	result, _, err := svc.callModerationIncremental(context.Background(), cfg, input, false)

	require.ErrorContains(t, err, "classifier policy revision mismatch")
	require.Nil(t, result)
	cache.chunkMu.Lock()
	require.Zero(t, cache.storeCalls, "revision mismatch must win over a concurrent unsafe verdict")
	cache.chunkMu.Unlock()
}

func TestContentModerationIncrementalCache_ProviderErrorWinsUnsafeRaceWithoutCacheWrite(t *testing.T) {
	cache := &contentModerationTestChunkCache{}
	cfg := defaultContentModerationConfig()
	cfg.ClassifierPolicyRevision = contentModerationTestClassifierPolicyRevision
	cfg.APIKeys = []string{"adapter-token"}
	cfg.RetryCount = 0

	var calls atomic.Int64
	bothEntered := make(chan struct{})
	transport := contentModerationTestRoundTripper(func(_ *http.Request) (*http.Response, error) {
		call := calls.Add(1)
		if call == 2 {
			close(bothEntered)
		}
		<-bothEntered
		if call == 1 {
			return contentModerationStaticResponse(http.StatusServiceUnavailable, "", 0), nil
		}
		return contentModerationStaticResponse(http.StatusOK, contentModerationTestClassifierPolicyRevision, 1), nil
	})
	svc := &ContentModerationService{
		chunkCache: cache,
		httpClient: &http.Client{Transport: transport},
	}
	markContentModerationClassifierPolicyVerified(svc, cfg)
	input := strings.Repeat("a", contentModerationChunkSizeRunes) + strings.Repeat("b", 2_048)

	result, _, err := svc.callModerationIncremental(context.Background(), cfg, input, false)

	require.ErrorContains(t, err, "moderation api status 503")
	require.Nil(t, result)
	cache.chunkMu.Lock()
	require.Zero(t, cache.storeCalls, "provider error must win over a concurrent unsafe verdict")
	cache.chunkMu.Unlock()
}

func TestContentModerationIncrementalCache_PureUnsafeCancellationStillStoresAndBlocks(t *testing.T) {
	cache := &contentModerationTestChunkCache{}
	cfg := defaultContentModerationConfig()
	cfg.ClassifierPolicyRevision = contentModerationTestClassifierPolicyRevision
	cfg.APIKeys = []string{"adapter-token"}
	cfg.RetryCount = 0

	var calls atomic.Int64
	bothEntered := make(chan struct{})
	transport := contentModerationTestRoundTripper(func(request *http.Request) (*http.Response, error) {
		call := calls.Add(1)
		if call == 2 {
			close(bothEntered)
		}
		<-bothEntered
		if call == 1 {
			return contentModerationStaticResponse(http.StatusOK, contentModerationTestClassifierPolicyRevision, 1), nil
		}
		<-request.Context().Done()
		return nil, request.Context().Err()
	})
	svc := &ContentModerationService{
		chunkCache: cache,
		httpClient: &http.Client{Transport: transport},
	}
	markContentModerationClassifierPolicyVerified(svc, cfg)
	input := strings.Repeat("a", contentModerationChunkSizeRunes) + strings.Repeat("b", 2_048)

	result, stats, err := svc.callModerationIncremental(context.Background(), cfg, input, false)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Flagged)
	require.Equal(t, 1, stats.ReviewedChunks)
	cache.chunkMu.Lock()
	require.Equal(t, 1, cache.storeCalls, "a strict unsafe verdict retains the existing cache write semantics")
	cache.chunkMu.Unlock()
}

func contentModerationStaticResponse(status int, revision string, score float64) *http.Response {
	body := `{"error":{"message":"synthetic provider failure"}}`
	if status >= http.StatusOK && status < http.StatusMultipleChoices {
		body = fmt.Sprintf(
			`{"classifier_policy_revision":%q,"results":[{"flagged":false,"category_scores":{"illicit":%g}}]}`,
			revision,
			score,
		)
	}
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
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
			ClassifierPolicyRevision: contentModerationTestClassifierPolicyRevision,
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
			ClassifierPolicyRevision: contentModerationTestClassifierPolicyRevision,
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
					ClassifierPolicyRevision: contentModerationTestClassifierPolicyRevision,
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
			ClassifierPolicyRevision: contentModerationTestClassifierPolicyRevision,
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

func TestContentModerationIncrementalCache_SafeTextWithAttachmentUsesTextOnlyProviderCall(t *testing.T) {
	var requestCount atomic.Int64
	var reviewedText string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		payload := decodeModerationTestRequest(t, r)
		var ok bool
		reviewedText, ok = payload.Input.(string)
		require.True(t, ok, "runtime moderation input must stay text-only")
		_ = json.NewEncoder(w).Encode(moderationAPIResponse{
			ClassifierPolicyRevision: contentModerationTestClassifierPolicyRevision,
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
	body := []byte(`{"messages":[{"role":"user","content":[{"type":"text","text":"safe prompt"},{"type":"image_url","image_url":{"url":"https://example.invalid/private.png?signature=synthetic-secret"}}]}]}`)

	decision, err := svc.Check(context.Background(), ContentModerationCheckInput{
		UserID:   1001,
		Endpoint: "/v1/chat/completions",
		Provider: "openai",
		Model:    "gpt-5.5",
		Protocol: ContentModerationProtocolOpenAIChat,
		Body:     body,
	})
	require.NoError(t, err)
	require.True(t, decision.Allowed)
	require.False(t, decision.Blocked)
	require.Equal(t, ContentModerationActionAllow, decision.Action)
	require.Equal(t, int64(1), requestCount.Load())
	require.Contains(t, reviewedText, "safe prompt")
	require.Contains(t, reviewedText, "[attachment kind=image source=remote extension=.png]")
	require.NotContains(t, reviewedText, "synthetic-secret")
	require.NotContains(t, reviewedText, "example.invalid")
	require.Equal(t, 1, cache.getCalls)
	require.Equal(t, 1, cache.storeCalls)
}

func TestContentModerationIncrementalCache_DangerousTextWithAttachmentBlocksAfterTextOnlyProviderCall(t *testing.T) {
	var requestCount atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		payload := decodeModerationTestRequest(t, r)
		reviewedText, ok := payload.Input.(string)
		require.True(t, ok)
		require.Contains(t, reviewedText, "SYNTHETIC_DANGEROUS_TEXT")
		require.Contains(t, reviewedText, "[attachment kind=file source=file_id extension=.pdf]")
		require.NotContains(t, reviewedText, "file-synthetic-opaque")
		_ = json.NewEncoder(w).Encode(moderationAPIResponse{
			ClassifierPolicyRevision: contentModerationTestClassifierPolicyRevision,
			Results: []moderationAPIResult{{
				CategoryScores: map[string]float64{"illicit": 1},
			}},
		})
	}))
	defer server.Close()

	cfg := defaultContentModerationConfig()
	cfg.BaseURL = server.URL
	cache := &contentModerationTestChunkCache{}
	svc, _ := newIncrementalContentModerationTestService(t, cfg, cache)
	body := []byte(`{"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"SYNTHETIC_DANGEROUS_TEXT"},{"type":"input_file","file_id":"file-synthetic-opaque","filename":"private.pdf"}]}]}`)

	decision, err := svc.Check(context.Background(), ContentModerationCheckInput{
		UserID:   1001,
		Endpoint: "/v1/responses",
		Provider: "openai",
		Model:    "gpt-5.5",
		Protocol: ContentModerationProtocolOpenAIResponses,
		Body:     body,
	})

	require.NoError(t, err)
	require.True(t, decision.Blocked)
	require.Equal(t, ContentModerationActionBlock, decision.Action)
	require.Equal(t, int64(1), requestCount.Load())
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
			ClassifierPolicyRevision: contentModerationTestClassifierPolicyRevision,
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

func TestContentModerationIncrementalCache_ReviewsSyntheticRiskInEveryProtocolTranscript(t *testing.T) {
	testCases := []struct {
		name     string
		protocol string
		body     string
	}{
		{
			name:     "responses instructions",
			protocol: ContentModerationProtocolOpenAIResponses,
			body:     `{"instructions":"SYNTHETIC_RISK_MARKER","input":"safe user text"}`,
		},
		{
			name:     "responses tool tail",
			protocol: ContentModerationProtocolOpenAIResponses,
			body:     `{"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"start"}]},{"type":"function_call_output","call_id":"call_1","output":"SYNTHETIC_RISK_MARKER"}]}`,
		},
		{
			name:     "chat system message",
			protocol: ContentModerationProtocolOpenAIChat,
			body:     `{"messages":[{"role":"system","content":"SYNTHETIC_RISK_MARKER"},{"role":"user","content":"safe user text"}]}`,
		},
		{
			name:     "chat assistant tail",
			protocol: ContentModerationProtocolOpenAIChat,
			body:     `{"messages":[{"role":"user","content":"start"},{"role":"assistant","content":"SYNTHETIC_RISK_MARKER"}]}`,
		},
		{
			name:     "anthropic system message",
			protocol: ContentModerationProtocolAnthropicMessages,
			body:     `{"system":"SYNTHETIC_RISK_MARKER","messages":[{"role":"user","content":"safe user text"}]}`,
		},
		{
			name:     "anthropic tool tail",
			protocol: ContentModerationProtocolAnthropicMessages,
			body:     `{"messages":[{"role":"user","content":"start"},{"role":"user","content":[{"type":"tool_result","tool_use_id":"tool_1","content":"SYNTHETIC_RISK_MARKER"}]}]}`,
		},
		{
			name:     "gemini system instruction",
			protocol: ContentModerationProtocolGemini,
			body:     `{"systemInstruction":{"parts":[{"text":"SYNTHETIC_RISK_MARKER"}]},"contents":[{"role":"user","parts":[{"text":"safe user text"}]}]}`,
		},
		{
			name:     "gemini function response tail",
			protocol: ContentModerationProtocolGemini,
			body:     `{"contents":[{"role":"user","parts":[{"text":"start"}]},{"role":"function","parts":[{"functionResponse":{"name":"synthetic_action","response":{"value":"SYNTHETIC_RISK_MARKER"}}}]}]}`,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			var reviewedText string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				payload := decodeModerationTestRequest(t, r)
				var ok bool
				reviewedText, ok = payload.Input.(string)
				require.True(t, ok)
				score := 0.0
				if strings.Contains(reviewedText, "SYNTHETIC_RISK_MARKER") {
					score = 1
				}
				_ = json.NewEncoder(w).Encode(moderationAPIResponse{ClassifierPolicyRevision: contentModerationTestClassifierPolicyRevision, Results: []moderationAPIResult{{
					CategoryScores: map[string]float64{"illicit": score},
				}}})
			}))
			defer server.Close()

			cfg := defaultContentModerationConfig()
			cfg.BaseURL = server.URL
			cfg.TimeoutMS = 2_000
			svc, _ := newIncrementalContentModerationTestService(t, cfg, &contentModerationTestChunkCache{})

			decision, err := svc.Check(context.Background(), ContentModerationCheckInput{
				UserID:   1001,
				Model:    "gpt-5.5",
				Protocol: testCase.protocol,
				Body:     []byte(testCase.body),
			})

			require.NoError(t, err)
			require.True(t, decision.Blocked)
			require.Contains(t, reviewedText, "SYNTHETIC_RISK_MARKER")
		})
	}
}

func TestContentModerationIncrementalCache_BlocksTailRiskAtOperationalSizes(t *testing.T) {
	for _, size := range []int{12_100, 32_768, 70_000} {
		t.Run(fmt.Sprintf("%d_runes", size), func(t *testing.T) {
			var markerSeen atomic.Bool
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				payload := decodeModerationTestRequest(t, r)
				text, ok := payload.Input.(string)
				require.True(t, ok)
				score := 0.0
				if strings.Contains(text, "SYNTHETIC_TAIL_RISK") {
					markerSeen.Store(true)
					score = 1
				}
				_ = json.NewEncoder(w).Encode(moderationAPIResponse{ClassifierPolicyRevision: contentModerationTestClassifierPolicyRevision, Results: []moderationAPIResult{{
					CategoryScores: map[string]float64{"illicit": score},
				}}})
			}))
			defer server.Close()

			cfg := defaultContentModerationConfig()
			cfg.BaseURL = server.URL
			cfg.TimeoutMS = 2_000
			svc, _ := newIncrementalContentModerationTestService(t, cfg, &contentModerationTestChunkCache{})
			prompt := strings.Repeat("甲", size) + " SYNTHETIC_TAIL_RISK"

			decision, err := svc.Check(context.Background(), incrementalOpenAIChatInput(prompt))

			require.NoError(t, err)
			require.True(t, decision.Blocked)
			require.True(t, markerSeen.Load(), "tail marker must reach the moderation provider")
		})
	}
}

func TestContentModerationIncrementalCache_BlocksRiskCrossingChunkBoundary(t *testing.T) {
	marker := "SYNTHETIC_BOUNDARY_RISK"
	markerRunes := []rune(marker)
	chatRolePrefixRunes := len([]rune("[user] "))
	prompt := strings.Repeat("安", contentModerationChunkSizeRunes-chatRolePrefixRunes-len(markerRunes)/2) + marker + strings.Repeat("全", 2_048)
	var reviewedMarker atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload := decodeModerationTestRequest(t, r)
		text, ok := payload.Input.(string)
		require.True(t, ok)
		score := 0.0
		if strings.Contains(text, marker) {
			reviewedMarker.Store(true)
			score = 1
		}
		_ = json.NewEncoder(w).Encode(moderationAPIResponse{ClassifierPolicyRevision: contentModerationTestClassifierPolicyRevision, Results: []moderationAPIResult{{
			CategoryScores: map[string]float64{"illicit": score},
		}}})
	}))
	defer server.Close()

	cfg := defaultContentModerationConfig()
	cfg.BaseURL = server.URL
	cfg.TimeoutMS = 2_000
	svc, _ := newIncrementalContentModerationTestService(t, cfg, &contentModerationTestChunkCache{})

	decision, err := svc.Check(context.Background(), incrementalOpenAIChatInput(prompt))

	require.NoError(t, err)
	require.True(t, decision.Blocked)
	require.True(t, reviewedMarker.Load())
}

func TestContentModerationIncrementalCache_AdapterFailureRetriesThenFailsClosedWithoutCacheWrite(t *testing.T) {
	var requestCount atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/readyz" {
			_ = json.NewEncoder(w).Encode(map[string]string{
				"status":                     "ready",
				"classifier_policy_revision": contentModerationTestClassifierPolicyRevision,
			})
			return
		}
		requestCount.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":{"message":"synthetic adapter failure"}}`))
	}))
	defer server.Close()

	cfg := defaultContentModerationConfig()
	cfg.Enabled = true
	cfg.Mode = ContentModerationModePreBlock
	cfg.IncrementalCacheEnabled = true
	cfg.ClassifierPolicyRevision = contentModerationTestClassifierPolicyRevision
	cfg.KeywordBlockingMode = ContentModerationKeywordModeAPIOnly
	cfg.BaseURL = server.URL
	cfg.APIKeys = []string{"adapter-token"}
	cfg.TimeoutMS = 2_000
	cfg.RetryCount = 2
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	cache := &contentModerationTestChunkCache{}
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

	decision, err := svc.Check(context.Background(), incrementalOpenAIChatInput("safe synthetic prompt"))

	require.NoError(t, err)
	require.True(t, decision.Blocked)
	require.Equal(t, ContentModerationActionError, decision.Action)
	require.Equal(t, http.StatusServiceUnavailable, decision.StatusCode)
	require.Equal(t, int64(3), requestCount.Load())
	require.Zero(t, cache.storeCalls, "an uncertain provider result must never enter the verdict cache")
	logs := requireContentModerationLogCount(t, repo, 1)
	require.Equal(t, ContentModerationActionError, logs[0].Action)
}

func TestContentModerationCheck_ProjectionDepthFailureFailsClosedBeforeProvider(t *testing.T) {
	var requestCount atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	deep := any("SYNTHETIC_DANGEROUS_TEXT")
	for index := 0; index < 70; index++ {
		deep = map[string]any{"next": deep}
	}
	body, err := json.Marshal(map[string]any{"messages": []any{map[string]any{"role": "tool", "content": deep}}})
	require.NoError(t, err)

	cfg := defaultContentModerationConfig()
	cfg.BaseURL = server.URL
	svc, repo := newIncrementalContentModerationTestService(t, cfg, &contentModerationTestChunkCache{})
	decision, err := svc.Check(context.Background(), ContentModerationCheckInput{
		UserID: 1001, Protocol: ContentModerationProtocolOpenAIChat, Body: body,
	})

	require.NoError(t, err)
	require.True(t, decision.Blocked)
	require.Equal(t, ContentModerationActionError, decision.Action)
	require.Equal(t, http.StatusServiceUnavailable, decision.StatusCode)
	require.Zero(t, requestCount.Load())
	logs := requireContentModerationLogCount(t, repo, 1)
	require.Equal(t, ContentModerationActionError, logs[0].Action)
}

func TestContentModerationCheck_StringifiedArgumentsDepthFailureFailsClosedBeforeProvider(t *testing.T) {
	var requestCount atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	deep := any("SYNTHETIC_DANGEROUS_TEXT")
	for index := 0; index < contentModerationProjectionMaxDepth+1; index++ {
		deep = map[string]any{"next": deep}
	}
	arguments, err := json.Marshal(deep)
	require.NoError(t, err)
	body, err := json.Marshal(map[string]any{
		"messages": []map[string]any{{
			"role": "assistant",
			"tool_calls": []map[string]any{{
				"type":     "function",
				"function": map[string]any{"name": "inspect", "arguments": string(arguments)},
			}},
		}},
	})
	require.NoError(t, err)

	cfg := defaultContentModerationConfig()
	cfg.BaseURL = server.URL
	svc, repo := newIncrementalContentModerationTestService(t, cfg, &contentModerationTestChunkCache{})
	decision, err := svc.Check(context.Background(), ContentModerationCheckInput{
		UserID: 1001, Protocol: ContentModerationProtocolOpenAIChat, Body: body,
	})

	require.NoError(t, err)
	require.True(t, decision.Blocked)
	require.Equal(t, ContentModerationActionError, decision.Action)
	require.Equal(t, http.StatusServiceUnavailable, decision.StatusCode)
	require.Zero(t, requestCount.Load())
	logs := requireContentModerationLogCount(t, repo, 1)
	require.Equal(t, ContentModerationActionError, logs[0].Action)
}

func TestContentModerationCheck_ObserveProjectionFailureAllowsAndRecordsError(t *testing.T) {
	deep := any("SYNTHETIC_DANGEROUS_TEXT")
	for index := 0; index < 70; index++ {
		deep = map[string]any{"next": deep}
	}
	body, err := json.Marshal(map[string]any{"messages": []any{map[string]any{"role": "tool", "content": deep}}})
	require.NoError(t, err)

	cfg := defaultContentModerationConfig()
	cfg.Enabled = true
	cfg.Mode = ContentModerationModeObserve
	cfg.KeywordBlockingMode = ContentModerationKeywordModeAPIOnly
	cfg.APIKeys = []string{"adapter-token"}
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	repo := &contentModerationTestRepo{}
	svc := NewContentModerationService(
		&contentModerationTestSettingRepo{values: map[string]string{
			SettingKeyRiskControlEnabled:      "true",
			SettingKeyContentModerationConfig: string(raw),
		}},
		repo, nil, nil, nil, nil, nil,
	)

	decision, err := svc.Check(context.Background(), ContentModerationCheckInput{
		UserID: 1001, Protocol: ContentModerationProtocolOpenAIChat, Body: body,
	})

	require.NoError(t, err)
	require.True(t, decision.Allowed)
	require.False(t, decision.Blocked)
	require.Equal(t, ContentModerationActionAllow, decision.Action)
	logs := requireContentModerationLogCount(t, repo, 1)
	require.Equal(t, ContentModerationActionError, logs[0].Action)
}
