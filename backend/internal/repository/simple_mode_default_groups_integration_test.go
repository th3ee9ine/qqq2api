//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	dbent "github.com/th3ee9ine/qqq2api/ent"
	"github.com/th3ee9ine/qqq2api/ent/group"
	"github.com/th3ee9ine/qqq2api/internal/service"
)

var retainedSimpleModePlatforms = []string{
	service.PlatformAnthropic,
	service.PlatformOpenAI,
}

var retiredSimpleModePlatforms = []string{
	service.PlatformGemini,
	service.PlatformAntigravity,
	service.PlatformGrok,
}

func TestEnsureSimpleModeDefaultGroups_CreatesMissingRetainedDefaults(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := testEntTx(t).Client()
	softDeleteSimpleModeGroups(t, ctx, client, retainedSimpleModePlatforms...)

	require.NoError(t, ensureSimpleModeDefaultGroups(ctx, client))

	for _, platform := range retainedSimpleModePlatforms {
		groups, err := client.Group.Query().
			Where(group.PlatformEQ(platform), group.DeletedAtIsNil()).
			All(ctx)
		require.NoError(t, err)
		require.Len(t, groups, 1)
		require.Equal(t, platform+"-default", groups[0].Name)
		require.NotNil(t, groups[0].Description)
		require.Equal(t, simpleModeDefaultGroupDescription, *groups[0].Description)
	}
}

func TestEnsureSimpleModeDefaultGroups_PreservesExistingRetainedGroups(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := testEntTx(t).Client()
	softDeleteSimpleModeGroups(t, ctx, client, retainedSimpleModePlatforms...)

	for _, platform := range retainedSimpleModePlatforms {
		_, err := client.Group.Create().
			SetName(platform + "-custom-existing").
			SetPlatform(platform).
			SetStatus(service.StatusActive).
			SetSubscriptionType(service.SubscriptionTypeStandard).
			SetRateMultiplier(1.0).
			SetIsExclusive(false).
			Save(ctx)
		require.NoError(t, err)
	}

	require.NoError(t, ensureSimpleModeDefaultGroups(ctx, client))

	for _, platform := range retainedSimpleModePlatforms {
		count, err := client.Group.Query().
			Where(group.PlatformEQ(platform), group.DeletedAtIsNil()).
			Count(ctx)
		require.NoError(t, err)
		require.Equal(t, 1, count)

		exists, err := client.Group.Query().
			Where(group.NameEQ(platform+"-default"), group.DeletedAtIsNil()).
			Exist(ctx)
		require.NoError(t, err)
		require.False(t, exists)
	}
}

func TestEnsureSimpleModeDefaultGroups_DoesNotCreateRetiredPlatformGroups(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := testEntTx(t).Client()
	softDeleteSimpleModeGroups(t, ctx, client, retiredSimpleModePlatforms...)

	require.NoError(t, ensureSimpleModeDefaultGroups(ctx, client))

	for _, platform := range retiredSimpleModePlatforms {
		count, err := client.Group.Query().
			Where(group.PlatformEQ(platform), group.DeletedAtIsNil()).
			Count(ctx)
		require.NoError(t, err)
		require.Zero(t, count)
	}
}

func softDeleteSimpleModeGroups(t *testing.T, ctx context.Context, client *dbent.Client, platforms ...string) {
	t.Helper()
	_, err := client.Group.Delete().
		Where(group.PlatformIn(platforms...), group.DeletedAtIsNil()).
		Exec(ctx)
	require.NoError(t, err)
}
