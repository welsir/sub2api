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

func TestPromptAuditRepositoryCreatePersistsAllFields(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	createdAt := time.Date(2026, 7, 20, 2, 30, 0, 0, time.UTC)
	groupID := int64(9)
	log := &service.PromptAuditLog{
		RequestID:   "req-1",
		UserID:      11,
		APIKeyID:    15,
		GroupID:     &groupID,
		Endpoint:    "/v1/responses",
		Protocol:    service.ContentModerationProtocolOpenAIResponses,
		Model:       "gpt-5.6-sol",
		PromptText:  "你好，audit",
		PromptChars: 8,
	}

	mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO prompt_audit_logs (
    request_id, user_id, api_key_id, group_id, endpoint,
    protocol, model, prompt_text, prompt_chars
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING id, created_at`)).
		WithArgs("req-1", int64(11), int64(15), int64(9), "/v1/responses", service.ContentModerationProtocolOpenAIResponses, "gpt-5.6-sol", "你好，audit", 8).
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at"}).AddRow(int64(101), createdAt))

	repo := NewPromptAuditRepository(db)
	require.NoError(t, repo.Create(context.Background(), log))
	require.Equal(t, int64(101), log.ID)
	require.Equal(t, createdAt, log.CreatedAt)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestPromptAuditRepositoryCreateUsesNullGroupAndWrapsErrors(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	log := &service.PromptAuditLog{RequestID: "req-2", UserID: 1, APIKeyID: 2, PromptText: "prompt", PromptChars: 6}
	dbErr := errors.New("database unavailable")
	mock.ExpectQuery("INSERT INTO prompt_audit_logs").
		WithArgs("req-2", int64(1), int64(2), nil, "", "", "", "prompt", 6).
		WillReturnError(dbErr)

	repo := NewPromptAuditRepository(db)
	err = repo.Create(context.Background(), log)
	require.ErrorContains(t, err, "insert prompt audit log")
	require.ErrorIs(t, err, dbErr)
	require.NoError(t, mock.ExpectationsWereMet())
}
