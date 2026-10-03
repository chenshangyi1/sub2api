package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestGetAdaptivePoolSnapshotReturnsStableOrderedTopology(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectQuery("FROM adaptive_group_configs c").
		WithArgs(int64(100)).
		WillReturnRows(sqlmock.NewRows([]string{
			"parent_group_id", "platform", "enabled", "config_generation",
			"use_manual_intelligence_order", "allow_hybrid",
			"leaf_group_id", "leaf_enabled", "sort_order",
		}).
			AddRow(int64(100), service.PlatformOpenAI, true, int64(17), false, true, int64(202), false, 10).
			AddRow(int64(100), service.PlatformOpenAI, true, int64(17), false, true, int64(201), true, 20))

	snapshot, err := getAdaptivePoolSnapshot(context.Background(), db, 100)
	require.NoError(t, err)
	require.Equal(t, &service.AdaptivePoolSnapshot{
		ParentGroupID:              100,
		Platform:                   service.PlatformOpenAI,
		Enabled:                    true,
		ConfigGeneration:           17,
		UseManualIntelligenceOrder: false,
		AllowHybrid:                true,
		Members: []service.AdaptiveLeafRef{
			{LeafGroupID: 202, Enabled: false, SortOrder: 10},
			{LeafGroupID: 201, Enabled: true, SortOrder: 20},
		},
	}, snapshot)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestGetAdaptivePoolSnapshotSupportsEmptyDisabledPool(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectQuery("FROM adaptive_group_configs c").
		WithArgs(int64(100)).
		WillReturnRows(sqlmock.NewRows([]string{
			"parent_group_id", "platform", "enabled", "config_generation",
			"use_manual_intelligence_order", "allow_hybrid",
			"leaf_group_id", "leaf_enabled", "sort_order",
		}).AddRow(int64(100), service.PlatformAnthropic, false, int64(3), false, false, nil, nil, nil))

	snapshot, err := getAdaptivePoolSnapshot(context.Background(), db, 100)
	require.NoError(t, err)
	require.False(t, snapshot.Enabled)
	require.Empty(t, snapshot.Members)
}

func TestGetAdaptivePoolSnapshotNotFound(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectQuery("FROM adaptive_group_configs c").
		WithArgs(int64(404)).
		WillReturnRows(sqlmock.NewRows([]string{
			"parent_group_id", "platform", "enabled", "config_generation",
			"use_manual_intelligence_order", "allow_hybrid",
			"leaf_group_id", "leaf_enabled", "sort_order",
		}))

	_, err = getAdaptivePoolSnapshot(context.Background(), db, 404)
	require.Error(t, err)
	require.True(t, errors.Is(err, service.ErrAdaptivePoolNotFound))
}
