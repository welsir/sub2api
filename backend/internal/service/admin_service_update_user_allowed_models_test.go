//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAdminServiceUpdateUserPersistsAllowedModelsAndInvalidatesAuthCache(t *testing.T) {
	base := &userRepoStub{user: &User{ID: 42, Email: "u@example.com"}}
	repo := &rpmUserRepoStub{userRepoStub: base}
	invalidator := &authCacheInvalidatorStub{}
	svc := &adminServiceImpl{
		userRepo:             repo,
		redeemCodeRepo:       &redeemRepoStub{},
		authCacheInvalidator: invalidator,
	}

	models := []string{"gpt-5*", "claude-sonnet-4-6"}
	updated, err := svc.UpdateUser(context.Background(), 42, &UpdateUserInput{AllowedModels: &models})

	require.NoError(t, err)
	require.Equal(t, models, updated.AllowedModels)
	require.Equal(t, models, repo.lastUpdated.AllowedModels)
	require.Equal(t, []int64{42}, invalidator.userIDs)
}
