package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type groupMonitorVisibilityRepo struct {
	monitors []*GroupMonitor
	byID     *GroupMonitor
	listErr  error
}

func (r *groupMonitorVisibilityRepo) List(context.Context, GroupMonitorListParams) ([]*GroupMonitor, int64, error) {
	if r.listErr != nil {
		return nil, 0, r.listErr
	}
	return r.monitors, int64(len(r.monitors)), nil
}

func (r *groupMonitorVisibilityRepo) GetByID(context.Context, int64) (*GroupMonitor, error) {
	return r.byID, nil
}
func (r *groupMonitorVisibilityRepo) GetByGroupID(context.Context, int64) (*GroupMonitor, error) {
	return nil, nil
}
func (r *groupMonitorVisibilityRepo) Create(context.Context, *GroupMonitor) error { return nil }
func (r *groupMonitorVisibilityRepo) Update(context.Context, *GroupMonitor) error { return nil }
func (r *groupMonitorVisibilityRepo) Delete(context.Context, int64) error         { return nil }
func (r *groupMonitorVisibilityRepo) ListDue(context.Context, time.Time) ([]*GroupMonitor, error) {
	return nil, nil
}
func (r *groupMonitorVisibilityRepo) UpdateAfterRun(context.Context, int64, time.Time, time.Time) error {
	return nil
}
func (r *groupMonitorVisibilityRepo) ListGroupAccounts(context.Context, int64) ([]*GroupMonitorAccount, error) {
	return nil, nil
}
func (r *groupMonitorVisibilityRepo) UpsertResult(context.Context, *GroupMonitorResult) error {
	return nil
}
func (r *groupMonitorVisibilityRepo) DeleteResultsForUnschedulableAccounts(context.Context, int64) error {
	return nil
}
func (r *groupMonitorVisibilityRepo) AppendHistory(context.Context, *GroupMonitorResult) error {
	return nil
}
func (r *groupMonitorVisibilityRepo) QueryHistoryStats(context.Context, int64, time.Time) (*GroupMonitorHistoryStats, error) {
	return nil, nil
}
func (r *groupMonitorVisibilityRepo) QueryHistorySeries(context.Context, int64, time.Time, int) ([]GroupMonitorSeriesPoint, error) {
	return nil, nil
}
func (r *groupMonitorVisibilityRepo) ListRecentRecords(context.Context, int64, int) ([]GroupMonitorHistoryRecord, error) {
	return nil, nil
}
func (r *groupMonitorVisibilityRepo) PruneHistory(context.Context, time.Time) (int64, error) {
	return 0, nil
}
func (r *groupMonitorVisibilityRepo) QueryHistoryStatsBatch(context.Context, time.Time) (map[int64]*GroupMonitorHistoryStats, error) {
	return nil, nil
}
func (r *groupMonitorVisibilityRepo) ListRecentRecordsBatch(context.Context, int) (map[int64][]GroupMonitorHistoryRecord, error) {
	return nil, nil
}
func (r *groupMonitorVisibilityRepo) ListAccountStatesBatch(context.Context) (map[int64][]*GroupMonitorAccountStatus, error) {
	return nil, nil
}
func (r *groupMonitorVisibilityRepo) QueryGroupUsageStatsBatch(context.Context, time.Time) (map[int64]*GroupUsageStats, error) {
	return nil, nil
}
func (r *groupMonitorVisibilityRepo) QueryGroupPassiveStatsBatch(context.Context, time.Time, int) (map[int64]*GroupPassiveStats, error) {
	return nil, nil
}
func (r *groupMonitorVisibilityRepo) GroupNamesBatch(context.Context, []int64) (map[int64]string, error) {
	return nil, nil
}
func (r *groupMonitorVisibilityRepo) RecentGroupModels(context.Context, int64, time.Time, int) ([]string, error) {
	return nil, nil
}
func (r *groupMonitorVisibilityRepo) ListResults(context.Context, int64) ([]*GroupMonitorAccountStatus, error) {
	return nil, nil
}
func (r *groupMonitorVisibilityRepo) ResetResults(context.Context, int64) error { return nil }
func (r *groupMonitorVisibilityRepo) ListRecentUsageEvents(context.Context, []int64, int) (map[int64][]GroupUsageEvent, error) {
	return nil, nil
}
func (r *groupMonitorVisibilityRepo) ListUsageEventsSince(context.Context, []int64, time.Time) (map[int64][]GroupUsageEvent, error) {
	return nil, nil
}
func (r *groupMonitorVisibilityRepo) ListProbeRecordsSince(context.Context, []int64, time.Time) (map[int64][]GroupProbeRecord, error) {
	return nil, nil
}
func (r *groupMonitorVisibilityRepo) AggregateUsageSince(context.Context, []int64, time.Time) (map[int64]*GroupWindowAgg, error) {
	return nil, nil
}
func (r *groupMonitorVisibilityRepo) AggregateProbesSince(context.Context, []int64, time.Time) (map[int64]*GroupWindowAgg, error) {
	return nil, nil
}

type groupMonitorVisibilityUserRepo struct {
	user *User
}

func (r groupMonitorVisibilityUserRepo) GetByID(context.Context, int64) (*User, error) {
	return r.user, nil
}

func TestGroupMonitorListVisibleToUserExcludesHiddenGroup(t *testing.T) {
	repo := &groupMonitorVisibilityRepo{monitors: []*GroupMonitor{{
		ID:               1,
		GroupID:          7,
		UserVisible:      false,
		UserVisibleSet:   true,
		IsExclusive:      false,
		SubscriptionType: SubscriptionTypeStandard,
	}}}
	svc := NewGroupMonitorService(repo, nil, nil)
	svc.SetVisibilitySources(groupMonitorVisibilityUserRepo{user: &User{ID: 42, Role: RoleUser}}, nil)

	monitors, total, err := svc.ListVisibleToUser(context.Background(), 42, GroupMonitorListParams{Page: 1, PageSize: 20})

	require.NoError(t, err)
	require.Empty(t, monitors)
	require.Zero(t, total)
}

func TestGroupMonitorGetVisibleToUserChecksMonitorWithoutListingAll(t *testing.T) {
	repo := &groupMonitorVisibilityRepo{
		byID: &GroupMonitor{
			ID:               1,
			GroupID:          7,
			UserVisible:      true,
			UserVisibleSet:   true,
			SubscriptionType: SubscriptionTypeStandard,
		},
		listErr: errors.New("list must not be called"),
	}
	svc := NewGroupMonitorService(repo, nil, nil)
	svc.SetVisibilitySources(groupMonitorVisibilityUserRepo{user: &User{ID: 42, Role: RoleUser}}, nil)

	monitor, err := svc.GetVisibleToUser(context.Background(), 42, 1)

	require.NoError(t, err)
	require.Equal(t, int64(1), monitor.ID)
}
