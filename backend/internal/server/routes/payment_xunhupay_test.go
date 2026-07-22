package routes

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRegisterPaymentRoutesIncludesXunhuPayWebhook(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	v1 := router.Group("/api/v1")
	pass := func(c *gin.Context) { c.Next() }

	RegisterPaymentRoutes(
		v1,
		&handler.PaymentHandler{},
		&handler.PaymentWebhookHandler{},
		&admin.PaymentHandler{},
		middleware.JWTAuthMiddleware(pass),
		middleware.AdminAuthMiddleware(pass),
		nil,
	)

	found := false
	for _, route := range router.Routes() {
		if route.Method == "POST" && route.Path == "/api/v1/payment/webhook/xunhupay" {
			found = true
			break
		}
	}
	require.True(t, found, "XunhuPay webhook route must be registered")
}

func TestRegisterPaymentRoutesIncludesAuthenticatedOrderQRCodeImage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	v1 := router.Group("/api/v1")
	pass := func(c *gin.Context) { c.Next() }

	RegisterPaymentRoutes(
		v1,
		&handler.PaymentHandler{},
		&handler.PaymentWebhookHandler{},
		&admin.PaymentHandler{},
		middleware.JWTAuthMiddleware(pass),
		middleware.AdminAuthMiddleware(pass),
		nil,
	)

	found := false
	for _, route := range router.Routes() {
		if route.Method == "GET" && route.Path == "/api/v1/payment/orders/:id/qr-image" {
			found = true
			break
		}
	}
	require.True(t, found, "provider QR images must be exposed only through the authenticated payment group")
}
