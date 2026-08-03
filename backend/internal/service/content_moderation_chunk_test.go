package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const contentModerationTestClassifierPolicyRevision = "minimax-strict-policy-v3"

func TestContentModerationConfig_ExposesExpectedClassifierPolicyRevision(t *testing.T) {
	var cfg ContentModerationConfig
	require.NoError(t, json.Unmarshal(
		[]byte(`{"classifier_policy_revision":"minimax-strict-policy-v3"}`),
		&cfg,
	))

	require.Equal(t, "minimax-strict-policy-v3", cfg.ClassifierPolicyRevision)
}

func TestContentModerationChunks_EmptyAndShortInput(t *testing.T) {
	require.Empty(t, splitContentModerationChunks("", contentModerationTestClassifierPolicyRevision))
	require.Empty(t, splitContentModerationChunks("   ", contentModerationTestClassifierPolicyRevision))

	chunks := splitContentModerationChunks("你好，世界", contentModerationTestClassifierPolicyRevision)
	require.Len(t, chunks, 1)
	require.Equal(t, "你好，世界", chunks[0].Text)
	require.Len(t, chunks[0].Hash, 64)
}

func TestContentModerationChunks_UseRuneBoundariesAndExactOverlap(t *testing.T) {
	runes := make([]rune, contentModerationChunkSizeRunes+37)
	for index := range runes {
		runes[index] = rune('一' + index%2000)
	}
	input := string(runes)

	chunks := splitContentModerationChunks(input, contentModerationTestClassifierPolicyRevision)
	require.Len(t, chunks, 2)

	first := []rune(chunks[0].Text)
	second := []rune(chunks[1].Text)
	require.Len(t, first, contentModerationChunkSizeRunes)
	require.Equal(
		t,
		first[len(first)-contentModerationChunkOverlapRunes:],
		second[:contentModerationChunkOverlapRunes],
	)
	require.Equal(t, runes, append(first, second[contentModerationChunkOverlapRunes:]...))
}

func TestContentModerationChunks_ExactBoundaryDoesNotCreateEmptyTail(t *testing.T) {
	input := strings.Repeat("界", contentModerationChunkSizeRunes)

	chunks := splitContentModerationChunks(input, contentModerationTestClassifierPolicyRevision)

	require.Len(t, chunks, 1)
	require.Equal(t, input, chunks[0].Text)
}

func TestContentModerationChunks_AppendingKeepsCompletedPrefixStable(t *testing.T) {
	base := strings.Repeat("a", contentModerationChunkSizeRunes*2+777)
	extended := base + strings.Repeat("b", contentModerationChunkSizeRunes)

	before := splitContentModerationChunks(base, contentModerationTestClassifierPolicyRevision)
	after := splitContentModerationChunks(extended, contentModerationTestClassifierPolicyRevision)

	require.Greater(t, len(before), 1)
	require.Greater(t, len(after), len(before))
	for index := 0; index < len(before)-1; index++ {
		require.Equal(t, before[index].Hash, after[index].Hash)
		require.Equal(t, before[index].Text, after[index].Text)
	}
	require.NotEqual(t, before[len(before)-1].Hash, after[len(before)-1].Hash)
}

func TestContentModerationChunkPolicyNamespace_IsCanonicalAndVersioned(t *testing.T) {
	first := defaultContentModerationConfig()
	first.BaseURL = "http://adapter.internal/"
	first.Model = "omni-moderation-latest"
	first.ClassifierPolicyRevision = contentModerationTestClassifierPolicyRevision
	first.Thresholds = map[string]float64{
		"sexual":  0.65,
		"illicit": 0.95,
	}
	second := cloneContentModerationConfig(first)
	second.Thresholds = map[string]float64{
		"illicit": 0.95,
		"sexual":  0.65,
	}

	namespace := contentModerationChunkPolicyNamespace(first)
	require.Len(t, namespace, 64)
	require.Equal(t, namespace, contentModerationChunkPolicyNamespace(second))

	second.Model = "other-model"
	require.NotEqual(t, namespace, contentModerationChunkPolicyNamespace(second))

	second = cloneContentModerationConfig(first)
	second.BaseURL = "http://other-adapter.internal"
	require.NotEqual(t, namespace, contentModerationChunkPolicyNamespace(second))

	second = cloneContentModerationConfig(first)
	second.Thresholds["illicit"] = 0.5
	require.NotEqual(t, namespace, contentModerationChunkPolicyNamespace(second))

	second = cloneContentModerationConfig(first)
	second.ClassifierPolicyRevision = "minimax-strict-policy-v4"
	require.NotEqual(t, namespace, contentModerationChunkPolicyNamespace(second))
}

func TestContentModerationChunkCacheRevision_CoversProjectionAndClassifierPolicy(t *testing.T) {
	const (
		legacyProjectionRevision = "incremental-full-context-v1"
		projectionRevision       = "incremental-full-context-v2-attachment-text-only"
		classifierRevision       = "minimax-strict-policy-v3"
	)
	text := "stable synthetic chunk"

	expectedHashSum := sha256.Sum256([]byte(projectionRevision + "\x00" + classifierRevision + "\x00" + text))
	expectedHash := hex.EncodeToString(expectedHashSum[:])
	legacyHashSum := sha256.Sum256([]byte(legacyProjectionRevision + "\x00" + text))
	legacyHash := hex.EncodeToString(legacyHashSum[:])
	require.Equal(t, expectedHash, contentModerationChunkHash(text, classifierRevision))
	require.NotEqual(t, legacyHash, contentModerationChunkHash(text, classifierRevision))

	expectedNamespaceSum := sha256.Sum256([]byte(projectionRevision + "\n" + classifierRevision + "\n32768:1024\n\n"))
	expectedNamespace := hex.EncodeToString(expectedNamespaceSum[:])
	legacyNamespaceSum := sha256.Sum256([]byte(legacyProjectionRevision + "\n32768:1024"))
	legacyNamespace := hex.EncodeToString(legacyNamespaceSum[:])
	cfg := &ContentModerationConfig{ClassifierPolicyRevision: classifierRevision}
	require.Equal(t, expectedNamespace, contentModerationChunkPolicyNamespace(cfg))
	require.NotEqual(t, legacyNamespace, contentModerationChunkPolicyNamespace(cfg))
}

func TestContentModerationChunks_CoverOperationalSizesWithoutGaps(t *testing.T) {
	testCases := []struct {
		name       string
		size       int
		wantChunks int
	}{
		{name: "12k", size: 12_100, wantChunks: 1},
		{name: "32k boundary", size: 32_768, wantChunks: 1},
		{name: "70k", size: 70_000, wantChunks: 3},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			input := strings.Repeat("甲", testCase.size)
			chunks := splitContentModerationChunks(input, contentModerationTestClassifierPolicyRevision)

			require.Len(t, chunks, testCase.wantChunks)
			reconstructed := []rune(chunks[0].Text)
			for index := 1; index < len(chunks); index++ {
				current := []rune(chunks[index].Text)
				require.GreaterOrEqual(t, len(current), contentModerationChunkOverlapRunes)
				require.Equal(t, reconstructed[len(reconstructed)-contentModerationChunkOverlapRunes:], current[:contentModerationChunkOverlapRunes])
				reconstructed = append(reconstructed, current[contentModerationChunkOverlapRunes:]...)
			}
			require.Equal(t, []rune(input), reconstructed)
		})
	}
}

func TestContentModerationChunks_OverlapKeepsRiskMarkerCrossingBoundaryIntact(t *testing.T) {
	marker := "SYNTHETIC_BOUNDARY_RISK"
	markerRunes := []rune(marker)
	input := strings.Repeat("安", contentModerationChunkSizeRunes-len(markerRunes)/2) + marker + strings.Repeat("全", 2_048)

	chunks := splitContentModerationChunks(input, contentModerationTestClassifierPolicyRevision)

	require.Len(t, chunks, 2)
	require.Contains(t, chunks[1].Text, marker, "overlap must carry the complete marker into a reviewable chunk")
}

func TestContentModerationClassifierPolicy_SlowKeyDoesNotBlockAnotherKey(t *testing.T) {
	aEntered := make(chan struct{})
	releaseA := make(chan struct{})
	var releaseAOnce sync.Once
	defer releaseAOnce.Do(func() { close(releaseA) })
	serverA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(aEntered)
		<-releaseA
		writeContentModerationReadinessTestResponse(t, w, contentModerationTestClassifierPolicyRevision)
	}))
	defer serverA.Close()

	bEntered := make(chan struct{})
	serverB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(bEntered)
		writeContentModerationReadinessTestResponse(t, w, contentModerationTestClassifierPolicyRevision)
	}))
	defer serverB.Close()

	svc := &ContentModerationService{
		httpClient: http.DefaultClient,
	}
	cfgA := &ContentModerationConfig{BaseURL: serverA.URL, ClassifierPolicyRevision: contentModerationTestClassifierPolicyRevision}
	cfgB := &ContentModerationConfig{BaseURL: serverB.URL, ClassifierPolicyRevision: contentModerationTestClassifierPolicyRevision}
	aDone := make(chan error, 1)
	go func() { aDone <- svc.ensureContentModerationClassifierPolicy(context.Background(), cfgA) }()
	<-aEntered

	bDone := make(chan error, 1)
	go func() { bDone <- svc.ensureContentModerationClassifierPolicy(context.Background(), cfgB) }()
	select {
	case err := <-bDone:
		require.NoError(t, err)
	case <-time.After(300 * time.Millisecond):
		releaseAOnce.Do(func() { close(releaseA) })
		<-aDone
		t.Fatal("slow readiness key A blocked unrelated key B")
	}
	select {
	case <-bEntered:
	default:
		t.Fatal("key B never reached its readiness endpoint")
	}

	releaseAOnce.Do(func() { close(releaseA) })
	require.NoError(t, <-aDone)
}

func TestContentModerationClassifierPolicy_SameKeyCoalescesOneReadinessCall(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			close(entered)
		}
		<-release
		writeContentModerationReadinessTestResponse(t, w, contentModerationTestClassifierPolicyRevision)
	}))
	defer server.Close()

	svc := &ContentModerationService{
		httpClient: http.DefaultClient,
	}
	cfg := &ContentModerationConfig{BaseURL: server.URL, ClassifierPolicyRevision: contentModerationTestClassifierPolicyRevision}
	const callers = 8
	start := make(chan struct{})
	errs := make(chan error, callers)
	for index := 0; index < callers; index++ {
		go func() {
			<-start
			errs <- svc.ensureContentModerationClassifierPolicy(context.Background(), cfg)
		}()
	}
	close(start)
	<-entered
	close(release)
	for index := 0; index < callers; index++ {
		require.NoError(t, <-errs)
	}
	require.Equal(t, int64(1), calls.Load())
}

func TestContentModerationClassifierPolicy_WaiterHonorsOwnDeadline(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(entered)
		<-release
		writeContentModerationReadinessTestResponse(t, w, contentModerationTestClassifierPolicyRevision)
	}))
	defer server.Close()

	svc := &ContentModerationService{
		httpClient: http.DefaultClient,
	}
	cfg := &ContentModerationConfig{BaseURL: server.URL, ClassifierPolicyRevision: contentModerationTestClassifierPolicyRevision}
	leaderDone := make(chan error, 1)
	go func() { leaderDone <- svc.ensureContentModerationClassifierPolicy(context.Background(), cfg) }()
	<-entered

	waiterCtx, cancelWaiter := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancelWaiter()
	waiterDone := make(chan error, 1)
	go func() { waiterDone <- svc.ensureContentModerationClassifierPolicy(waiterCtx, cfg) }()
	select {
	case err := <-waiterDone:
		require.ErrorIs(t, err, context.DeadlineExceeded)
	case <-time.After(300 * time.Millisecond):
		releaseOnce.Do(func() { close(release) })
		<-leaderDone
		t.Fatal("same-key readiness waiter ignored its context deadline")
	}

	releaseOnce.Do(func() { close(release) })
	require.NoError(t, <-leaderDone)
}

func TestContentModerationClassifierPolicy_FailureRetriesAndInvalidateReverifies(t *testing.T) {
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		writeContentModerationReadinessTestResponse(t, w, contentModerationTestClassifierPolicyRevision)
	}))
	defer server.Close()

	svc := &ContentModerationService{
		httpClient: http.DefaultClient,
	}
	cfg := &ContentModerationConfig{BaseURL: server.URL, ClassifierPolicyRevision: contentModerationTestClassifierPolicyRevision}

	require.Error(t, svc.ensureContentModerationClassifierPolicy(context.Background(), cfg))
	require.NoError(t, svc.ensureContentModerationClassifierPolicy(context.Background(), cfg))
	require.NoError(t, svc.ensureContentModerationClassifierPolicy(context.Background(), cfg))
	require.Equal(t, int64(2), calls.Load(), "successful verification must be reused")

	svc.invalidateContentModerationClassifierPolicy(cfg)
	require.NoError(t, svc.ensureContentModerationClassifierPolicy(context.Background(), cfg))
	require.Equal(t, int64(3), calls.Load(), "invalidation must force a fresh readiness probe")
}

func TestContentModerationClassifierPolicy_InvalidateRejectsInFlightSuccess(t *testing.T) {
	firstEntered := make(chan struct{})
	releaseFirst := make(chan struct{})
	secondEntered := make(chan struct{})
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		switch calls.Add(1) {
		case 1:
			close(firstEntered)
			<-releaseFirst
		case 2:
			close(secondEntered)
		}
		writeContentModerationReadinessTestResponse(t, w, contentModerationTestClassifierPolicyRevision)
	}))
	defer server.Close()

	svc := &ContentModerationService{httpClient: http.DefaultClient}
	cfg := &ContentModerationConfig{BaseURL: server.URL, ClassifierPolicyRevision: contentModerationTestClassifierPolicyRevision}
	done := make(chan error, 1)
	go func() {
		done <- svc.ensureContentModerationClassifierPolicy(context.Background(), cfg)
	}()

	<-firstEntered
	svc.invalidateContentModerationClassifierPolicy(cfg)
	close(releaseFirst)

	select {
	case <-secondEntered:
	case err := <-done:
		require.NoError(t, err)
		t.Fatal("an in-flight success from the invalidated generation was accepted without re-probing")
	case <-time.After(500 * time.Millisecond):
		t.Fatal("readiness did not re-probe after invalidating an in-flight success")
	}
	require.NoError(t, <-done)
	require.Equal(t, int64(2), calls.Load())
}

func writeContentModerationReadinessTestResponse(t *testing.T, w http.ResponseWriter, revision string) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	require.NoError(t, json.NewEncoder(w).Encode(map[string]string{
		"status":                     "ready",
		"classifier_policy_revision": revision,
	}))
}
