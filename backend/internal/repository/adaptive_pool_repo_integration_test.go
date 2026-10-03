//go:build integration

package repository

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func createAdaptivePoolTestGroup(t *testing.T, platform, suffix string) int64 {
	t.Helper()
	var id int64
	name := fmt.Sprintf("adaptive-pool-%s-%d", suffix, time.Now().UnixNano())
	require.NoError(t, integrationDB.QueryRowContext(context.Background(), `
		INSERT INTO groups (name, platform, status)
		VALUES ($1, $2, 'active')
		RETURNING id
	`, name, platform).Scan(&id))
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM groups WHERE id = $1", id)
	})
	return id
}

func TestAdaptivePoolReplaceSerializesConcurrentGenerations(t *testing.T) {
	ctx := context.Background()
	parentID := createAdaptivePoolTestGroup(t, service.PlatformOpenAI, "parent")
	leafA := createAdaptivePoolTestGroup(t, service.PlatformOpenAI, "leaf-a")
	leafB := createAdaptivePoolTestGroup(t, service.PlatformOpenAI, "leaf-b")
	repo := NewAdaptivePoolAdminRepository(integrationDB)

	before, err := repo.ReplaceAdaptivePool(ctx, service.AdaptivePoolUpdate{
		ParentGroupID: parentID,
		Enabled:       true,
		Members:       []service.AdaptiveLeafRef{{LeafGroupID: leafA, Enabled: true}},
	})
	require.NoError(t, err)

	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for i, leafID := range []int64{leafA, leafB} {
		wg.Add(1)
		go func(sortOrder int, candidate int64) {
			defer wg.Done()
			<-start
			_, replaceErr := repo.ReplaceAdaptivePool(ctx, service.AdaptivePoolUpdate{
				ParentGroupID: parentID,
				Enabled:       true,
				Members: []service.AdaptiveLeafRef{{
					LeafGroupID: candidate,
					Enabled:     true,
					SortOrder:   sortOrder,
				}},
			})
			errs <- replaceErr
		}(i+1, leafID)
	}
	close(start)
	wg.Wait()
	close(errs)
	for replaceErr := range errs {
		require.NoError(t, replaceErr)
	}

	after, err := repo.GetAdaptivePoolSnapshot(ctx, parentID)
	require.NoError(t, err)
	require.GreaterOrEqual(t, after.ConfigGeneration, before.ConfigGeneration+2)
	require.Len(t, after.Members, 1, "concurrent full replacements must not interleave member sets")
	require.Contains(t, []int64{leafA, leafB}, after.Members[0].LeafGroupID)
}

func TestAdaptivePoolReplaceRollsBackGenerationAndMembers(t *testing.T) {
	ctx := context.Background()
	parentID := createAdaptivePoolTestGroup(t, service.PlatformOpenAI, "rollback-parent")
	goodLeaf := createAdaptivePoolTestGroup(t, service.PlatformOpenAI, "rollback-good")
	wrongPlatformLeaf := createAdaptivePoolTestGroup(t, service.PlatformAnthropic, "rollback-wrong")
	repo := NewAdaptivePoolAdminRepository(integrationDB)

	before, err := repo.ReplaceAdaptivePool(ctx, service.AdaptivePoolUpdate{
		ParentGroupID: parentID,
		Enabled:       true,
		Members:       []service.AdaptiveLeafRef{{LeafGroupID: goodLeaf, Enabled: true}},
	})
	require.NoError(t, err)

	_, err = repo.ReplaceAdaptivePool(ctx, service.AdaptivePoolUpdate{
		ParentGroupID: parentID,
		Enabled:       false,
		Members:       []service.AdaptiveLeafRef{{LeafGroupID: wrongPlatformLeaf, Enabled: true}},
	})
	require.ErrorContains(t, err, "platforms must match")

	after, err := repo.GetAdaptivePoolSnapshot(ctx, parentID)
	require.NoError(t, err)
	require.Equal(t, before, after, "failed replacement must roll back generation, enabled state, and members")
}

func TestAdaptivePoolGenerationExhaustionRollsBack(t *testing.T) {
	ctx := context.Background()
	parentID := createAdaptivePoolTestGroup(t, service.PlatformAnthropic, "overflow-parent")
	leafID := createAdaptivePoolTestGroup(t, service.PlatformAnthropic, "overflow-leaf")
	repo := NewAdaptivePoolAdminRepository(integrationDB)

	before, err := repo.ReplaceAdaptivePool(ctx, service.AdaptivePoolUpdate{
		ParentGroupID: parentID,
		Enabled:       true,
		Members:       []service.AdaptiveLeafRef{{LeafGroupID: leafID, Enabled: true}},
	})
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `
		UPDATE adaptive_group_configs
		   SET config_generation = 9223372036854775807
		 WHERE parent_group_id = $1
	`, parentID)
	require.NoError(t, err)

	_, err = repo.ReplaceAdaptivePool(ctx, service.AdaptivePoolUpdate{
		ParentGroupID: parentID,
		Enabled:       true,
		Members: []service.AdaptiveLeafRef{{
			LeafGroupID: leafID,
			Enabled:     true,
			SortOrder:   99,
		}},
	})
	require.ErrorContains(t, err, "adaptive config generation exhausted")

	after, err := repo.GetAdaptivePoolSnapshot(ctx, parentID)
	require.NoError(t, err)
	require.Equal(t, int64(9223372036854775807), after.ConfigGeneration)
	require.Equal(t, before.Members, after.Members)
}
