package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestExtractOutTradeNoFromXunhuPayNotification(t *testing.T) {
	t.Parallel()

	rawBody := "appid=app-1&trade_order_id=order-123&status=OD&hash=signed"
	require.Equal(t, "order-123", extractOutTradeNo(rawBody, payment.TypeXunhuPay))
}

func TestXunhuPaySuccessResponseIsPlainSuccess(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	writeSuccessResponse(context, payment.TypeXunhuPay)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "success", recorder.Body.String())
}
