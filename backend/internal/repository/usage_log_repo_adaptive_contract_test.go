package repository

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestPrepareUsageLogInsert_AdaptiveColumnsStayOrdered(t *testing.T) {
	t.Parallel()

	baseCost := 1.25
	managementFee := 0.1875
	totalCost := 1.4375
	uncappedBaseCost := 1.25
	platformOverageCost := 0.0
	parentGroupID := int64(101)
	routedGroupID := int64(202)
	attemptNo := 2
	pricingSnapshotID := "pricing-generation-17"
	reservationID := "8ef6ac46-74eb-40b4-9585-125b3b4ec6f6"
	evidenceHash := "1d8d2e16d66d6dd89d08f5f6438c30a04f6f5fc4e54513b5b4f41cfdb640ec81"
	settlementStatus := "pending"

	prepared := prepareUsageLogInsert(&service.UsageLog{
		AdaptiveBaseCost:            &baseCost,
		AdaptiveManagementFeeCost:   &managementFee,
		AdaptiveTotalCost:           &totalCost,
		AdaptiveUncappedBaseCost:    &uncappedBaseCost,
		AdaptivePlatformOverageCost: &platformOverageCost,
		AdaptiveParentGroupID:       &parentGroupID,
		RoutedGroupID:               &routedGroupID,
		AdaptiveAttemptNo:           &attemptNo,
		AdaptivePricingSnapshotID:   &pricingSnapshotID,
		AdaptiveReservationID:       &reservationID,
		AdaptiveEvidenceHash:        &evidenceHash,
		AdaptiveSettlementStatus:    &settlementStatus,
	})

	require.Len(t, usageLogInsertArgTypes, 71)
	require.Len(t, prepared.args, len(usageLogInsertArgTypes))
	require.Equal(t, "numeric", usageLogInsertArgTypes[57])
	require.Equal(t, "numeric", usageLogInsertArgTypes[58])
	require.Equal(t, "numeric", usageLogInsertArgTypes[59])
	require.Equal(t, "numeric", usageLogInsertArgTypes[60])
	require.Equal(t, "numeric", usageLogInsertArgTypes[61])
	require.Equal(t, "bigint", usageLogInsertArgTypes[62])
	require.Equal(t, "bigint", usageLogInsertArgTypes[63])
	require.Equal(t, "integer", usageLogInsertArgTypes[64])
	require.Equal(t, "text", usageLogInsertArgTypes[65])
	require.Equal(t, "uuid", usageLogInsertArgTypes[66])
	require.Equal(t, "text", usageLogInsertArgTypes[67])
	require.Equal(t, "text", usageLogInsertArgTypes[68])

	require.Same(t, &baseCost, prepared.args[57])
	require.Same(t, &managementFee, prepared.args[58])
	require.Same(t, &totalCost, prepared.args[59])
	require.Same(t, &uncappedBaseCost, prepared.args[60])
	require.Same(t, &platformOverageCost, prepared.args[61])
	require.Equal(t, sql.NullInt64{Int64: parentGroupID, Valid: true}, prepared.args[62])
	require.Equal(t, sql.NullInt64{Int64: routedGroupID, Valid: true}, prepared.args[63])
	require.Equal(t, sql.NullInt64{Int64: int64(attemptNo), Valid: true}, prepared.args[64])
	require.Equal(t, sql.NullString{String: pricingSnapshotID, Valid: true}, prepared.args[65])
	require.Equal(t, sql.NullString{String: reservationID, Valid: true}, prepared.args[66])
	require.Equal(t, sql.NullString{String: evidenceHash, Valid: true}, prepared.args[67])
	require.Equal(t, sql.NullString{String: settlementStatus, Valid: true}, prepared.args[68])
	require.Equal(t, sql.NullString{}, prepared.args[69])
	require.Equal(t, "text", usageLogInsertArgTypes[69])
	require.Equal(t, "timestamptz", usageLogInsertArgTypes[70])
}

func TestUsageLogInsertQueries_IncludeAdaptiveColumns(t *testing.T) {
	t.Parallel()

	prepared := prepareUsageLogInsert(&service.UsageLog{
		UserID:    1,
		APIKeyID:  2,
		AccountID: 3,
		RequestID: "req-adaptive-columns",
		Model:     "gpt-5",
		CreatedAt: time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC),
	})
	key := usageLogBatchKey(prepared.requestID, 2)

	batchQuery, batchArgs := buildUsageLogBatchInsertQuery([]string{key}, map[string]usageLogInsertPrepared{key: prepared})
	bestEffortQuery, bestEffortArgs := buildUsageLogBestEffortInsertQuery([]usageLogInsertPrepared{prepared})

	for _, query := range []string{batchQuery, bestEffortQuery} {
		require.Contains(t, query, "adaptive_base_cost")
		require.Contains(t, query, "adaptive_management_fee_cost")
		require.Contains(t, query, "adaptive_total_cost")
		require.Contains(t, query, "adaptive_uncapped_base_cost")
		require.Contains(t, query, "adaptive_platform_overage_cost")
		require.Contains(t, query, "adaptive_parent_group_id")
		require.Contains(t, query, "routed_group_id")
		require.Contains(t, query, "adaptive_attempt_no")
		require.Contains(t, query, "adaptive_pricing_snapshot_id")
		require.Contains(t, query, "adaptive_reservation_id")
		require.Contains(t, query, "adaptive_evidence_hash")
		require.Contains(t, query, "adaptive_settlement_status")
		require.Contains(t, query, "ON CONFLICT (request_id, api_key_id) DO NOTHING")
		require.NotContains(t, strings.ToUpper(query), "DO UPDATE")
	}
	require.Len(t, batchArgs, len(prepared.args)+1)
	require.Len(t, bestEffortArgs, len(prepared.args))
}

func TestUsageLogRepositoryCreateAdaptive_ReusesOnlyMatchingEvidence(t *testing.T) {
	t.Parallel()

	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db, db: db}
	log := adaptiveUsageEvidenceFixture()
	createdAt := time.Date(2026, 7, 22, 2, 3, 4, 0, time.UTC)

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id::text[[:space:]]+FROM usage_billing_reservations").
		WithArgs(*log.AdaptiveReservationID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(*log.AdaptiveReservationID))
	mock.ExpectQuery("SELECT id, created_at, user_id, api_key_id, account_id, request_id").
		WithArgs(*log.AdaptiveReservationID).
		WillReturnRows(adaptiveUsageEvidenceRows(log, 801, createdAt, *log.AdaptiveAttemptNo))
	mock.ExpectCommit()

	inserted, err := repo.Create(context.Background(), log)

	require.NoError(t, err)
	require.False(t, inserted)
	require.Equal(t, int64(801), log.ID)
	require.Equal(t, createdAt, log.CreatedAt)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageLogRepositoryCreateAdaptive_RejectsMismatchedOrDuplicateEvidence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		rows func(*service.UsageLog, time.Time) *sqlmock.Rows
	}{
		{
			name: "attempt mismatch",
			rows: func(log *service.UsageLog, createdAt time.Time) *sqlmock.Rows {
				return adaptiveUsageEvidenceRows(log, 802, createdAt, 2)
			},
		},
		{
			name: "multiple evidence rows",
			rows: func(log *service.UsageLog, createdAt time.Time) *sqlmock.Rows {
				rows := adaptiveUsageEvidenceRows(log, 803, createdAt, *log.AdaptiveAttemptNo)
				return rows.AddRow(adaptiveUsageEvidenceRowValues(log, 804, createdAt.Add(time.Nanosecond), *log.AdaptiveAttemptNo)...)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock := newSQLMock(t)
			repo := &usageLogRepository{sql: db, db: db}
			log := adaptiveUsageEvidenceFixture()
			createdAt := time.Date(2026, 7, 22, 2, 3, 5, 0, time.UTC)

			mock.ExpectBegin()
			mock.ExpectQuery("SELECT id::text[[:space:]]+FROM usage_billing_reservations").
				WithArgs(*log.AdaptiveReservationID).
				WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(*log.AdaptiveReservationID))
			mock.ExpectQuery("SELECT id, created_at, user_id, api_key_id, account_id, request_id").
				WithArgs(*log.AdaptiveReservationID).
				WillReturnRows(tt.rows(log, createdAt))
			mock.ExpectRollback()

			inserted, err := repo.Create(context.Background(), log)

			require.False(t, inserted)
			require.ErrorIs(t, err, service.ErrAdaptiveUsageEvidenceConflict)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestUsageLogRepositoryCreateAdaptive_RejectsAmbientEntTransaction(t *testing.T) {
	t.Parallel()

	repo := &usageLogRepository{}
	ctx := dbent.NewTxContext(context.Background(), &dbent.Tx{})
	inserted, err := repo.Create(ctx, adaptiveUsageEvidenceFixture())

	require.False(t, inserted)
	require.True(t, errors.Is(err, service.ErrAdaptiveUsageEvidenceTransaction))
}

func TestUsageLogRepositoryCreateBestEffort_RejectsAdaptiveEvidence(t *testing.T) {
	t.Parallel()

	repo := &usageLogRepository{}
	err := repo.CreateBestEffort(context.Background(), adaptiveUsageEvidenceFixture())
	require.ErrorIs(t, err, service.ErrAdaptiveUsageEvidenceTransaction)
}

func adaptiveUsageEvidenceFixture() *service.UsageLog {
	baseCost := 1.25
	managementFee := 0.1875
	totalCost := 1.4375
	uncappedBaseCost := 1.25
	platformOverageCost := 0.0
	parentGroupID := int64(101)
	routedGroupID := int64(202)
	attemptNo := 1
	pricingSnapshotID := "pricing-generation-17"
	reservationID := "8ef6ac46-74eb-40b4-9585-125b3b4ec6f6"
	evidenceHash := service.HashUsageReservationKey("adaptive-evidence")
	settlementStatus := service.AdaptiveSettlementStatusPending
	return &service.UsageLog{
		UserID:                      10,
		APIKeyID:                    20,
		AccountID:                   30,
		RequestID:                   "request-adaptive-1",
		AdaptiveBaseCost:            &baseCost,
		AdaptiveManagementFeeCost:   &managementFee,
		AdaptiveTotalCost:           &totalCost,
		AdaptiveUncappedBaseCost:    &uncappedBaseCost,
		AdaptivePlatformOverageCost: &platformOverageCost,
		AdaptiveParentGroupID:       &parentGroupID,
		RoutedGroupID:               &routedGroupID,
		AdaptiveAttemptNo:           &attemptNo,
		AdaptivePricingSnapshotID:   &pricingSnapshotID,
		AdaptiveReservationID:       &reservationID,
		AdaptiveEvidenceHash:        &evidenceHash,
		AdaptiveSettlementStatus:    &settlementStatus,
	}
}

func adaptiveUsageEvidenceRows(log *service.UsageLog, id int64, createdAt time.Time, attemptNo int) *sqlmock.Rows {
	columns := []string{
		"id", "created_at", "user_id", "api_key_id", "account_id", "request_id",
		"actual_cost", "adaptive_base_cost", "adaptive_management_fee_cost",
		"adaptive_total_cost", "adaptive_uncapped_base_cost", "adaptive_platform_overage_cost",
		"adaptive_parent_group_id", "routed_group_id", "adaptive_attempt_no",
		"adaptive_pricing_snapshot_id", "adaptive_reservation_id", "adaptive_evidence_hash",
		"adaptive_settlement_status",
	}
	return sqlmock.NewRows(columns).AddRow(adaptiveUsageEvidenceRowValues(log, id, createdAt, attemptNo)...)
}

func adaptiveUsageEvidenceRowValues(log *service.UsageLog, id int64, createdAt time.Time, attemptNo int) []driver.Value {
	return []driver.Value{
		id,
		createdAt,
		log.UserID,
		log.APIKeyID,
		log.AccountID,
		log.RequestID,
		log.ActualCost,
		*log.AdaptiveBaseCost,
		*log.AdaptiveManagementFeeCost,
		*log.AdaptiveTotalCost,
		*log.AdaptiveUncappedBaseCost,
		*log.AdaptivePlatformOverageCost,
		*log.AdaptiveParentGroupID,
		*log.RoutedGroupID,
		attemptNo,
		*log.AdaptivePricingSnapshotID,
		*log.AdaptiveReservationID,
		*log.AdaptiveEvidenceHash,
		*log.AdaptiveSettlementStatus,
	}
}

func TestUsageLogSelectColumns_AdaptiveStayOrderedWithInsert(t *testing.T) {
	t.Parallel()

	cols := strings.Split(strings.ReplaceAll(usageLogSelectColumns, " ", ""), ",")
	require.GreaterOrEqual(t, len(cols), 15)

	wantTail := []string{
		"account_stats_cost",
		"adaptive_base_cost",
		"adaptive_management_fee_cost",
		"adaptive_total_cost",
		"adaptive_uncapped_base_cost",
		"adaptive_platform_overage_cost",
		"adaptive_parent_group_id",
		"routed_group_id",
		"adaptive_attempt_no",
		"adaptive_pricing_snapshot_id",
		"adaptive_reservation_id",
		"adaptive_evidence_hash",
		"adaptive_settlement_status",
		"session_id",
		"created_at",
	}
	require.Equal(t, wantTail, cols[len(cols)-len(wantTail):])
}

func TestUsageLogRepositoryListWithFilters_GroupIDMatchesAdaptiveParentAndRouted(t *testing.T) {
	t.Parallel()

	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}
	filters := usagestats.UsageLogFilters{GroupID: 101, ExactTotal: true}

	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM usage_logs WHERE \(group_id = \$1 OR adaptive_parent_group_id = \$1 OR routed_group_id = \$1\)`).
		WithArgs(int64(101)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(0)))
	mock.ExpectQuery(`SELECT .* FROM usage_logs WHERE \(group_id = \$1 OR adaptive_parent_group_id = \$1 OR routed_group_id = \$1\) ORDER BY id DESC LIMIT \$2 OFFSET \$3`).
		WithArgs(int64(101), 20, 0).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	logs, page, err := repo.ListWithFilters(context.Background(), pagination.PaginationParams{Page: 1, PageSize: 20}, filters)
	require.NoError(t, err)
	require.Empty(t, logs)
	require.NotNil(t, page)
	require.Equal(t, int64(0), page.Total)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestScanUsageLog_AdaptiveColumns(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 22, 2, 3, 4, 0, time.UTC)
	log, err := scanUsageLog(usageLogScannerStub{values: []any{
		int64(9),
		int64(10),
		int64(20),
		int64(30),
		sql.NullString{Valid: true, String: "req-adaptive-scan"},
		"gpt-5",
		sql.NullString{Valid: true, String: "gpt-5"},
		sql.NullString{},
		sql.NullString{},
		sql.NullBool{},
		sql.NullInt64{Valid: true, Int64: 202},
		sql.NullInt64{},
		1, 2, 3, 4, 5, 6,
		0, 0.0,
		0, 0.0,
		0.1, 0.2, 0.3, 0.4, 1.0, 0.0,
		1.0,
		sql.NullFloat64{},
		int16(service.BillingTypeBalance),
		int16(service.RequestTypeSync),
		false,
		false,
		sql.NullInt64{},
		sql.NullInt64{},
		sql.NullString{},
		sql.NullString{},
		0,
		sql.NullString{},
		sql.NullString{},
		sql.NullString{},
		sql.NullString{},
		sql.NullString{},
		0,
		sql.NullString{},
		sql.NullInt64{},
		sql.NullString{},
		sql.NullString{},
		sql.NullString{},
		sql.NullString{},
		false,
		false,
		sql.NullInt64{},
		sql.NullString{},
		sql.NullString{},
		sql.NullString{},
		sql.NullFloat64{},
		sql.NullFloat64{Valid: true, Float64: 0.8},
		sql.NullFloat64{Valid: true, Float64: 0.12},
		sql.NullFloat64{Valid: true, Float64: 0.92},
		sql.NullFloat64{Valid: true, Float64: 1.2},
		sql.NullFloat64{Valid: true, Float64: 0.4},
		sql.NullInt64{Valid: true, Int64: 101},
		sql.NullInt64{Valid: true, Int64: 202},
		sql.NullInt64{Valid: true, Int64: 2},
		sql.NullString{Valid: true, String: "pricing-42"},
		sql.NullString{Valid: true, String: "reservation-7"},
		sql.NullString{Valid: true, String: "evidence-7"},
		sql.NullString{Valid: true, String: "captured"},
		sql.NullString{},
		now,
	}})
	require.NoError(t, err)
	require.NotNil(t, log.AdaptiveBaseCost)
	require.InDelta(t, 0.8, *log.AdaptiveBaseCost, 1e-12)
	require.NotNil(t, log.AdaptiveManagementFeeCost)
	require.InDelta(t, 0.12, *log.AdaptiveManagementFeeCost, 1e-12)
	require.NotNil(t, log.AdaptiveTotalCost)
	require.InDelta(t, 0.92, *log.AdaptiveTotalCost, 1e-12)
	require.NotNil(t, log.AdaptiveUncappedBaseCost)
	require.InDelta(t, 1.2, *log.AdaptiveUncappedBaseCost, 1e-12)
	require.NotNil(t, log.AdaptivePlatformOverageCost)
	require.InDelta(t, 0.4, *log.AdaptivePlatformOverageCost, 1e-12)
	require.NotNil(t, log.AdaptiveParentGroupID)
	require.Equal(t, int64(101), *log.AdaptiveParentGroupID)
	require.NotNil(t, log.RoutedGroupID)
	require.Equal(t, int64(202), *log.RoutedGroupID)
	require.NotNil(t, log.AdaptiveAttemptNo)
	require.Equal(t, 2, *log.AdaptiveAttemptNo)
	require.NotNil(t, log.AdaptivePricingSnapshotID)
	require.Equal(t, "pricing-42", *log.AdaptivePricingSnapshotID)
	require.NotNil(t, log.AdaptiveReservationID)
	require.Equal(t, "reservation-7", *log.AdaptiveReservationID)
	require.NotNil(t, log.AdaptiveEvidenceHash)
	require.Equal(t, "evidence-7", *log.AdaptiveEvidenceHash)
	require.NotNil(t, log.AdaptiveSettlementStatus)
	require.Equal(t, "captured", *log.AdaptiveSettlementStatus)
}

func TestCollectUsageLogIDs_IncludesAdaptiveParentAndRouted(t *testing.T) {
	t.Parallel()

	leaf := int64(303)
	parent := int64(101)
	routed := int64(202)
	ids := collectUsageLogIDs([]service.UsageLog{{
		UserID:                1,
		APIKeyID:              2,
		AccountID:             3,
		GroupID:               &leaf,
		AdaptiveParentGroupID: &parent,
		RoutedGroupID:         &routed,
	}})
	require.ElementsMatch(t, []int64{leaf, parent, routed}, ids.groupIDs)
}
