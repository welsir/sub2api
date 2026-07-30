// [INPUT]: Activation configuration, activation lifecycle service, JWT subjects, and idempotency helpers.
// [OUTPUT]: Public activation offer, current-user activation status, and idempotent recall claim handlers.
// [POS]: HTTP boundary for the Omni new-user activation lifecycle.
package handler

import (
	"context"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

const activationRecallClaimScope = "user.activation.recall.claim"

type UserActivationAPIService interface {
	GetStatus(ctx context.Context, userID int64) (*service.UserActivationStatus, error)
	ClaimRecall(ctx context.Context, userID int64) (*service.UserActivationStatus, error)
}

type ActivationHandler struct {
	service UserActivationAPIService
	enabled bool
}

type publicActivationOffer struct {
	Enabled            bool    `json:"enabled"`
	StarterCreditUSD   int     `json:"starter_credit_usd,omitempty"`
	StarterValidHours  int     `json:"starter_valid_hours,omitempty"`
	RecallCreditUSD    int     `json:"recall_credit_usd,omitempty"`
	RecallValidHours   int     `json:"recall_valid_hours,omitempty"`
	RecallWindowDays   int     `json:"recall_window_days,omitempty"`
	MinimumRechargeCNY int     `json:"minimum_recharge_cny,omitempty"`
	RechargeCreditRate int     `json:"recharge_credit_rate,omitempty"`
	ProRateMultiplier  float64 `json:"pro_rate_multiplier,omitempty"`
	SupportWeChat      string  `json:"support_wechat,omitempty"`
}

func NewActivationHandler(
	activationService UserActivationAPIService,
	cfg config.UserActivationConfig,
) *ActivationHandler {
	return &ActivationHandler{
		service: activationService,
		enabled: cfg.Enabled,
	}
}

func ProvideActivationHandler(
	activationService *service.UserActivationService,
	cfg *config.Config,
) *ActivationHandler {
	return NewActivationHandler(activationService, cfg.UserActivation)
}

func (h *ActivationHandler) GetPublicOffer(c *gin.Context) {
	if !h.enabled {
		response.Success(c, publicActivationOffer{Enabled: false})
		return
	}
	response.Success(c, publicActivationOffer{
		Enabled:            true,
		StarterCreditUSD:   1,
		StarterValidHours:  24,
		RecallCreditUSD:    2,
		RecallValidHours:   24,
		RecallWindowDays:   7,
		MinimumRechargeCNY: 10,
		RechargeCreditRate: 1,
		ProRateMultiplier:  0.2,
		SupportWeChat:      "welsir02",
	})
}

func (h *ActivationHandler) GetStatus(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	status, err := h.service.GetStatus(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, status)
}

func (h *ActivationHandler) ClaimRecall(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	if strings.TrimSpace(c.GetHeader("Idempotency-Key")) == "" {
		response.ErrorFrom(c, service.ErrIdempotencyKeyRequired)
		return
	}
	executeUserIdempotentJSON(
		c,
		activationRecallClaimScope,
		struct{}{},
		service.DefaultWriteIdempotencyTTL(),
		func(ctx context.Context) (any, error) {
			return h.service.ClaimRecall(ctx, subject.UserID)
		},
	)
}
