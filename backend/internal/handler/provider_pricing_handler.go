package handler

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type ProviderPricingReader interface {
	Get(ctx context.Context) (*service.ProviderPricingResponse, error)
}

type ProviderPricingHandler struct {
	reader ProviderPricingReader
	cfg    config.ProviderPricingConfig
	now    func() time.Time
}

func NewProviderPricingHandler(reader ProviderPricingReader, cfg config.ProviderPricingConfig) *ProviderPricingHandler {
	return &ProviderPricingHandler{reader: reader, cfg: cfg, now: time.Now}
}

func ProvideProviderPricingHandler(reader *service.ProviderPricingService, cfg *config.Config) *ProviderPricingHandler {
	return NewProviderPricingHandler(reader, cfg.ProviderPricing)
}

func (h *ProviderPricingHandler) Get(c *gin.Context) {
	if !h.cfg.Enabled {
		c.Status(http.StatusNotFound)
		return
	}
	if h.cfg.RequireHMAC && !h.validSignature(c.GetHeader("X-Hvoy-Ts"), c.GetHeader("X-Hvoy-Sign")) {
		c.JSON(http.StatusUnauthorized, gin.H{
			"schema_version": "1.1",
			"success":        false,
			"message":        "invalid or expired HMAC signature",
			"data":           nil,
		})
		return
	}

	result, err := h.reader.Get(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"schema_version": "1.1",
			"success":        false,
			"message":        "provider pricing is temporarily unavailable",
			"data":           nil,
		})
		return
	}
	c.Header("Cache-Control", "private, no-store")
	c.JSON(http.StatusOK, result)
}

func (h *ProviderPricingHandler) validSignature(timestamp, signature string) bool {
	if timestamp == "" || signature == "" {
		return false
	}
	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return false
	}
	delta := h.now().Unix() - ts
	if delta < 0 {
		delta = -delta
	}
	if delta > int64(h.cfg.MaxSkewSeconds) {
		return false
	}
	received, err := hex.DecodeString(signature)
	if err != nil || len(received) != sha256.Size {
		return false
	}
	mac := hmac.New(sha256.New, []byte(h.cfg.HMACSecret))
	_, _ = mac.Write([]byte(timestamp))
	return hmac.Equal(received, mac.Sum(nil))
}
