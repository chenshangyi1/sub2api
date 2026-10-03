package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

type adaptiveCoordinatorUsageLogRepo struct {
	create func(*UsageLog) (bool, error)
	get    func(int64) (*UsageLog, error)
}

func (r *adaptiveCoordinatorUsageLogRepo) Create(_ context.Context, log *UsageLog) (bool, error) {
	return r.create(log)
}

func (r *adaptiveCoordinatorUsageLogRepo) GetByID(_ context.Context, id int64) (*UsageLog, error) {
	return r.get(id)
}

type adaptiveCoordinatorReservationRepo struct {
	captureCalls int
	capture      func(*UsageReservationCaptureCommand) (*UsageReservationResult, error)
}

func (r *adaptiveCoordinatorReservationRepo) Reserve(context.Context, *UsageReservationReserveCommand) (*UsageReservationResult, error) {
	return nil, errors.New("unexpected Reserve")
}

func (r *adaptiveCoordinatorReservationRepo) MarkInFlight(context.Context, *UsageReservationMarkInFlightCommand) (*UsageReservationResult, error) {
	return nil, errors.New("unexpected MarkInFlight")
}

func (r *adaptiveCoordinatorReservationRepo) MarkAttemptFailed(context.Context, *UsageReservationAttemptFailedCommand) (*UsageReservationResult, error) {
	return nil, errors.New("unexpected MarkAttemptFailed")
}

func (r *adaptiveCoordinatorReservationRepo) Capture(_ context.Context, cmd *UsageReservationCaptureCommand) (*UsageReservationResult, error) {
	r.captureCalls++
	return r.capture(cmd)
}

func (r *adaptiveCoordinatorReservationRepo) Release(context.Context, *UsageReservationReleaseCommand) (*UsageReservationResult, error) {
	return nil, errors.New("unexpected Release")
}

func (r *adaptiveCoordinatorReservationRepo) Renew(context.Context, *UsageReservationRenewCommand) (*UsageReservationResult, error) {
	return nil, errors.New("unexpected Renew")
}

func (r *adaptiveCoordinatorReservationRepo) ReconcileExpired(context.Context, *UsageReservationReconcileCommand) (*UsageReservationReconcileResult, error) {
	return nil, errors.New("unexpected ReconcileExpired")
}

func TestAdaptiveBillingCoordinatorCapture_ReusesMatchingConflictEvidence(t *testing.T) {
	t.Parallel()

	billing := testAdaptiveCoordinatorBillingContext()
	var persisted *UsageLog
	usageRepo := &adaptiveCoordinatorUsageLogRepo{
		create: func(log *UsageLog) (bool, error) {
			persisted = cloneAdaptiveCoordinatorUsageLog(log)
			persisted.ID = 701
			persisted.CreatedAt = time.Date(2026, 7, 22, 1, 2, 3, 0, time.UTC)
			log.ID = persisted.ID
			log.CreatedAt = persisted.CreatedAt
			return false, nil
		},
		get: func(id int64) (*UsageLog, error) {
			require.Equal(t, persisted.ID, id)
			return cloneAdaptiveCoordinatorUsageLog(persisted), nil
		},
	}
	reservationRepo := &adaptiveCoordinatorReservationRepo{
		capture: func(cmd *UsageReservationCaptureCommand) (*UsageReservationResult, error) {
			require.Equal(t, persisted.ID, cmd.UsageLogID)
			require.Equal(t, persisted.CreatedAt, cmd.UsageLogCreatedAt)
			require.Equal(t, *persisted.AdaptiveEvidenceHash, cmd.EvidenceHash)
			return successfulAdaptiveCoordinatorCaptureResult(billing, false), nil
		},
	}

	log := testAdaptiveCoordinatorUsageLog()
	fee, result, err := NewAdaptiveBillingCoordinator(reservationRepo, usageRepo).
		Capture(context.Background(), billing, log, decimal.RequireFromString("1.25"))

	require.NoError(t, err)
	require.NotNil(t, fee)
	require.NotNil(t, result)
	require.False(t, result.Applied)
	require.Equal(t, 1, reservationRepo.captureCalls)
	require.Equal(t, AdaptiveSettlementStatusCaptured, *log.AdaptiveSettlementStatus)
}

func TestAdaptiveBillingCoordinatorCapture_ConflictBindingFailsClosed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*UsageLog)
	}{
		{
			name: "reservation",
			mutate: func(log *UsageLog) {
				value := "d905ad6a-5e7e-4ac8-b89b-f7682b7f97bb"
				log.AdaptiveReservationID = &value
			},
		},
		{
			name: "attempt",
			mutate: func(log *UsageLog) {
				value := 2
				log.AdaptiveAttemptNo = &value
			},
		},
		{
			name: "fingerprint",
			mutate: func(log *UsageLog) {
				value := HashUsageReservationKey("different-evidence")
				log.AdaptiveEvidenceHash = &value
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			billing := testAdaptiveCoordinatorBillingContext()
			var persisted *UsageLog
			usageRepo := &adaptiveCoordinatorUsageLogRepo{
				create: func(log *UsageLog) (bool, error) {
					persisted = cloneAdaptiveCoordinatorUsageLog(log)
					persisted.ID = 702
					persisted.CreatedAt = time.Date(2026, 7, 22, 1, 2, 4, 0, time.UTC)
					tt.mutate(persisted)
					log.ID = persisted.ID
					log.CreatedAt = persisted.CreatedAt
					return false, nil
				},
				get: func(int64) (*UsageLog, error) {
					return cloneAdaptiveCoordinatorUsageLog(persisted), nil
				},
			}
			reservationRepo := &adaptiveCoordinatorReservationRepo{
				capture: func(*UsageReservationCaptureCommand) (*UsageReservationResult, error) {
					return successfulAdaptiveCoordinatorCaptureResult(billing, true), nil
				},
			}

			_, result, err := NewAdaptiveBillingCoordinator(reservationRepo, usageRepo).
				Capture(context.Background(), billing, testAdaptiveCoordinatorUsageLog(), decimal.RequireFromString("1.25"))

			require.ErrorIs(t, err, ErrAdaptiveUsageEvidenceConflict)
			require.Nil(t, result)
			require.Zero(t, reservationRepo.captureCalls)
		})
	}
}

func TestAdaptiveBillingCoordinatorCapture_RetryReusesSinglePendingEvidence(t *testing.T) {
	t.Parallel()

	errFirstCapture := errors.New("transient capture failure")
	var persisted *UsageLog
	createCalls := 0
	usageRepo := &adaptiveCoordinatorUsageLogRepo{
		create: func(log *UsageLog) (bool, error) {
			createCalls++
			if persisted == nil {
				log.ID = 703
				log.CreatedAt = time.Date(2026, 7, 22, 1, 2, 5, 0, time.UTC)
				persisted = cloneAdaptiveCoordinatorUsageLog(log)
				return true, nil
			}
			log.ID = persisted.ID
			log.CreatedAt = persisted.CreatedAt
			return false, nil
		},
		get: func(int64) (*UsageLog, error) {
			return cloneAdaptiveCoordinatorUsageLog(persisted), nil
		},
	}
	reservationRepo := &adaptiveCoordinatorReservationRepo{}
	reservationRepo.capture = func(*UsageReservationCaptureCommand) (*UsageReservationResult, error) {
		if reservationRepo.captureCalls == 1 {
			return nil, errFirstCapture
		}
		return successfulAdaptiveCoordinatorCaptureResult(testAdaptiveCoordinatorBillingContext(), true), nil
	}
	coordinator := NewAdaptiveBillingCoordinator(reservationRepo, usageRepo)

	firstLog := testAdaptiveCoordinatorUsageLog()
	_, firstResult, err := coordinator.Capture(context.Background(), testAdaptiveCoordinatorBillingContext(), firstLog, decimal.RequireFromString("1.25"))
	require.ErrorIs(t, err, errFirstCapture)
	require.Nil(t, firstResult)
	require.Equal(t, AdaptiveSettlementStatusPending, *firstLog.AdaptiveSettlementStatus)

	secondLog := testAdaptiveCoordinatorUsageLog()
	_, secondResult, err := coordinator.Capture(context.Background(), testAdaptiveCoordinatorBillingContext(), secondLog, decimal.RequireFromString("1.25"))
	require.NoError(t, err)
	require.NotNil(t, secondResult)
	require.Equal(t, 2, createCalls)
	require.Equal(t, 2, reservationRepo.captureCalls)
	require.Equal(t, int64(703), secondLog.ID)
	require.Equal(t, AdaptiveSettlementStatusCaptured, *secondLog.AdaptiveSettlementStatus)
}

func TestAdaptiveBillingCoordinatorCapture_AcceptsCapturedReplayEvidence(t *testing.T) {
	t.Parallel()

	billing := testAdaptiveCoordinatorBillingContext()
	var persisted *UsageLog
	usageRepo := &adaptiveCoordinatorUsageLogRepo{
		create: func(log *UsageLog) (bool, error) {
			persisted = cloneAdaptiveCoordinatorUsageLog(log)
			persisted.ID = 704
			persisted.CreatedAt = time.Date(2026, 7, 22, 1, 2, 6, 0, time.UTC)
			persisted.ActualCost = *persisted.AdaptiveTotalCost
			status := AdaptiveSettlementStatusCaptured
			persisted.AdaptiveSettlementStatus = &status
			log.ID = persisted.ID
			log.CreatedAt = persisted.CreatedAt
			return false, nil
		},
		get: func(int64) (*UsageLog, error) {
			return cloneAdaptiveCoordinatorUsageLog(persisted), nil
		},
	}
	reservationRepo := &adaptiveCoordinatorReservationRepo{
		capture: func(*UsageReservationCaptureCommand) (*UsageReservationResult, error) {
			return successfulAdaptiveCoordinatorCaptureResult(billing, false), nil
		},
	}

	log := testAdaptiveCoordinatorUsageLog()
	_, result, err := NewAdaptiveBillingCoordinator(reservationRepo, usageRepo).
		Capture(context.Background(), billing, log, decimal.RequireFromString("1.25"))

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, persisted.ActualCost, log.ActualCost)
	require.Equal(t, AdaptiveSettlementStatusCaptured, *log.AdaptiveSettlementStatus)
}

func testAdaptiveCoordinatorBillingContext() *AdaptiveBillingContext {
	return &AdaptiveBillingContext{
		ReservationID:     "7d5f997a-f2ad-4405-bd4e-0ed5b761045b",
		LogicalRequestID:  "logical-request-1",
		OwnerID:           "gateway-1",
		FencingToken:      3,
		RowVersion:        7,
		ParentGroupID:     101,
		LeafGroupID:       202,
		AttemptNo:         1,
		PricingGeneration: 11,
		ConfigGeneration:  12,
		PricingSnapshotID: "pricing-11",
		ManagementFeeBPS:  DefaultAdaptiveManagementFeeBPS,
		HeldBaseCost:      decimal.RequireFromString("2.0000000000"),
		HeldManagementFee: decimal.RequireFromString("0.3000000000"),
		HeldTotal:         decimal.RequireFromString("2.3000000000"),
		FundingSource:     UsageReservationFundingBalance,
	}
}

func testAdaptiveCoordinatorUsageLog() *UsageLog {
	return &UsageLog{
		UserID:       10,
		APIKeyID:     20,
		AccountID:    30,
		RequestID:    "request-1",
		Model:        "gpt-test",
		InputTokens:  4,
		OutputTokens: 5,
		BillingType:  BillingTypeBalance,
	}
}

func successfulAdaptiveCoordinatorCaptureResult(billing *AdaptiveBillingContext, applied bool) *UsageReservationResult {
	return &UsageReservationResult{
		Applied: applied,
		Reservation: &UsageBillingReservation{
			ID:           billing.ReservationID,
			OwnerID:      billing.OwnerID,
			FencingToken: billing.FencingToken,
			RowVersion:   billing.RowVersion + 1,
			Status:       UsageReservationStatusCaptured,
		},
	}
}

func cloneAdaptiveCoordinatorUsageLog(log *UsageLog) *UsageLog {
	if log == nil {
		return nil
	}
	clone := *log
	clone.AdaptiveBaseCost = cloneAdaptiveCoordinatorPointer(log.AdaptiveBaseCost)
	clone.AdaptiveManagementFeeCost = cloneAdaptiveCoordinatorPointer(log.AdaptiveManagementFeeCost)
	clone.AdaptiveTotalCost = cloneAdaptiveCoordinatorPointer(log.AdaptiveTotalCost)
	clone.AdaptiveUncappedBaseCost = cloneAdaptiveCoordinatorPointer(log.AdaptiveUncappedBaseCost)
	clone.AdaptivePlatformOverageCost = cloneAdaptiveCoordinatorPointer(log.AdaptivePlatformOverageCost)
	clone.AdaptiveParentGroupID = cloneAdaptiveCoordinatorPointer(log.AdaptiveParentGroupID)
	clone.RoutedGroupID = cloneAdaptiveCoordinatorPointer(log.RoutedGroupID)
	clone.AdaptiveAttemptNo = cloneAdaptiveCoordinatorPointer(log.AdaptiveAttemptNo)
	clone.AdaptivePricingSnapshotID = cloneAdaptiveCoordinatorPointer(log.AdaptivePricingSnapshotID)
	clone.AdaptiveReservationID = cloneAdaptiveCoordinatorPointer(log.AdaptiveReservationID)
	clone.AdaptiveEvidenceHash = cloneAdaptiveCoordinatorPointer(log.AdaptiveEvidenceHash)
	clone.AdaptiveSettlementStatus = cloneAdaptiveCoordinatorPointer(log.AdaptiveSettlementStatus)
	return &clone
}

func cloneAdaptiveCoordinatorPointer[T any](value *T) *T {
	if value == nil {
		return nil
	}
	clone := *value
	return &clone
}
