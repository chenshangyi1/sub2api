package repository

import (
	"context"
	"database/sql/driver"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

var adaptiveEvidenceColumns = []string{
	"id", "created_at", "adaptive_reservation_id", "user_id", "api_key_id",
	"subscription_id", "billing_type", "adaptive_parent_group_id", "routed_group_id",
	"adaptive_attempt_no", "adaptive_pricing_snapshot_id", "adaptive_evidence_hash",
	"adaptive_base_cost", "adaptive_management_fee_cost", "adaptive_total_cost",
	"adaptive_uncapped_base_cost", "adaptive_platform_overage_cost",
}

func TestAdaptiveReconciliationEvidenceRepository_Inspect(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := NewAdaptiveReconciliationEvidenceRepository(db)
	createdAt := time.Now().UTC().Round(time.Microsecond)
	evidenceHash := service.HashUsageReservationKey("evidence")
	failureHash := service.HashUsageReservationKey("failed")

	mock.ExpectQuery("SELECT id, created_at, adaptive_reservation_id::text").
		WithArgs("00000000-0000-0000-0000-000000000001").
		WillReturnRows(sqlmock.NewRows(adaptiveEvidenceColumns).AddRow(
			99, createdAt, "00000000-0000-0000-0000-000000000001", 11, 12,
			nil, 0, 10, 20, 2, "pricing-v1", evidenceHash,
			"3.0000000000", "0.4500000000", "3.4500000000",
			"4.0000000000", "1.0000000000",
		))
	mock.ExpectQuery("SELECT status, failure_evidence_hash").
		WithArgs("00000000-0000-0000-0000-000000000001").
		WillReturnRows(sqlmock.NewRows([]string{"status", "failure_evidence_hash"}).
			AddRow("failed", failureHash).
			AddRow("started", nil))

	snapshot, err := repo.Inspect(context.Background(), "00000000-0000-0000-0000-000000000001")
	require.NoError(t, err)
	require.NotNil(t, snapshot.PendingUsage)
	require.True(t, snapshot.PendingUsage.Success)
	require.Equal(t, int64(99), snapshot.PendingUsage.UsageLogID)
	require.Equal(t, "4.0000000000", snapshot.PendingUsage.UncappedBaseCost.StringFixed(10))
	require.Equal(t, 2, snapshot.AttemptCount)
	require.Equal(t, 1, snapshot.FailedAttemptCount)
	require.Equal(t, 1, snapshot.StartedAttemptCount)
	require.Equal(t, failureHash, snapshot.LatestFailedEvidenceHash)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAdaptiveReconciliationEvidenceRepository_RejectsMultiplePendingRows(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := NewAdaptiveReconciliationEvidenceRepository(db)
	createdAt := time.Now().UTC()
	row := func(id int64) []driver.Value {
		return []driver.Value{
			id, createdAt, "00000000-0000-0000-0000-000000000002", 11, 12,
			nil, 0, 10, 20, 1, "pricing-v1", service.HashUsageReservationKey("evidence"),
			"1.0000000000", "0.1500000000", "1.1500000000", "1.0000000000", "0.0000000000",
		}
	}
	rows := sqlmock.NewRows(adaptiveEvidenceColumns)
	rows.AddRow(row(1)...)
	rows.AddRow(row(2)...)
	mock.ExpectQuery("SELECT id, created_at, adaptive_reservation_id::text").
		WithArgs("00000000-0000-0000-0000-000000000002").
		WillReturnRows(rows)

	snapshot, err := repo.Inspect(context.Background(), "00000000-0000-0000-0000-000000000002")
	require.Nil(t, snapshot)
	require.ErrorIs(t, err, service.ErrAdaptiveReconciliationEvidenceInvalid)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageReservationReleaseEvidence_IgnoresVoidedUsageRows(t *testing.T) {
	source, err := os.ReadFile("usage_billing_reservation_repo.go")
	require.NoError(t, err)
	text := string(source)
	start := strings.Index(text, "func lockAndRejectUsageReservationEvidence(")
	require.Greater(t, start, 0)
	rest := text[start+1:]
	next := strings.Index(rest, "\nfunc ")
	require.Greater(t, next, 0)
	body := text[start : start+1+next]
	require.Contains(t, body, "adaptive_settlement_status = 'pending'",
		"voided/captured usage rows must not block Release or MarkAttemptFailed")
}
