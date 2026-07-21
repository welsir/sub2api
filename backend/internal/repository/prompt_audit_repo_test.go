package repository

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestPromptAuditRepositoryCreatePersistsCompleteUpstreamAttempt(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	startedAt := time.Date(2026, 7, 21, 1, 2, 3, 0, time.UTC)
	completedAt := startedAt.Add(2 * time.Second)
	groupID := int64(9)
	log := &service.UpstreamAuditLog{
		RequestID: "req-1", AttemptNo: 2, UserID: 11, APIKeyID: 15, GroupID: &groupID,
		AccountID: 42, Endpoint: "/v1/responses", Protocol: "openai_responses", Model: "gpt-5.6-sol",
		Transport: "http", Method: "POST", UpstreamURL: "https://upstream.example/v1/responses",
		RequestHeadersJSON: `{"content-type":["application/json"]}`, RequestContentType: "application/json",
		RequestSHA256: "request-hash", RequestBytes: 100, RequestCompressedBytes: 50, RequestBodyZstd: []byte("request-zstd"),
		ResponseStatus: 200, ResponseHeadersJSON: `{"content-type":["text/event-stream"]}`, ResponseContentType: "text/event-stream",
		ResponseSHA256: "response-hash", ResponseBytes: 200, ResponseCompressedBytes: 80, ResponseBodyZstd: []byte("response-zstd"),
		ResponseComplete: true, Outcome: service.UpstreamAuditOutcomeCompleted, StartedAt: startedAt, CompletedAt: completedAt,
	}

	mock.ExpectExec(regexp.QuoteMeta("SELECT public.ensure_upstream_audit_partition($1)")).
		WithArgs(startedAt).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO public.upstream_audit_logs (")).
		WithArgs(
			startedAt, "req-1", 2, int64(11), int64(15), int64(9), int64(42), "/v1/responses", "openai_responses", "gpt-5.6-sol",
			"http", "POST", "https://upstream.example/v1/responses", `{"content-type":["application/json"]}`, "application/json",
			"request-hash", int64(100), 50, []byte("request-zstd"), 200, `{"content-type":["text/event-stream"]}`, "text/event-stream",
			"response-hash", int64(200), 80, []byte("response-zstd"), true, service.UpstreamAuditOutcomeCompleted, "", completedAt,
		).
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at"}).AddRow(int64(101), startedAt))

	repo := NewPromptAuditRepository(db)
	require.NoError(t, repo.Create(context.Background(), log))
	require.Equal(t, int64(101), log.ID)
	require.Equal(t, startedAt, log.CreatedAt)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestPromptAuditRepositoryCreateUsesNullGroupAndWrapsErrors(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	startedAt := time.Date(2026, 7, 21, 1, 2, 3, 0, time.UTC)
	log := &service.UpstreamAuditLog{RequestID: "req-2", StartedAt: startedAt}
	dbErr := errors.New("database unavailable")
	mock.ExpectExec("SELECT public.ensure_upstream_audit_partition").WillReturnError(dbErr)

	repo := NewPromptAuditRepository(db)
	err = repo.Create(context.Background(), log)
	require.ErrorContains(t, err, "insert upstream audit log")
	require.ErrorIs(t, err, dbErr)
	require.NoError(t, mock.ExpectationsWereMet())
}
