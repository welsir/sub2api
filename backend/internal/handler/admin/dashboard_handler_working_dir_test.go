package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type workingDirRepoCapture struct {
	service.UsageLogRepository
	userID int64
	limit  int
}

func (r *workingDirRepoCapture) GetWorkingDirSpending(_ context.Context, _, _ time.Time, userID int64, limit int) ([]usagestats.WorkingDirSpendingItem, error) {
	r.userID = userID
	r.limit = limit
	return []usagestats.WorkingDirSpendingItem{{
		UserID:           9,
		Email:            "u@example.com",
		WorkingDirectory: "/work/company",
		ActualCost:       12.5,
		Requests:         4,
	}}, nil
}

func TestDashboardWorkingDirSpending(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &workingDirRepoCapture{}
	svc := service.NewDashboardService(repo, nil, nil, nil)
	h := NewDashboardHandler(svc, nil)
	router := gin.New()
	router.GET("/admin/dashboard/working-dirs", h.GetWorkingDirSpending)

	req := httptest.NewRequest(http.MethodGet, "/admin/dashboard/working-dirs?start_date=2026-07-01&end_date=2026-07-07&user_id=9&limit=25", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, int64(9), repo.userID)
	require.Equal(t, 25, repo.limit)
	require.Contains(t, rec.Body.String(), `"working_directory":"/work/company"`)
	require.Contains(t, rec.Body.String(), `"total_actual_cost":12.5`)
}
