package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type promptAuditRepository struct {
	db *sql.DB
}

func NewPromptAuditRepository(db *sql.DB) service.PromptAuditRepository {
	return &promptAuditRepository{db: db}
}

func (r *promptAuditRepository) Create(ctx context.Context, log *service.PromptAuditLog) error {
	if log == nil {
		return nil
	}

	var groupID any
	if log.GroupID != nil {
		groupID = *log.GroupID
	}

	err := r.db.QueryRowContext(ctx, `
INSERT INTO prompt_audit_logs (
    request_id, user_id, api_key_id, group_id, endpoint,
    protocol, model, prompt_text, prompt_chars
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING id, created_at`,
		log.RequestID,
		log.UserID,
		log.APIKeyID,
		groupID,
		log.Endpoint,
		log.Protocol,
		log.Model,
		log.PromptText,
		log.PromptChars,
	).Scan(&log.ID, &log.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert prompt audit log: %w", err)
	}
	return nil
}
