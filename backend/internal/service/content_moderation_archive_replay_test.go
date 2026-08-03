// [INPUT]: Synthetic and opt-in archived upstream request bodies.
// [OUTPUT]: Privacy-safe moderation archive replay coverage and aggregate test metrics.
// [POS]: Test-only bridge from exact archived payloads to the full moderation decision path.
// [PROTOCOL]: Keep replay data local and never log prompts, identifiers, credentials, or request bodies.

package service

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/require"
)

const (
	contentModerationReplayMaxBodyBytes  = 64 << 20
	contentModerationReplayMaxRows       = 200
	contentModerationReplayMaxZstdLayers = 3
)

type contentModerationReplayRow struct {
	Endpoint        string
	Protocol        string
	Model           string
	Body            []byte
	Hash            [sha256.Size]byte
	Runes           int
	Items           int
	Attachments     int
	AttachmentOnly  bool
	Tool            bool
	Continue        bool
	ConfirmedPolicy bool
}

type contentModerationReplaySummary struct {
	Rows             int
	Allowed          int
	Blocked          int
	Errors           int
	AttachmentRows   int
	AttachmentOnly   int
	AttachmentTool   int
	AttachmentLong   int
	BodiesPreserved  int
	Tool             int
	MultiTurn        int
	Continue         int
	Long12K          int
	Long32K          int
	Long64K          int
	AdapterCalls     int64
	TextOnlyCalls    int64
	MarkerCalls      int64
	RawMetadataCalls int64
	GzipCalls        int64
	ConfirmedPolicy  int
	ProtocolCount    map[string]int
	Latencies        []time.Duration
}

type contentModerationPromptReplayRow struct {
	Protocol string
	Model    string
	Prompt   string
	Hash     [sha256.Size]byte
	Runes    int
	Category string
	Continue bool
}

func TestContentModerationArchiveReplay_DecodesSyntheticNestedZstdCSV(t *testing.T) {
	body := []byte(`{"model":"gpt-test","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"synthetic archive replay"},{"type":"input_file","file_id":"file-synthetic-opaque","filename":"synthetic.pdf"}]}]}`)
	responseBody := []byte(`{"error":{"code":"cyber_policy"}}`)
	encoder, err := zstd.NewWriter(nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = encoder.Close() })
	nested := encoder.EncodeAll(encoder.EncodeAll(body, nil), nil)

	var archive bytes.Buffer
	gzipWriter := gzip.NewWriter(&archive)
	csvWriter := csv.NewWriter(gzipWriter)
	require.NoError(t, csvWriter.Write([]string{"endpoint", "protocol", "model", "request_body_zstd", "response_status", "response_body_zstd"}))
	require.NoError(t, csvWriter.Write([]string{
		"/v1/responses",
		"openai_responses",
		"gpt-test",
		base64.StdEncoding.EncodeToString(nested),
		"200",
		base64.StdEncoding.EncodeToString(encoder.EncodeAll(responseBody, nil)),
	}))
	csvWriter.Flush()
	require.NoError(t, csvWriter.Error())
	require.NoError(t, gzipWriter.Close())

	rows, err := readContentModerationReplayArchive(bytes.NewReader(archive.Bytes()))
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, ContentModerationProtocolOpenAIResponses, rows[0].Protocol)
	require.Equal(t, "/v1/responses", rows[0].Endpoint)
	require.JSONEq(t, string(body), string(rows[0].Body))
	require.True(t, rows[0].ConfirmedPolicy)
	require.Equal(t, 1, rows[0].Attachments)
	require.False(t, rows[0].AttachmentOnly)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(rows[0].Body, &parsed))
	require.Equal(t, "gpt-test", parsed["model"])
}

func TestContentModerationArchiveReplay_ExactPolicyTokenExcludesLocalSessionBlock(t *testing.T) {
	require.True(t, contentModerationReplayHasExactToken(`{"code":"cyber_policy"}`, "cyber_policy"))
	require.False(t, contentModerationReplayHasExactToken(`{"code":"cyber_policy_session_blocked"}`, "cyber_policy"))
}

func TestContentModerationArchiveReplay_V1AttachmentProjectionThroughDecisionPath(t *testing.T) {
	longText := strings.Repeat("安", 70_000)
	longBody, err := json.Marshal(map[string]any{
		"messages": []map[string]any{{
			"role": "user",
			"content": []map[string]any{
				{"type": "text", "text": longText},
				{"type": "image_url", "image_url": map[string]any{"url": "https://archive-replay.invalid/long.png?token=synthetic"}},
			},
		}},
	})
	require.NoError(t, err)

	rows := []contentModerationReplayRow{
		newSyntheticContentModerationReplayRow(
			ContentModerationProtocolOpenAIChat,
			"/v1/chat/completions",
			[]byte(`{"messages":[{"role":"user","content":[{"type":"text","text":"synthetic normal attachment"},{"type":"image_url","image_url":{"url":"https://archive-replay.invalid/normal.png?token=synthetic"}}]}]}`),
		),
		newSyntheticContentModerationReplayRow(
			ContentModerationProtocolOpenAIResponses,
			"/v1/responses",
			[]byte(`{"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"SYNTHETIC_ARCHIVE_DANGEROUS_TEXT"},{"type":"input_file","file_id":"file-synthetic-opaque","filename":"synthetic.pdf"}]}]}`),
		),
		newSyntheticContentModerationReplayRow(
			ContentModerationProtocolAnthropicMessages,
			"/v1/messages",
			[]byte(`{"messages":[{"role":"user","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"QUJD"}}]}]}`),
		),
		newSyntheticContentModerationReplayRow(
			ContentModerationProtocolOpenAIResponses,
			"/v1/responses",
			[]byte(`{"input":[{"type":"function_call","call_id":"call_synthetic","name":"inspect_synthetic","arguments":"{}"},{"type":"function_call_output","call_id":"call_synthetic","output":{"asset":{"url":"https://archive-replay.invalid/tool.pdf?token=synthetic"},"status":"synthetic tool output"}}]}`),
		),
		newSyntheticContentModerationReplayRow(
			ContentModerationProtocolOpenAIChat,
			"/v1/chat/completions",
			longBody,
		),
	}

	summary := runContentModerationReplayDecisionPath(t, rows)

	require.Equal(t, len(rows), summary.Rows)
	require.Equal(t, 4, summary.Allowed)
	require.Equal(t, 1, summary.Blocked)
	require.Zero(t, summary.Errors)
	require.Equal(t, 5, summary.AttachmentRows)
	require.Equal(t, 1, summary.AttachmentOnly)
	require.Equal(t, 1, summary.AttachmentTool)
	require.Equal(t, 1, summary.AttachmentLong)
	require.Equal(t, len(rows), summary.BodiesPreserved)
	require.Equal(t, summary.AdapterCalls, summary.TextOnlyCalls)
	require.GreaterOrEqual(t, summary.MarkerCalls, int64(summary.AttachmentRows))
	require.Zero(t, summary.RawMetadataCalls)
	require.Greater(t, summary.GzipCalls, int64(0))
}

func newSyntheticContentModerationReplayRow(protocol string, endpoint string, body []byte) contentModerationReplayRow {
	input := ExtractContentModerationInput(protocol, body)
	attachments, attachmentOnly := contentModerationReplayProjectedAttachmentStats(input.Text)
	return contentModerationReplayRow{
		Endpoint:       endpoint,
		Protocol:       protocol,
		Model:          "synthetic-replay-model",
		Body:           body,
		Hash:           sha256.Sum256(body),
		Runes:          len([]rune(input.Text)),
		Items:          contentModerationReplayItemCount(protocol, body),
		Attachments:    attachments,
		AttachmentOnly: attachmentOnly,
		Tool:           contentModerationReplayHasTool(body),
	}
}

func TestContentModerationArchiveReplay_LocalArchiveThroughFullDecisionPath(t *testing.T) {
	archivePath := strings.TrimSpace(os.Getenv("SUB2API_MODERATION_REPLAY_ARCHIVE"))
	if archivePath == "" {
		t.Skip("set SUB2API_MODERATION_REPLAY_ARCHIVE to an upstream_audit_logs.csv.gz archive")
	}
	paths, err := contentModerationReplayArchivePaths(archivePath)
	require.NoError(t, err)
	require.NotEmpty(t, paths)
	if len(paths) != 1 {
		t.Skip("set SUB2API_MODERATION_REPLAY_ARCHIVE to one archive; the root-wide truth scan uses TestContentModerationArchiveReplay_ConfirmedPolicyArchiveRootThroughFullDecisionPath")
	}
	var rows []contentModerationReplayRow
	for _, path := range paths {
		file, openErr := os.Open(path)
		if openErr != nil {
			require.FailNow(t, "open replay archive failed")
		}
		archiveRows, readErr := readContentModerationReplayArchiveLimit(file, contentModerationReplayMaxRows)
		require.NoError(t, file.Close())
		require.NoError(t, readErr)
		rows = append(rows, archiveRows...)
		rows = selectContentModerationReplayRows(rows, contentModerationReplayMaxRows)
	}
	require.NotEmpty(t, rows)
	summary := runContentModerationReplayDecisionPath(t, rows)
	p50, p95 := contentModerationReplayLatencyPercentiles(summary.Latencies)
	t.Logf(
		"archive replay aggregate: archives=%d rows=%d protocols=%v allowed=%d blocked=%d errors=%d attachments=%d attachment_only=%d attachment_tool=%d attachment_long=%d bodies_preserved=%d confirmed_cyber_policy=%d tool=%d items_21_plus=%d continue=%d long_12k=%d long_32k=%d long_64k=%d adapter_calls=%d text_only_calls=%d marker_calls=%d gzip_calls=%d p50=%s p95=%s",
		len(paths),
		summary.Rows,
		summary.ProtocolCount,
		summary.Allowed,
		summary.Blocked,
		summary.Errors,
		summary.AttachmentRows,
		summary.AttachmentOnly,
		summary.AttachmentTool,
		summary.AttachmentLong,
		summary.BodiesPreserved,
		summary.ConfirmedPolicy,
		summary.Tool,
		summary.MultiTurn,
		summary.Continue,
		summary.Long12K,
		summary.Long32K,
		summary.Long64K,
		summary.AdapterCalls,
		summary.TextOnlyCalls,
		summary.MarkerCalls,
		summary.GzipCalls,
		p50.Round(time.Microsecond),
		p95.Round(time.Microsecond),
	)
}

func TestContentModerationArchiveReplay_ConfirmedPolicyArchiveRootThroughFullDecisionPath(t *testing.T) {
	archivePath := strings.TrimSpace(os.Getenv("SUB2API_MODERATION_REPLAY_ARCHIVE"))
	if archivePath == "" {
		t.Skip("set SUB2API_MODERATION_REPLAY_ARCHIVE to an archive root containing upstream_audit_logs.csv.gz files")
	}
	paths, err := contentModerationReplayArchivePaths(archivePath)
	require.NoError(t, err)
	require.NotEmpty(t, paths)
	pathInfo, err := os.Stat(archivePath)
	if err != nil {
		require.FailNow(t, "stat replay archive failed")
	}
	var rows []contentModerationReplayRow
	for _, path := range paths {
		file, openErr := os.Open(path)
		if openErr != nil {
			require.FailNow(t, "open replay archive failed")
		}
		archiveRows, readErr := readContentModerationConfirmedPolicyReplayArchive(file)
		require.NoError(t, file.Close())
		require.NoError(t, readErr)
		rows = append(rows, archiveRows...)
	}
	if len(rows) == 0 && !pathInfo.IsDir() {
		t.Skip("the selected archive contains no confirmed cyber_policy response")
	}
	require.NotEmpty(t, rows, "the selected archive root must contain confirmed cyber_policy responses")
	summary := runContentModerationReplayDecisionPath(t, rows)
	require.Equal(t, len(rows), summary.ConfirmedPolicy)
	p50, p95 := contentModerationReplayLatencyPercentiles(summary.Latencies)
	t.Logf(
		"confirmed policy replay aggregate: archives=%d rows=%d protocols=%v allowed=%d blocked=%d errors=%d attachments=%d attachment_only=%d attachment_tool=%d attachment_long=%d bodies_preserved=%d adapter_calls=%d text_only_calls=%d marker_calls=%d gzip_calls=%d p50=%s p95=%s",
		len(paths), summary.Rows, summary.ProtocolCount, summary.Allowed, summary.Blocked, summary.Errors,
		summary.AttachmentRows, summary.AttachmentOnly, summary.AttachmentTool, summary.AttachmentLong,
		summary.BodiesPreserved, summary.AdapterCalls, summary.TextOnlyCalls, summary.MarkerCalls,
		summary.GzipCalls, p50.Round(time.Microsecond), p95.Round(time.Microsecond),
	)
}

func TestContentModerationArchiveReplay_ConfirmedPolicyArchiveRootThroughRealAdapter(t *testing.T) {
	archivePath := strings.TrimSpace(os.Getenv("SUB2API_MODERATION_REPLAY_ARCHIVE"))
	adapterURL := strings.TrimSpace(os.Getenv("SUB2API_MODERATION_REAL_ADAPTER_URL"))
	adapterKey := strings.TrimSpace(os.Getenv("SUB2API_MODERATION_REAL_ADAPTER_KEY"))
	if archivePath == "" || adapterURL == "" || adapterKey == "" {
		t.Skip("set replay archive, real adapter URL, and real adapter key to run hosted semantic replay")
	}
	revision := strings.TrimSpace(os.Getenv("SUB2API_MODERATION_REAL_CLASSIFIER_REVISION"))
	if revision == "" {
		revision = "minimax-strict-policy-v5"
	}

	paths, err := contentModerationReplayArchivePaths(archivePath)
	require.NoError(t, err)
	require.NotEmpty(t, paths)
	var rows []contentModerationReplayRow
	for _, path := range paths {
		file, openErr := os.Open(path)
		if openErr != nil {
			require.FailNow(t, "open replay archive failed")
		}
		archiveRows, readErr := readContentModerationConfirmedPolicyReplayArchive(file)
		require.NoError(t, file.Close())
		require.NoError(t, readErr)
		rows = append(rows, archiveRows...)
	}
	require.NotEmpty(t, rows, "the selected archive root must contain confirmed cyber_policy responses")
	rows = filterContentModerationRealReplayRows(
		rows,
		strings.TrimSpace(os.Getenv("SUB2API_MODERATION_REAL_ROW_HASH_PREFIXES")),
	)
	require.NotEmpty(t, rows, "the real replay row filter must match at least one confirmed policy request")

	cfg := defaultContentModerationConfig()
	cfg.BaseURL = adapterURL
	cfg.APIKeys = []string{adapterKey}
	cfg.TimeoutMS = maxContentModerationTimeoutMS
	cfg.ClassifierPolicyRevision = revision
	svc, repo := newIncrementalContentModerationTestService(t, cfg, &contentModerationTestChunkCache{})
	svc.invalidateContentModerationClassifierPolicy(cfg)

	summary := runContentModerationReplayAgainstRealAdapter(t, svc, repo, rows)
	p50, p95 := contentModerationReplayLatencyPercentiles(summary.Latencies)
	maxLatency := summary.Latencies[len(summary.Latencies)-1]
	t.Logf(
		"real adapter confirmed policy replay aggregate: archives=%d rows=%d protocols=%v allowed=%d blocked=%d errors=%d attachments=%d tool=%d items_21_plus=%d continue=%d adapter_request_p50=%s adapter_request_p95=%s adapter_request_max=%s",
		len(paths), summary.Rows, summary.ProtocolCount, summary.Allowed, summary.Blocked,
		summary.Errors, summary.AttachmentRows, summary.Tool, summary.MultiTurn, summary.Continue,
		p50.Round(time.Millisecond), p95.Round(time.Millisecond), maxLatency.Round(time.Millisecond),
	)
	require.Zero(t, summary.Errors, "confirmed policy replay must not fail closed from adapter errors")
	require.Equal(t, summary.Rows, summary.Blocked, "every confirmed OAI cyber_policy request must be blocked locally")
}

func TestContentModerationArchiveReplay_ConfirmedPolicyInventoryMetadata(t *testing.T) {
	archivePath := strings.TrimSpace(os.Getenv("SUB2API_MODERATION_REPLAY_ARCHIVE"))
	if archivePath == "" {
		t.Skip("set SUB2API_MODERATION_REPLAY_ARCHIVE to inspect confirmed policy metadata")
	}
	paths, err := contentModerationReplayArchivePaths(archivePath)
	require.NoError(t, err)
	var rows []contentModerationReplayRow
	for _, path := range paths {
		file, openErr := os.Open(path)
		if openErr != nil {
			require.FailNow(t, "open replay archive failed")
		}
		archiveRows, readErr := readContentModerationConfirmedPolicyReplayArchive(file)
		require.NoError(t, file.Close())
		require.NoError(t, readErr)
		rows = append(rows, archiveRows...)
	}
	require.NotEmpty(t, rows)
	sort.Slice(rows, func(i, j int) bool { return rows[i].Runes > rows[j].Runes })
	for _, row := range rows {
		t.Logf(
			"confirmed policy metadata: row_hash_prefix=%s protocol=%s runes=%d items=%d body_bytes=%d attachments=%d tool=%t",
			hex.EncodeToString(row.Hash[:6]), row.Protocol, row.Runes, row.Items,
			len(row.Body), row.Attachments, row.Tool,
		)
	}
}

func runContentModerationReplayAgainstRealAdapter(
	t *testing.T,
	svc *ContentModerationService,
	repo *contentModerationTestRepo,
	rows []contentModerationReplayRow,
) contentModerationReplaySummary {
	t.Helper()
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer slog.SetDefault(previousLogger)

	summary := contentModerationReplaySummary{ProtocolCount: make(map[string]int)}
	for _, row := range rows {
		beforeHash := sha256.Sum256(row.Body)
		require.True(t, row.Hash == beforeHash, "archive replay request body digest must match")
		requestID := hex.EncodeToString(row.Hash[:6])
		started := time.Now()
		decision, checkErr := svc.Check(context.Background(), ContentModerationCheckInput{
			RequestID: requestID,
			UserID:    1,
			Endpoint:  row.Endpoint,
			Provider:  "real_adapter_archive_replay",
			Model:     row.Model,
			Protocol:  row.Protocol,
			Body:      row.Body,
		})
		summary.Latencies = append(summary.Latencies, time.Since(started))
		summary.Rows++
		summary.ProtocolCount[row.Protocol]++
		if beforeHash != sha256.Sum256(row.Body) {
			require.FailNow(t, "moderation replay mutated an upstream request body")
		}
		summary.BodiesPreserved++
		if checkErr != nil || decision == nil || decision.Action == ContentModerationActionError {
			summary.Errors++
			errorKind := "decision_error"
			for _, log := range repo.snapshotLogs() {
				if log.RequestID == requestID && log.Action == ContentModerationActionError {
					errorKind = contentModerationReplayErrorKind(log.Error)
				}
			}
			t.Logf(
				"real adapter replay error metadata: row_hash_prefix=%s runes=%d items=%d body_bytes=%d attachments=%d tool=%t error_kind=%s latency=%s",
				requestID, row.Runes, row.Items, len(row.Body), row.Attachments, row.Tool,
				errorKind, summary.Latencies[len(summary.Latencies)-1].Round(time.Millisecond),
			)
		} else if decision.Blocked {
			summary.Blocked++
		} else if decision.Allowed {
			summary.Allowed++
		}
		if row.Attachments > 0 {
			summary.AttachmentRows++
		}
		if row.Tool {
			summary.Tool++
		}
		if row.Items >= 21 {
			summary.MultiTurn++
		}
		if row.Continue {
			summary.Continue++
		}
	}
	require.Equal(t, summary.Rows, summary.Allowed+summary.Blocked+summary.Errors)
	require.Equal(t, summary.Rows, summary.BodiesPreserved)
	return summary
}

func filterContentModerationRealReplayRows(
	rows []contentModerationReplayRow,
	rawPrefixes string,
) []contentModerationReplayRow {
	if rawPrefixes == "" {
		return rows
	}
	prefixes := strings.Split(strings.ToLower(rawPrefixes), ",")
	filtered := make([]contentModerationReplayRow, 0, len(rows))
	for _, row := range rows {
		digest := hex.EncodeToString(row.Hash[:])
		for _, prefix := range prefixes {
			prefix = strings.TrimSpace(prefix)
			if prefix != "" && strings.HasPrefix(digest, prefix) {
				filtered = append(filtered, row)
				break
			}
		}
	}
	return filtered
}

func contentModerationReplayErrorKind(errorText string) string {
	lower := strings.ToLower(errorText)
	for _, candidate := range []string{
		"inference_timeout",
		"adapter_overloaded",
		"backend_billing_failed",
		"backend_auth_failed",
		"model_output_parse_error",
		"moderation_backend_error",
		"classifier policy revision mismatch",
		"context deadline exceeded",
		"moderation api returned empty results",
	} {
		if strings.Contains(lower, candidate) {
			return candidate
		}
	}
	if errorText == "" {
		return "empty_error"
	}
	return "other"
}

func runContentModerationReplayDecisionPath(t *testing.T, rows []contentModerationReplayRow) contentModerationReplaySummary {
	t.Helper()
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer slog.SetDefault(previousLogger)

	var adapterCalls atomic.Int64
	var textOnlyCalls atomic.Int64
	var markerCalls atomic.Int64
	var rawMetadataCalls atomic.Int64
	var gzipCalls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/readyz" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status":                     "ready",
				"classifier_policy_revision": contentModerationTestClassifierPolicyRevision,
			})
			return
		}
		adapterCalls.Add(1)
		if r.Header.Get("Content-Encoding") == "gzip" {
			gzipCalls.Add(1)
		}
		payload := decodeModerationTestRequest(t, r)
		text, ok := payload.Input.(string)
		require.True(t, ok)
		textOnlyCalls.Add(1)
		if strings.Contains(text, "[attachment ") {
			markerCalls.Add(1)
		}
		for _, rawMetadata := range []string{
			"archive-replay.invalid", "file-synthetic-opaque", "token=synthetic", "synthetic.pdf", "QUJD",
		} {
			if strings.Contains(text, rawMetadata) {
				rawMetadataCalls.Add(1)
				break
			}
		}
		score := 0.0
		if strings.Contains(text, "SYNTHETIC_ARCHIVE_DANGEROUS_TEXT") {
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
	cfg.TimeoutMS = 10_000
	cache := &contentModerationTestChunkCache{}
	svc, _ := newIncrementalContentModerationTestService(t, cfg, cache)
	summary := contentModerationReplaySummary{ProtocolCount: make(map[string]int)}

	for _, row := range rows {
		beforeHash := sha256.Sum256(row.Body)
		require.True(t, row.Hash == beforeHash, "archive replay request body digest must match")
		started := time.Now()
		decision, checkErr := svc.Check(context.Background(), ContentModerationCheckInput{
			UserID:   1,
			Endpoint: row.Endpoint,
			Provider: "archive_replay",
			Model:    row.Model,
			Protocol: row.Protocol,
			Body:     row.Body,
		})
		summary.Latencies = append(summary.Latencies, time.Since(started))
		require.NoError(t, checkErr)
		require.NotNil(t, decision)
		require.True(t, beforeHash == sha256.Sum256(row.Body), "moderation replay must not mutate the upstream request body")
		summary.BodiesPreserved++
		summary.Rows++
		summary.ProtocolCount[row.Protocol]++
		if decision.Action == ContentModerationActionError {
			summary.Errors++
		} else if decision.Blocked {
			summary.Blocked++
		} else if decision.Allowed {
			summary.Allowed++
		}
		if row.Attachments > 0 {
			summary.AttachmentRows++
			if row.AttachmentOnly {
				summary.AttachmentOnly++
			}
			if row.Tool {
				summary.AttachmentTool++
			}
			if row.Runes >= 12_000 {
				summary.AttachmentLong++
			}
		}
		if row.Tool {
			summary.Tool++
		}
		if row.Items >= 21 {
			summary.MultiTurn++
		}
		if row.Continue {
			summary.Continue++
		}
		if row.ConfirmedPolicy {
			summary.ConfirmedPolicy++
		}
		switch {
		case row.Runes >= 64_000:
			summary.Long64K++
		case row.Runes >= 32_000:
			summary.Long32K++
		case row.Runes >= 12_000:
			summary.Long12K++
		}
	}

	summary.AdapterCalls = adapterCalls.Load()
	summary.TextOnlyCalls = textOnlyCalls.Load()
	summary.MarkerCalls = markerCalls.Load()
	summary.RawMetadataCalls = rawMetadataCalls.Load()
	summary.GzipCalls = gzipCalls.Load()
	require.Equal(t, summary.Rows, summary.Allowed+summary.Blocked+summary.Errors)
	require.Equal(t, summary.Rows, summary.BodiesPreserved)
	require.Equal(t, summary.AdapterCalls, summary.TextOnlyCalls)
	require.Greater(t, summary.AdapterCalls, int64(0))
	require.Zero(t, summary.Errors, "all archived requests must complete the text-only mock decision path")
	return summary
}

func contentModerationReplayLatencyPercentiles(latencies []time.Duration) (time.Duration, time.Duration) {
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	return latencies[len(latencies)/2], latencies[(len(latencies)-1)*95/100]
}

func TestContentModerationArchiveReplay_LocalPromptCorpusThroughDecisionPath(t *testing.T) {
	root := strings.TrimSpace(os.Getenv("SUB2API_MODERATION_REPLAY_PROMPT_ROOT"))
	if root == "" {
		t.Skip("set SUB2API_MODERATION_REPLAY_PROMPT_ROOT to the local prompt archive root")
	}
	paths, err := filepath.Glob(filepath.Join(root, "2026-07-*", "prompt_audit_logs.csv.gz"))
	require.NoError(t, err)
	require.NotEmpty(t, paths)
	sort.Strings(paths)

	var candidates []contentModerationPromptReplayRow
	for _, path := range paths {
		rows, readErr := readContentModerationPromptReplayArchive(path)
		require.NoError(t, readErr)
		candidates = append(candidates, rows...)
	}
	rows := selectContentModerationPromptReplayRows(candidates)
	require.GreaterOrEqual(t, len(rows), 500)

	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer slog.SetDefault(previousLogger)
	var adapterCalls atomic.Int64
	var gzipCalls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/readyz" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status":                     "ready",
				"classifier_policy_revision": contentModerationTestClassifierPolicyRevision,
			})
			return
		}
		adapterCalls.Add(1)
		if r.Header.Get("Content-Encoding") == "gzip" {
			gzipCalls.Add(1)
		}
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
	cfg.TimeoutMS = 10_000
	svc, _ := newIncrementalContentModerationTestService(t, cfg, &contentModerationTestChunkCache{})
	protocols := make(map[string]int)
	categories := make(map[string]int)
	lengths := make(map[string]int)
	latencies := make([]time.Duration, 0, len(rows))
	continueCount := 0
	for _, row := range rows {
		body := contentModerationPromptReplayBody(row.Protocol, row.Prompt)
		require.NotEmpty(t, body)
		started := time.Now()
		decision, checkErr := svc.Check(context.Background(), ContentModerationCheckInput{
			UserID:   1,
			Endpoint: "local_prompt_replay",
			Provider: "archive_replay",
			Model:    row.Model,
			Protocol: row.Protocol,
			Body:     body,
		})
		latencies = append(latencies, time.Since(started))
		require.NoError(t, checkErr)
		require.NotNil(t, decision)
		require.True(t, decision.Allowed)
		protocols[row.Protocol]++
		categories[row.Category]++
		lengths[contentModerationPromptReplayLengthBucket(row.Runes)]++
		if row.Continue {
			continueCount++
		}
	}
	require.Greater(t, adapterCalls.Load(), int64(0))
	require.Greater(t, gzipCalls.Load(), int64(0))
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	p50 := latencies[len(latencies)/2]
	p95 := latencies[(len(latencies)-1)*95/100]
	t.Logf(
		"prompt replay aggregate: candidates=%d selected=%d protocols=%v categories=%v lengths=%v continue=%d adapter_calls=%d gzip_calls=%d p50=%s p95=%s",
		len(candidates),
		len(rows),
		protocols,
		categories,
		lengths,
		continueCount,
		adapterCalls.Load(),
		gzipCalls.Load(),
		p50.Round(time.Microsecond),
		p95.Round(time.Microsecond),
	)
}

func TestContentModerationArchiveReplay_LocalPromptCorpusThroughRealAdapter(t *testing.T) {
	root := strings.TrimSpace(os.Getenv("SUB2API_MODERATION_REPLAY_PROMPT_ROOT"))
	adapterURL := strings.TrimSpace(os.Getenv("SUB2API_MODERATION_REAL_ADAPTER_URL"))
	adapterKey := strings.TrimSpace(os.Getenv("SUB2API_MODERATION_REAL_ADAPTER_KEY"))
	if root == "" || adapterURL == "" || adapterKey == "" {
		t.Skip("set prompt root, real adapter URL, and real adapter key to run hosted prompt replay")
	}
	revision := strings.TrimSpace(os.Getenv("SUB2API_MODERATION_REAL_CLASSIFIER_REVISION"))
	if revision == "" {
		revision = "minimax-strict-policy-v5"
	}
	paths, err := filepath.Glob(filepath.Join(root, "2026-07-*", "prompt_audit_logs.csv.gz"))
	require.NoError(t, err)
	require.NotEmpty(t, paths)
	sort.Strings(paths)

	var candidates []contentModerationPromptReplayRow
	for _, path := range paths {
		rows, readErr := readContentModerationPromptReplayArchive(path)
		require.NoError(t, readErr)
		candidates = append(candidates, rows...)
	}
	rows := selectContentModerationRealPromptReplayRows(candidates)
	if categoryFilter := strings.TrimSpace(os.Getenv("SUB2API_MODERATION_REAL_PROMPT_CATEGORY")); categoryFilter != "" {
		filtered := rows[:0]
		for _, row := range rows {
			if row.Category == categoryFilter {
				filtered = append(filtered, row)
			}
		}
		rows = filtered
	}
	require.NotEmpty(t, rows)

	cfg := defaultContentModerationConfig()
	cfg.BaseURL = adapterURL
	cfg.APIKeys = []string{adapterKey}
	cfg.TimeoutMS = maxContentModerationTimeoutMS
	cfg.ClassifierPolicyRevision = revision
	svc, _ := newIncrementalContentModerationTestService(t, cfg, &contentModerationTestChunkCache{})
	svc.invalidateContentModerationClassifierPolicy(cfg)

	type categorySummary struct{ rows, allowed, blocked, errors int }
	categories := make(map[string]categorySummary)
	latencies := make([]time.Duration, 0, len(rows))
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer slog.SetDefault(previousLogger)
	logRowMetadata := strings.TrimSpace(os.Getenv("SUB2API_MODERATION_REAL_LOG_ROW_METADATA")) == "1"
	for _, row := range rows {
		body := contentModerationPromptReplayBody(row.Protocol, row.Prompt)
		require.NotEmpty(t, body)
		started := time.Now()
		decision, checkErr := svc.Check(context.Background(), ContentModerationCheckInput{
			RequestID: hex.EncodeToString(row.Hash[:6]),
			UserID:    1,
			Endpoint:  "/real_prompt_replay",
			Provider:  "real_adapter_prompt_replay",
			Model:     row.Model,
			Protocol:  row.Protocol,
			Body:      body,
		})
		latencies = append(latencies, time.Since(started))
		summary := categories[row.Category]
		summary.rows++
		outcome := "allow"
		if checkErr != nil || decision == nil || decision.Action == ContentModerationActionError {
			summary.errors++
			outcome = "error"
		} else if decision.Blocked {
			summary.blocked++
			outcome = "block"
		} else if decision.Allowed {
			summary.allowed++
		}
		categories[row.Category] = summary
		if logRowMetadata {
			t.Logf(
				"real prompt replay metadata: row_hash_prefix=%s category=%s runes=%d protocol=%s outcome=%s",
				hex.EncodeToString(row.Hash[:6]), row.Category, row.Runes, row.Protocol, outcome,
			)
		}
	}
	p50, p95 := contentModerationReplayLatencyPercentiles(latencies)
	for _, category := range []string{
		"high_risk_candidate", "boundary_candidate", "legitimate_reverse_security", "normal_other",
	} {
		summary := categories[category]
		t.Logf(
			"real prompt replay category: category=%s rows=%d allowed=%d blocked=%d errors=%d",
			category, summary.rows, summary.allowed, summary.blocked, summary.errors,
		)
		require.Zero(t, summary.errors, "real prompt replay must not fail from adapter errors")
	}
	t.Logf(
		"real prompt replay aggregate: rows=%d p50=%s p95=%s max=%s",
		len(rows), p50.Round(time.Millisecond), p95.Round(time.Millisecond),
		latencies[len(latencies)-1].Round(time.Millisecond),
	)
}

func readContentModerationReplayArchive(reader io.Reader) ([]contentModerationReplayRow, error) {
	return readContentModerationReplayArchiveLimit(reader, 0)
}

func readContentModerationReplayArchiveLimit(reader io.Reader, limit int) ([]contentModerationReplayRow, error) {
	return readContentModerationReplayArchiveOptions(reader, limit, false)
}

func readContentModerationConfirmedPolicyReplayArchive(reader io.Reader) ([]contentModerationReplayRow, error) {
	return readContentModerationReplayArchiveOptions(reader, 0, true)
}

func readContentModerationReplayArchiveOptions(reader io.Reader, limit int, confirmedPolicyOnly bool) ([]contentModerationReplayRow, error) {
	gzipReader, err := gzip.NewReader(reader)
	if err != nil {
		return nil, errors.New("open replay gzip archive")
	}
	defer gzipReader.Close()
	csvReader := csv.NewReader(gzipReader)
	csvReader.FieldsPerRecord = -1
	header, err := csvReader.Read()
	if err != nil {
		return nil, errors.New("read replay CSV header")
	}
	columns := make(map[string]int, len(header))
	for index, name := range header {
		columns[strings.TrimSpace(name)] = index
	}
	for _, required := range []string{"endpoint", "protocol", "model", "request_body_zstd"} {
		if _, ok := columns[required]; !ok {
			return nil, fmt.Errorf("replay CSV missing required column %q", required)
		}
	}
	decoder, err := zstd.NewReader(nil,
		zstd.WithDecoderMaxMemory(contentModerationReplayMaxBodyBytes),
		zstd.WithDecoderMaxWindow(contentModerationReplayMaxBodyBytes),
	)
	if err != nil {
		return nil, errors.New("create replay zstd decoder")
	}
	defer decoder.Close()

	rows := make([]contentModerationReplayRow, 0, contentModerationReplayMaxRows)
	for rowNumber := 2; ; rowNumber++ {
		record, readErr := csvReader.Read()
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return nil, fmt.Errorf("read replay CSV row %d", rowNumber)
		}
		field := func(name string) string {
			index, ok := columns[name]
			if !ok {
				return ""
			}
			if index < 0 || index >= len(record) {
				return ""
			}
			return strings.TrimSpace(record[index])
		}
		confirmedPolicy, policyErr := contentModerationReplayConfirmedPolicy(
			decoder,
			field("response_status"),
			field("outcome"),
			field("error_message"),
			field("response_body_zstd"),
		)
		if policyErr != nil {
			return nil, fmt.Errorf("decode replay response at row %d: %w", rowNumber, policyErr)
		}
		if confirmedPolicyOnly && !confirmedPolicy {
			continue
		}
		protocol := contentModerationReplayProtocol(field("protocol"), field("endpoint"))
		if protocol == "" || field("request_body_zstd") == "" {
			continue
		}
		compressed, decodeErr := decodeContentModerationReplayBytea(field("request_body_zstd"))
		if decodeErr != nil {
			return nil, fmt.Errorf("decode replay body at row %d", rowNumber)
		}
		if len(compressed) == 0 {
			continue
		}
		body, decodeErr := decodeContentModerationReplayZstdLayers(decoder, compressed)
		if decodeErr != nil {
			return nil, fmt.Errorf("decompress replay body at row %d: %w", rowNumber, decodeErr)
		}
		if !json.Valid(body) {
			return nil, fmt.Errorf("replay body at row %d is not JSON", rowNumber)
		}
		input := ExtractContentModerationInput(protocol, body)
		if input.IsEmpty() {
			continue
		}
		items := contentModerationReplayItemCount(protocol, body)
		attachments, attachmentOnly := contentModerationReplayProjectedAttachmentStats(input.Text)
		rows = append(rows, contentModerationReplayRow{
			Endpoint:        field("endpoint"),
			Protocol:        protocol,
			Model:           field("model"),
			Body:            body,
			Hash:            sha256.Sum256(body),
			Runes:           len([]rune(input.Text)),
			Items:           items,
			Attachments:     attachments,
			AttachmentOnly:  attachmentOnly,
			Tool:            contentModerationReplayHasTool(body),
			Continue:        contentModerationReplayHasContinue(protocol, body),
			ConfirmedPolicy: confirmedPolicy,
		})
		if limit > 0 && len(rows) >= limit+16 {
			rows = append([]contentModerationReplayRow(nil), selectContentModerationReplayRows(rows, limit)...)
		}
	}
	if limit > 0 {
		rows = selectContentModerationReplayRows(rows, limit)
	}
	return rows, nil
}

func contentModerationReplayArchivePaths(path string) ([]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, errors.New("stat replay archive path")
	}
	if !info.IsDir() {
		return []string{path}, nil
	}
	var paths []string
	err = filepath.Walk(path, func(candidate string, fileInfo os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !fileInfo.IsDir() && fileInfo.Name() == "upstream_audit_logs.csv.gz" {
			paths = append(paths, candidate)
		}
		return nil
	})
	if err != nil {
		return nil, errors.New("walk replay archive path")
	}
	sort.Strings(paths)
	return paths, nil
}

func contentModerationReplayConfirmedPolicy(decoder *zstd.Decoder, _ string, outcome string, errorMessage string, encodedBody string) (bool, error) {
	if contentModerationReplayHasExactToken(outcome, "cyber_policy") || contentModerationReplayHasExactToken(errorMessage, "cyber_policy") {
		return true, nil
	}
	if encodedBody == "" {
		return false, nil
	}
	compressed, err := decodeContentModerationReplayBytea(encodedBody)
	if err != nil {
		return false, errors.New("decode response bytea")
	}
	body, err := decodeContentModerationReplayZstdLayers(decoder, compressed)
	if err != nil {
		return false, errors.New("decompress response body")
	}
	return contentModerationReplayHasExactToken(string(body), "cyber_policy"), nil
}

func contentModerationReplayHasExactToken(value string, expected string) bool {
	for _, token := range strings.FieldsFunc(strings.ToLower(value), func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') && r != '_'
	}) {
		if token == expected {
			return true
		}
	}
	return false
}

func decodeContentModerationReplayBytea(value string) ([]byte, error) {
	if strings.HasPrefix(value, `\x`) {
		return hex.DecodeString(value[2:])
	}
	if decoded, err := base64.StdEncoding.DecodeString(value); err == nil {
		return decoded, nil
	}
	return base64.RawStdEncoding.DecodeString(value)
}

func decodeContentModerationReplayZstdLayers(decoder *zstd.Decoder, value []byte) ([]byte, error) {
	current := value
	for layer := 0; layer < contentModerationReplayMaxZstdLayers && hasContentModerationReplayZstdMagic(current); layer++ {
		decoded, err := decoder.DecodeAll(current, nil)
		if err != nil {
			return nil, errors.New("invalid zstd layer")
		}
		if len(decoded) > contentModerationReplayMaxBodyBytes {
			return nil, errors.New("decompressed body exceeds replay limit")
		}
		current = decoded
	}
	if hasContentModerationReplayZstdMagic(current) {
		return nil, errors.New("too many zstd layers")
	}
	if len(current) > contentModerationReplayMaxBodyBytes {
		return nil, errors.New("replay body exceeds limit")
	}
	return current, nil
}

func hasContentModerationReplayZstdMagic(value []byte) bool {
	return len(value) >= 4 && bytes.Equal(value[:4], []byte{0x28, 0xb5, 0x2f, 0xfd})
}

func contentModerationReplayProtocol(protocol string, endpoint string) string {
	protocol = strings.TrimSpace(strings.ToLower(protocol))
	switch protocol {
	case ContentModerationProtocolOpenAIResponses, ContentModerationProtocolOpenAIChat,
		ContentModerationProtocolAnthropicMessages, ContentModerationProtocolGemini,
		ContentModerationProtocolOpenAIImages:
		return protocol
	}
	endpoint = strings.ToLower(strings.TrimSpace(endpoint))
	switch {
	case strings.Contains(endpoint, "/responses"):
		return ContentModerationProtocolOpenAIResponses
	case strings.Contains(endpoint, "/chat/completions"):
		return ContentModerationProtocolOpenAIChat
	case strings.Contains(endpoint, "/messages"):
		return ContentModerationProtocolAnthropicMessages
	case strings.Contains(endpoint, "generatecontent"):
		return ContentModerationProtocolGemini
	case strings.Contains(endpoint, "/images"):
		return ContentModerationProtocolOpenAIImages
	default:
		return ""
	}
}

func contentModerationReplayItemCount(protocol string, body []byte) int {
	var root map[string]json.RawMessage
	if json.Unmarshal(body, &root) != nil {
		return 0
	}
	key := "input"
	switch protocol {
	case ContentModerationProtocolOpenAIChat, ContentModerationProtocolAnthropicMessages:
		key = "messages"
	case ContentModerationProtocolGemini:
		key = "contents"
	}
	var items []json.RawMessage
	if json.Unmarshal(root[key], &items) != nil {
		return 1
	}
	return len(items)
}

func contentModerationReplayHasTool(body []byte) bool {
	lower := strings.ToLower(string(body))
	for _, marker := range []string{`"tool_calls"`, `"tool_use"`, `"tool_result"`, `"function_call"`, `"functioncall"`, `"functionresponse"`} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func contentModerationReplayProjectedAttachmentStats(text string) (int, bool) {
	// This is an aggregate projection-shape metric only. Classifier interpretation
	// of user-authored marker-like text belongs to the adapter prompt contract.
	remainder := text
	count := 0
	for {
		start := strings.Index(remainder, "[attachment ")
		if start < 0 {
			break
		}
		endOffset := strings.IndexByte(remainder[start:], ']')
		if endOffset < 0 {
			break
		}
		end := start + endOffset + 1
		remainder = remainder[:start] + " " + remainder[end:]
		count++
	}
	for _, role := range []string{
		"[system]", "[developer]", "[instructions]", "[user]", "[assistant]", "[tool]",
	} {
		remainder = strings.ReplaceAll(remainder, role, " ")
	}
	return count, count > 0 && strings.TrimSpace(remainder) == ""
}

func contentModerationReplayHasContinue(protocol string, body []byte) bool {
	var root map[string]any
	if json.Unmarshal(body, &root) != nil {
		return false
	}
	key := "input"
	switch protocol {
	case ContentModerationProtocolOpenAIChat, ContentModerationProtocolAnthropicMessages:
		key = "messages"
	case ContentModerationProtocolGemini:
		key = "contents"
	}
	if value, ok := root[key].(string); ok {
		return contentModerationReplayIsContinue(value)
	}
	items, ok := root[key].([]any)
	if !ok {
		return false
	}
	for index := len(items) - 1; index >= 0; index-- {
		item, ok := items[index].(map[string]any)
		if !ok || strings.ToLower(strings.TrimSpace(fmt.Sprint(item["role"]))) != "user" {
			continue
		}
		content := item["content"]
		if protocol == ContentModerationProtocolGemini {
			content = item["parts"]
		}
		var values []string
		collectContentModerationReplayStrings(content, &values)
		return contentModerationReplayIsContinue(strings.Join(values, " "))
	}
	return false
}

func collectContentModerationReplayStrings(value any, out *[]string) {
	switch typed := value.(type) {
	case string:
		*out = append(*out, typed)
	case []any:
		for _, item := range typed {
			collectContentModerationReplayStrings(item, out)
		}
	case map[string]any:
		for _, key := range []string{"text", "content"} {
			if nested, ok := typed[key]; ok {
				collectContentModerationReplayStrings(nested, out)
			}
		}
	}
}

func contentModerationReplayIsContinue(value string) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.Trim(normalized, "。.!！?？")
	switch normalized {
	case "continue", "继续", "继续生成", "继续吧", "接着", "接着来":
		return true
	default:
		return false
	}
}

func selectContentModerationReplayRows(rows []contentModerationReplayRow, limit int) []contentModerationReplayRow {
	if limit <= 0 || len(rows) <= limit {
		return rows
	}
	sorted := append([]contentModerationReplayRow(nil), rows...)
	sort.Slice(sorted, func(i, j int) bool {
		return bytes.Compare(sorted[i].Hash[:], sorted[j].Hash[:]) < 0
	})
	selected := make([]contentModerationReplayRow, 0, limit)
	seen := make(map[[sha256.Size]byte]struct{}, limit)
	add := func(predicate func(contentModerationReplayRow) bool, categoryLimit int) {
		count := 0
		for _, row := range sorted {
			if len(selected) >= limit || count >= categoryLimit {
				return
			}
			if _, ok := seen[row.Hash]; ok || !predicate(row) {
				continue
			}
			seen[row.Hash] = struct{}{}
			selected = append(selected, row)
			count++
		}
	}
	add(func(row contentModerationReplayRow) bool { return row.ConfirmedPolicy }, limit)
	add(func(row contentModerationReplayRow) bool { return row.Continue }, 15)
	add(func(row contentModerationReplayRow) bool { return row.AttachmentOnly }, 10)
	add(func(row contentModerationReplayRow) bool { return row.Attachments > 0 && row.Tool }, 20)
	add(func(row contentModerationReplayRow) bool { return row.Attachments > 0 && row.Runes >= 12_000 }, 20)
	add(func(row contentModerationReplayRow) bool { return row.Attachments > 0 }, 30)
	for _, protocol := range []string{
		ContentModerationProtocolOpenAIResponses,
		ContentModerationProtocolOpenAIChat,
		ContentModerationProtocolAnthropicMessages,
		ContentModerationProtocolGemini,
		ContentModerationProtocolOpenAIImages,
	} {
		protocol := protocol
		add(func(row contentModerationReplayRow) bool { return row.Protocol == protocol && len(row.Body) <= 1<<20 }, 10)
	}
	add(func(row contentModerationReplayRow) bool { return len(row.Body) > 8<<20 }, 2)
	add(func(row contentModerationReplayRow) bool { return len(row.Body) > 1<<20 && len(row.Body) <= 8<<20 }, 8)
	add(func(row contentModerationReplayRow) bool { return row.Tool && len(row.Body) <= 1<<20 }, 25)
	add(func(row contentModerationReplayRow) bool { return row.Items >= 21 && len(row.Body) <= 1<<20 }, 25)
	for _, bucket := range []struct {
		predicate func(contentModerationReplayRow) bool
		limit     int
	}{
		{predicate: func(row contentModerationReplayRow) bool { return row.Runes < 12_000 && len(row.Body) <= 1<<20 }, limit: 20},
		{predicate: func(row contentModerationReplayRow) bool {
			return row.Runes >= 12_000 && row.Runes < 32_000 && len(row.Body) <= 1<<20
		}, limit: 20},
		{predicate: func(row contentModerationReplayRow) bool {
			return row.Runes >= 32_000 && row.Runes < 64_000 && len(row.Body) <= 1<<20
		}, limit: 20},
		{predicate: func(row contentModerationReplayRow) bool { return row.Runes >= 64_000 && len(row.Body) <= 1<<20 }, limit: 25},
	} {
		add(bucket.predicate, bucket.limit)
	}
	add(func(row contentModerationReplayRow) bool { return len(row.Body) <= 1<<20 }, limit)
	add(func(contentModerationReplayRow) bool { return true }, limit)
	return selected
}

func readContentModerationPromptReplayArchive(path string) ([]contentModerationPromptReplayRow, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("open prompt replay archive")
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return nil, errors.New("open prompt replay gzip")
	}
	defer gzipReader.Close()
	csvReader := csv.NewReader(gzipReader)
	csvReader.FieldsPerRecord = -1
	header, err := csvReader.Read()
	if err != nil {
		return nil, errors.New("read prompt replay header")
	}
	columns := make(map[string]int, len(header))
	for index, name := range header {
		columns[strings.TrimSpace(name)] = index
	}
	for _, required := range []string{"endpoint", "protocol", "model", "prompt_text"} {
		if _, ok := columns[required]; !ok {
			return nil, fmt.Errorf("prompt replay CSV missing required column %q", required)
		}
	}
	rows := make([]contentModerationPromptReplayRow, 0, 4096)
	for rowNumber := 2; ; rowNumber++ {
		record, readErr := csvReader.Read()
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return nil, fmt.Errorf("read prompt replay row %d", rowNumber)
		}
		field := func(name string) string {
			index := columns[name]
			if index < 0 || index >= len(record) {
				return ""
			}
			return strings.TrimSpace(record[index])
		}
		prompt := field("prompt_text")
		protocol := contentModerationReplayProtocol(field("protocol"), field("endpoint"))
		if prompt == "" || protocol == "" {
			continue
		}
		digest := sha256.Sum256([]byte(protocol + "\x00" + field("model") + "\x00" + prompt))
		rows = append(rows, contentModerationPromptReplayRow{
			Protocol: protocol,
			Model:    field("model"),
			Prompt:   prompt,
			Hash:     digest,
			Runes:    len([]rune(prompt)),
			Category: contentModerationPromptReplayCategory(prompt),
			Continue: contentModerationReplayIsContinue(prompt),
		})
	}
	return rows, nil
}

func selectContentModerationPromptReplayRows(candidates []contentModerationPromptReplayRow) []contentModerationPromptReplayRow {
	sorted := append([]contentModerationPromptReplayRow(nil), candidates...)
	sort.Slice(sorted, func(i, j int) bool { return bytes.Compare(sorted[i].Hash[:], sorted[j].Hash[:]) < 0 })
	selected := make([]contentModerationPromptReplayRow, 0, 1400)
	seen := make(map[[sha256.Size]byte]struct{}, 1400)
	add := func(predicate func(contentModerationPromptReplayRow) bool, limit int) {
		count := 0
		for _, row := range sorted {
			if count >= limit {
				return
			}
			if _, ok := seen[row.Hash]; ok || !predicate(row) {
				continue
			}
			seen[row.Hash] = struct{}{}
			selected = append(selected, row)
			count++
		}
	}
	for _, bucket := range []struct {
		name  string
		limit int
	}{
		{name: "high_risk_candidate", limit: 200},
		{name: "boundary_candidate", limit: 150},
		{name: "legitimate_reverse_security", limit: 200},
		{name: "normal_other", limit: 300},
	} {
		bucket := bucket
		add(func(row contentModerationPromptReplayRow) bool { return row.Category == bucket.name }, bucket.limit)
	}
	add(func(row contentModerationPromptReplayRow) bool { return row.Continue }, 100)
	for _, bucket := range []string{"12k_32k", "32k_64k", "64k_plus"} {
		bucket := bucket
		add(func(row contentModerationPromptReplayRow) bool {
			return contentModerationPromptReplayLengthBucket(row.Runes) == bucket
		}, 70)
	}
	for _, protocol := range []string{
		ContentModerationProtocolAnthropicMessages,
		ContentModerationProtocolGemini,
		ContentModerationProtocolOpenAIImages,
	} {
		protocol := protocol
		add(func(row contentModerationPromptReplayRow) bool { return row.Protocol == protocol }, 100)
	}
	return selected
}

func selectContentModerationRealPromptReplayRows(candidates []contentModerationPromptReplayRow) []contentModerationPromptReplayRow {
	sorted := append([]contentModerationPromptReplayRow(nil), candidates...)
	sort.Slice(sorted, func(i, j int) bool { return bytes.Compare(sorted[i].Hash[:], sorted[j].Hash[:]) < 0 })
	selected := make([]contentModerationPromptReplayRow, 0, 40)
	seen := make(map[[sha256.Size]byte]struct{}, 40)
	add := func(predicate func(contentModerationPromptReplayRow) bool, limit int) {
		count := 0
		for _, row := range sorted {
			if count >= limit {
				return
			}
			if _, ok := seen[row.Hash]; ok || !predicate(row) {
				continue
			}
			seen[row.Hash] = struct{}{}
			selected = append(selected, row)
			count++
		}
	}
	for _, category := range []string{
		"high_risk_candidate", "boundary_candidate", "legitimate_reverse_security", "normal_other",
	} {
		category := category
		add(func(row contentModerationPromptReplayRow) bool {
			return row.Category == category && row.Runes <= 12_000
		}, 8)
	}
	add(func(row contentModerationPromptReplayRow) bool { return row.Continue }, 2)
	for _, bucket := range []string{"12k_32k", "32k_64k", "64k_plus"} {
		bucket := bucket
		add(func(row contentModerationPromptReplayRow) bool {
			return contentModerationPromptReplayLengthBucket(row.Runes) == bucket
		}, 2)
	}
	return selected
}

func contentModerationPromptReplayCategory(prompt string) string {
	lower := strings.ToLower(prompt)
	highRisk := containsAnyContentModerationReplayTerm(lower, []string{
		"绕过", "破解", "卡密", "漏洞利用", "攻击链", "提权", "shellcode", "rce", "payload", "木马", "恶意软件", "dma", "反作弊", "credential theft", "authorization bypass",
	}) && containsAnyContentModerationReplayTerm(lower, []string{
		"实现", "代码", "教程", "移除", "规避", "执行", "攻击", "加载", "注入", "窃取", "bypass", "exploit", "execute",
	})
	legitimate := containsAnyContentModerationReplayTerm(lower, []string{
		"逆向", "反编译", "漏洞", "安全", "恶意软件", "反作弊", "渗透", "ctf", "reverse engineering",
	}) && containsAnyContentModerationReplayTerm(lower, []string{
		"授权", "研究", "分析", "检测", "防御", "修复", "审计", "课程", "竞赛", "合法", "defensive", "authorized",
	})
	switch {
	case highRisk && legitimate:
		return "boundary_candidate"
	case highRisk:
		return "high_risk_candidate"
	case legitimate:
		return "legitimate_reverse_security"
	default:
		return "normal_other"
	}
}

func containsAnyContentModerationReplayTerm(text string, terms []string) bool {
	for _, term := range terms {
		if strings.Contains(text, term) {
			return true
		}
	}
	return false
}

func contentModerationPromptReplayLengthBucket(runes int) string {
	switch {
	case runes >= 64_000:
		return "64k_plus"
	case runes >= 32_000:
		return "32k_64k"
	case runes >= 12_000:
		return "12k_32k"
	case runes >= 4_000:
		return "4k_12k"
	case runes >= 1_000:
		return "1k_4k"
	case runes >= 256:
		return "256_1k"
	default:
		return "under_256"
	}
}

func contentModerationPromptReplayBody(protocol string, prompt string) []byte {
	var payload any
	switch protocol {
	case ContentModerationProtocolOpenAIResponses:
		payload = map[string]any{"input": prompt}
	case ContentModerationProtocolOpenAIChat, ContentModerationProtocolAnthropicMessages:
		payload = map[string]any{"messages": []map[string]any{{"role": "user", "content": prompt}}}
	case ContentModerationProtocolGemini:
		payload = map[string]any{"contents": []map[string]any{{"role": "user", "parts": []map[string]any{{"text": prompt}}}}}
	case ContentModerationProtocolOpenAIImages:
		payload = map[string]any{"prompt": prompt}
	default:
		return nil
	}
	body, _ := json.Marshal(payload)
	return body
}
