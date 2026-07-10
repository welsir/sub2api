package dto

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAPIKeyFromServiceCopiesSelectedGroupIDs(t *testing.T) {
	source := &service.APIKey{
		ID:       1,
		GroupIDs: []int64{2, 3},
	}

	got := APIKeyFromService(source)
	require.Equal(t, []int64{2, 3}, got.GroupIDs)

	source.GroupIDs[0] = 99
	require.Equal(t, []int64{2, 3}, got.GroupIDs)
}
