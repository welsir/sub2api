package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type promptAuditRepository struct {
	db *sql.DB
}

func NewPromptAuditRepository(db *sql.DB) service.PromptAuditRepository {
	return &promptAuditRepository{db: db}
}

func (r *promptAuditRepository) Create(ctx context.Context, log *service.UpstreamAuditLog) error {
	if log == nil {
		return nil
	}
	if log.StartedAt.IsZero() {
		log.StartedAt = time.Now()
	}
	if log.CompletedAt.IsZero() {
		log.CompletedAt = log.StartedAt
	}
	createdAt := log.StartedAt
	var groupID any
	if log.GroupID != nil {
		groupID = *log.GroupID
	}
	if _, err := r.db.ExecContext(ctx, "SELECT public.ensure_upstream_audit_partition($1)", createdAt); err != nil {
		return fmt.Errorf("insert upstream audit log: ensure daily partition: %w", err)
	}

	err := r.db.QueryRowContext(ctx, `INSERT INTO public.upstream_audit_logs (
    created_at, request_id, attempt_no, user_id, api_key_id, group_id, account_id,
    endpoint, protocol, model, transport, method, upstream_url,
    request_headers_json, request_content_type, request_sha256, request_bytes,
    request_compressed_bytes, request_body_zstd, response_status,
    response_headers_json, response_content_type, response_sha256, response_bytes,
    response_compressed_bytes, response_body_zstd, response_complete, outcome,
    error_message, completed_at, started_at
)
VALUES (
	    $1, $2, $3, $4, $5, $6, $7,
    $8, $9, $10, $11, $12, $13,
    $14, $15, $16, $17,
    $18, $19, $20,
    $21, $22, $23, $24,
    $25, $26, $27, $28,
	    $29, $30, $1
)
RETURNING id, created_at`,
		createdAt,
		log.RequestID,
		log.AttemptNo,
		log.UserID,
		log.APIKeyID,
		groupID,
		log.AccountID,
		log.Endpoint,
		log.Protocol,
		log.Model,
		log.Transport,
		log.Method,
		log.UpstreamURL,
		log.RequestHeadersJSON,
		log.RequestContentType,
		log.RequestSHA256,
		log.RequestBytes,
		log.RequestCompressedBytes,
		log.RequestBodyZstd,
		log.ResponseStatus,
		log.ResponseHeadersJSON,
		log.ResponseContentType,
		log.ResponseSHA256,
		log.ResponseBytes,
		log.ResponseCompressedBytes,
		log.ResponseBodyZstd,
		log.ResponseComplete,
		log.Outcome,
		log.ErrorMessage,
		log.CompletedAt,
	).Scan(&log.ID, &log.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert upstream audit log: %w", err)
	}
	return nil
}
