package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

type adaptiveReconcilerReservationRepoStub struct {
	mu sync.Mutex

	claimResult *UsageReservationReconcileResult
	claimErr    error
	claimCmd    *UsageReservationReconcileCommand
	claimCalled chan struct{}

	markFailedFn func(context.Context, *UsageReservationAttemptFailedCommand) (*UsageReservationResult, error)
	captureFn    func(context.Context, *UsageReservationCaptureCommand) (*UsageReservationResult, error)
	releaseFn    func(context.Context, *UsageReservationReleaseCommand) (*UsageReservationResult, error)
	voidFn       func(context.Context, string, int64, time.Time, string) (int64, error)

	markFailedCalls []*UsageReservationAttemptFailedCommand
	captureCalls    []*UsageReservationCaptureCommand
	releaseCalls    []*UsageReservationReleaseCommand
	voidCalls       int
}

func (s *adaptiveReconcilerReservationRepoStub) Reserve(context.Context, *UsageReservationReserveCommand) (*UsageReservationResult, error) {
	return nil, errors.New("unexpected Reserve call")
}

func (s *adaptiveReconcilerReservationRepoStub) MarkInFlight(context.Context, *UsageReservationMarkInFlightCommand) (*UsageReservationResult, error) {
	return nil, errors.New("unexpected MarkInFlight call")
}

func (s *adaptiveReconcilerReservationRepoStub) MarkAttemptFailed(ctx context.Context, cmd *UsageReservationAttemptFailedCommand) (*UsageReservationResult, error) {
	s.mu.Lock()
	s.markFailedCalls = append(s.markFailedCalls, cmd)
	fn := s.markFailedFn
	s.mu.Unlock()
	if fn == nil {
		return nil, errors.New("unexpected MarkAttemptFailed call")
	}
	return fn(ctx, cmd)
}

func (s *adaptiveReconcilerReservationRepoStub) Capture(ctx context.Context, cmd *UsageReservationCaptureCommand) (*UsageReservationResult, error) {
	s.mu.Lock()
	s.captureCalls = append(s.captureCalls, cmd)
	fn := s.captureFn
	s.mu.Unlock()
	if fn == nil {
		return nil, errors.New("unexpected Capture call")
	}
	return fn(ctx, cmd)
}

func (s *adaptiveReconcilerReservationRepoStub) Release(ctx context.Context, cmd *UsageReservationReleaseCommand) (*UsageReservationResult, error) {
	s.mu.Lock()
	s.releaseCalls = append(s.releaseCalls, cmd)
	fn := s.releaseFn
	s.mu.Unlock()
	if fn == nil {
		return nil, errors.New("unexpected Release call")
	}
	return fn(ctx, cmd)
}

func (s *adaptiveReconcilerReservationRepoStub) Renew(context.Context, *UsageReservationRenewCommand) (*UsageReservationResult, error) {
	return nil, errors.New("unexpected Renew call")
}

type adaptiveReconcilerVoidingReservationRepoStub struct {
	*adaptiveReconcilerReservationRepoStub
}

func (s *adaptiveReconcilerVoidingReservationRepoStub) VoidPendingAdaptiveUsage(
	ctx context.Context,
	reservationID string,
	usageLogID int64,
	usageLogCreatedAt time.Time,
	evidenceHash string,
) (int64, error) {
	s.mu.Lock()
	s.voidCalls++
	if len(s.markFailedCalls) != 0 {
		s.mu.Unlock()
		return 0, errors.New("void must run before MarkAttemptFailed; pending usage blocks fail-attempt")
	}
	fn := s.voidFn
	s.mu.Unlock()
	if fn == nil {
		return 1, nil
	}
	return fn(ctx, reservationID, usageLogID, usageLogCreatedAt, evidenceHash)
}

func (s *adaptiveReconcilerReservationRepoStub) ReconcileExpired(ctx context.Context, cmd *UsageReservationReconcileCommand) (*UsageReservationReconcileResult, error) {
	s.mu.Lock()
	s.claimCmd = cmd
	called := s.claimCalled
	result := s.claimResult
	err := s.claimErr
	s.mu.Unlock()
	if called != nil {
		select {
		case called <- struct{}{}:
		default:
		}
	}
	if result == nil && err == nil {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return result, err
}

type adaptiveReconcilerEvidenceRepoStub struct {
	inspect func(context.Context, string) (*AdaptiveReconciliationEvidenceSnapshot, error)
}

func (s *adaptiveReconcilerEvidenceRepoStub) Inspect(ctx context.Context, reservationID string) (*AdaptiveReconciliationEvidenceSnapshot, error) {
	return s.inspect(ctx, reservationID)
}

func adaptiveReconcilerOptions(workerID string) AdaptiveReservationReconcilerOptions {
	return AdaptiveReservationReconcilerOptions{
		Interval:     time.Hour,
		BatchSize:    10,
		Concurrency:  2,
		CycleTimeout: time.Second,
		ItemTimeout:  500 * time.Millisecond,
		ClaimTTL:     time.Minute,
		WorkerID:     workerID,
	}
}

func adaptiveClaimFixture(workerID, id string, inFlight bool) UsageBillingReservation {
	parentGroupID := int64(10)
	reconciliationLease := time.Now().Add(time.Minute)
	reservation := UsageBillingReservation{
		ID:                           id,
		LogicalRequestID:             "request-" + id,
		UserID:                       11,
		APIKeyID:                     12,
		ParentGroupID:                &parentGroupID,
		CanonicalModel:               "model",
		PricingSnapshotID:            "pricing-v1",
		FundingSource:                UsageReservationFundingBalance,
		Status:                       UsageReservationStatusReconciling,
		ManagementFeeBPS:             DefaultAdaptiveManagementFeeBPS,
		HeldBaseCost:                 decimal.RequireFromString("10.0000000000"),
		HeldManagementFee:            decimal.RequireFromString("1.5000000000"),
		HeldTotal:                    decimal.RequireFromString("11.5000000000"),
		ReconcileFromStatus:          UsageReservationStatusAuthorized,
		OwnerID:                      workerID,
		FencingToken:                 4,
		RowVersion:                   7,
		ReconciliationLeaseExpiresAt: &reconciliationLease,
	}
	if inFlight {
		leafGroupID := int64(20)
		attemptNo := 1
		attemptStartedAt := time.Now().UTC().Add(-3 * time.Hour)
		leaseExpiresAt := time.Now().UTC().Add(-6 * time.Minute)
		reservation.ActiveLeafGroupID = &leafGroupID
		reservation.ActiveAttemptNo = &attemptNo
		reservation.AttemptStartedAt = &attemptStartedAt
		reservation.LeaseExpiresAt = leaseExpiresAt
		reservation.ReconcileFromStatus = UsageReservationStatusInFlight
	}
	return reservation
}

func adaptivePendingEvidenceFixture(reservation UsageBillingReservation) *AdaptivePendingUsageEvidence {
	return &AdaptivePendingUsageEvidence{
		UsageLogID:              99,
		UsageLogCreatedAt:       time.Now().UTC().Round(time.Microsecond),
		ReservationID:           reservation.ID,
		UserID:                  reservation.UserID,
		APIKeyID:                reservation.APIKeyID,
		BillingType:             0,
		ParentGroupID:           reservation.ParentGroupID,
		LeafGroupID:             *reservation.ActiveLeafGroupID,
		AttemptNo:               *reservation.ActiveAttemptNo,
		PricingSnapshotID:       reservation.PricingSnapshotID,
		EvidenceHash:            HashUsageReservationKey("usage-evidence-" + reservation.ID),
		BaseCost:                decimal.RequireFromString("2.0000000000"),
		ManagementFeeCost:       decimal.RequireFromString("0.3000000000"),
		TotalCost:               decimal.RequireFromString("2.3000000000"),
		UncappedBaseCost:        decimal.RequireFromString("2.0000000000"),
		PlatformOverageBaseCost: decimal.Zero,
		Success:                 true,
	}
}

func TestAdaptiveReservationReconciler_CapturesPendingUsageEvidence(t *testing.T) {
	workerID := "worker-capture"
	reservation := adaptiveClaimFixture(workerID, "reservation-capture", true)
	recentAttempt := time.Now().UTC().Add(-time.Minute)
	reservation.AttemptStartedAt = &recentAttempt
	evidence := adaptivePendingEvidenceFixture(reservation)
	repo := &adaptiveReconcilerReservationRepoStub{
		claimResult: &UsageReservationReconcileResult{Examined: 1, Claimed: []UsageBillingReservation{reservation}},
	}
	repo.captureFn = func(_ context.Context, cmd *UsageReservationCaptureCommand) (*UsageReservationResult, error) {
		require.Equal(t, evidence.UncappedBaseCost, cmd.ActualBaseCost)
		require.Equal(t, evidence.EvidenceHash, cmd.EvidenceHash)
		captured := reservation
		captured.Status = UsageReservationStatusCaptured
		return &UsageReservationResult{Reservation: &captured, Applied: true}, nil
	}
	evidenceRepo := &adaptiveReconcilerEvidenceRepoStub{inspect: func(context.Context, string) (*AdaptiveReconciliationEvidenceSnapshot, error) {
		return &AdaptiveReconciliationEvidenceSnapshot{
			PendingUsage:        evidence,
			AttemptCount:        1,
			StartedAttemptCount: 1,
		}, nil
	}}

	worker := NewAdaptiveReservationReconciler(repo, evidenceRepo, adaptiveReconcilerOptions(workerID))
	result, err := worker.RunOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, AdaptiveReservationReconciliationRun{Examined: 1, Claimed: 1, Captured: 1}, result)
	require.Len(t, repo.captureCalls, 1)
	require.Empty(t, repo.releaseCalls)
	require.Equal(t, uint64(1), worker.Health().Captured)
}

func TestAdaptiveReservationReconciler_DefersAmbiguousInFlightAttemptUntilLostLeaseGrace(t *testing.T) {
	workerID := "worker-deferred"
	reservation := adaptiveClaimFixture(workerID, "reservation-deferred", true)
	reservation.LeaseExpiresAt = time.Now().UTC().Add(-4 * time.Minute)
	repo := &adaptiveReconcilerReservationRepoStub{
		claimResult: &UsageReservationReconcileResult{Examined: 1, Claimed: []UsageBillingReservation{reservation}},
	}
	evidenceRepo := &adaptiveReconcilerEvidenceRepoStub{inspect: func(context.Context, string) (*AdaptiveReconciliationEvidenceSnapshot, error) {
		return &AdaptiveReconciliationEvidenceSnapshot{AttemptCount: 1, StartedAttemptCount: 1}, nil
	}}

	worker := NewAdaptiveReservationReconciler(repo, evidenceRepo, adaptiveReconcilerOptions(workerID))
	result, err := worker.RunOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, AdaptiveReservationReconciliationRun{Examined: 1, Claimed: 1, Deferred: 1}, result)
	require.Empty(t, repo.markFailedCalls)
	require.Empty(t, repo.captureCalls)
	require.Empty(t, repo.releaseCalls)
	require.Equal(t, uint64(1), worker.Health().Deferred)
	require.Equal(t, uint64(0), worker.Health().Failures)
}

func TestDefaultAdaptiveReservationReconcilerOptions_LostLeaseGraceIsFiveMinutes(t *testing.T) {
	require.Equal(t, 5*time.Minute, DefaultAdaptiveReservationReconcilerOptions().HardHoldAge)
}

func TestAdaptiveReservationReconciler_ReleasesAuthorizedClaimWithoutAttempts(t *testing.T) {
	workerID := "worker-release"
	reservation := adaptiveClaimFixture(workerID, "reservation-release", false)
	repo := &adaptiveReconcilerReservationRepoStub{
		claimResult: &UsageReservationReconcileResult{Examined: 1, Claimed: []UsageBillingReservation{reservation}},
	}
	repo.releaseFn = func(_ context.Context, cmd *UsageReservationReleaseCommand) (*UsageReservationResult, error) {
		require.Empty(t, cmd.EvidenceHash)
		require.Equal(t, adaptiveReconcileReleaseReason, cmd.Reason)
		released := reservation
		released.Status = UsageReservationStatusReleased
		return &UsageReservationResult{Reservation: &released, Applied: true}, nil
	}
	evidenceRepo := &adaptiveReconcilerEvidenceRepoStub{inspect: func(context.Context, string) (*AdaptiveReconciliationEvidenceSnapshot, error) {
		return &AdaptiveReconciliationEvidenceSnapshot{}, nil
	}}

	worker := NewAdaptiveReservationReconciler(repo, evidenceRepo, adaptiveReconcilerOptions(workerID))
	result, err := worker.RunOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, result.Released)
	require.Len(t, repo.releaseCalls, 1)
	require.Empty(t, repo.captureCalls)
}

func TestAdaptiveReservationReconciler_FailsExpiredAttemptThenReleases(t *testing.T) {
	workerID := "worker-failed-attempt"
	reservation := adaptiveClaimFixture(workerID, "reservation-failed-attempt", true)
	recentAttempt := time.Now().UTC().Add(-time.Minute)
	reservation.AttemptStartedAt = &recentAttempt
	var failureHash atomic.Value
	repo := &adaptiveReconcilerReservationRepoStub{
		claimResult: &UsageReservationReconcileResult{Examined: 1, Claimed: []UsageBillingReservation{reservation}},
	}
	repo.markFailedFn = func(_ context.Context, cmd *UsageReservationAttemptFailedCommand) (*UsageReservationResult, error) {
		failureHash.Store(cmd.EvidenceHash)
		updated := reservation
		updated.ActiveAttemptNo = nil
		updated.ActiveLeafGroupID = nil
		updated.RowVersion++
		return &UsageReservationResult{Reservation: &updated, Applied: true}, nil
	}
	repo.releaseFn = func(_ context.Context, cmd *UsageReservationReleaseCommand) (*UsageReservationResult, error) {
		require.Equal(t, failureHash.Load().(string), cmd.EvidenceHash)
		require.Equal(t, int64(8), cmd.RowVersion)
		released := reservation
		released.Status = UsageReservationStatusReleased
		return &UsageReservationResult{Reservation: &released, Applied: true}, nil
	}
	var inspections atomic.Int32
	evidenceRepo := &adaptiveReconcilerEvidenceRepoStub{inspect: func(context.Context, string) (*AdaptiveReconciliationEvidenceSnapshot, error) {
		if inspections.Add(1) == 1 {
			return &AdaptiveReconciliationEvidenceSnapshot{AttemptCount: 1, StartedAttemptCount: 1}, nil
		}
		return &AdaptiveReconciliationEvidenceSnapshot{
			AttemptCount:             1,
			FailedAttemptCount:       1,
			LatestFailedEvidenceHash: failureHash.Load().(string),
		}, nil
	}}

	worker := NewAdaptiveReservationReconciler(repo, evidenceRepo, adaptiveReconcilerOptions(workerID))
	result, err := worker.RunOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, result.Released)
	require.Len(t, repo.markFailedCalls, 1)
	require.Len(t, repo.releaseCalls, 1)
}

func TestAdaptiveReservationReconciler_VoidsOrphanedPendingUsageBeforeFailingAttempt(t *testing.T) {
	workerID := "worker-orphan-void-first"
	reservation := adaptiveClaimFixture(workerID, "reservation-orphan-void-first", true)
	evidence := adaptivePendingEvidenceFixture(reservation)
	evidence.LeafGroupID++
	inner := &adaptiveReconcilerReservationRepoStub{
		claimResult: &UsageReservationReconcileResult{Examined: 1, Claimed: []UsageBillingReservation{reservation}},
	}
	inner.markFailedFn = func(_ context.Context, _ *UsageReservationAttemptFailedCommand) (*UsageReservationResult, error) {
		updated := reservation
		updated.ActiveAttemptNo = nil
		updated.ActiveLeafGroupID = nil
		updated.AttemptStartedAt = nil
		updated.ReconcileFromStatus = UsageReservationStatusAuthorized
		updated.RowVersion++
		return &UsageReservationResult{Reservation: &updated, Applied: true}, nil
	}
	var failureHash atomic.Value
	inner.releaseFn = func(_ context.Context, cmd *UsageReservationReleaseCommand) (*UsageReservationResult, error) {
		require.Equal(t, failureHash.Load().(string), cmd.EvidenceHash)
		released := reservation
		released.Status = UsageReservationStatusReleased
		return &UsageReservationResult{Reservation: &released, Applied: true}, nil
	}
	repo := &adaptiveReconcilerVoidingReservationRepoStub{adaptiveReconcilerReservationRepoStub: inner}
	var inspections atomic.Int32
	evidenceRepo := &adaptiveReconcilerEvidenceRepoStub{inspect: func(context.Context, string) (*AdaptiveReconciliationEvidenceSnapshot, error) {
		switch inspections.Add(1) {
		case 1:
			return &AdaptiveReconciliationEvidenceSnapshot{
				PendingUsage:        evidence,
				AttemptCount:        1,
				StartedAttemptCount: 1,
			}, nil
		default:
			hash := HashUsageReservationKey("orphan-failed")
			failureHash.Store(hash)
			return &AdaptiveReconciliationEvidenceSnapshot{
				AttemptCount:             1,
				FailedAttemptCount:       1,
				LatestFailedEvidenceHash: hash,
			}, nil
		}
	}}

	worker := NewAdaptiveReservationReconciler(repo, evidenceRepo, adaptiveReconcilerOptions(workerID))
	result, err := worker.RunOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, result.Released)
	require.Equal(t, 1, inner.voidCalls)
	require.Len(t, inner.markFailedCalls, 1)
	require.Len(t, inner.releaseCalls, 1)
	require.Empty(t, inner.captureCalls)
}

func TestAdaptiveReservationReconciler_FailsClosedWhenUsageAppearsAfterAttemptFailure(t *testing.T) {
	workerID := "worker-late-usage"
	reservation := adaptiveClaimFixture(workerID, "reservation-late-usage", true)
	pendingUsage := adaptivePendingEvidenceFixture(reservation)
	repo := &adaptiveReconcilerReservationRepoStub{
		claimResult: &UsageReservationReconcileResult{Examined: 1, Claimed: []UsageBillingReservation{reservation}},
	}
	repo.markFailedFn = func(_ context.Context, _ *UsageReservationAttemptFailedCommand) (*UsageReservationResult, error) {
		updated := reservation
		updated.ActiveAttemptNo = nil
		updated.ActiveLeafGroupID = nil
		updated.AttemptStartedAt = nil
		updated.RowVersion++
		return &UsageReservationResult{Reservation: &updated, Applied: true}, nil
	}
	repo.captureFn = func(context.Context, *UsageReservationCaptureCommand) (*UsageReservationResult, error) {
		return nil, errors.New("capture must not use evidence for an attempt already marked failed")
	}
	repo.releaseFn = func(context.Context, *UsageReservationReleaseCommand) (*UsageReservationResult, error) {
		return nil, errors.New("release must not discard newly appeared usage")
	}
	var inspections atomic.Int32
	evidenceRepo := &adaptiveReconcilerEvidenceRepoStub{inspect: func(context.Context, string) (*AdaptiveReconciliationEvidenceSnapshot, error) {
		if inspections.Add(1) == 1 {
			return &AdaptiveReconciliationEvidenceSnapshot{AttemptCount: 1, StartedAttemptCount: 1}, nil
		}
		return &AdaptiveReconciliationEvidenceSnapshot{
			PendingUsage:       pendingUsage,
			AttemptCount:       1,
			FailedAttemptCount: 1,
		}, nil
	}}

	worker := NewAdaptiveReservationReconciler(repo, evidenceRepo, adaptiveReconcilerOptions(workerID))
	result, err := worker.RunOnce(context.Background())
	require.ErrorIs(t, err, ErrAdaptiveReconciliationStateConflict)
	require.Equal(t, AdaptiveReservationReconciliationRun{Examined: 1, Claimed: 1, Failed: 1}, result)
	require.Len(t, repo.markFailedCalls, 1)
	require.Empty(t, repo.captureCalls)
	require.Empty(t, repo.releaseCalls)
}

func TestAdaptiveReservationReconciler_MalformedEvidenceFailsClosed(t *testing.T) {
	workerID := "worker-malformed"
	reservation := adaptiveClaimFixture(workerID, "reservation-malformed", true)
	evidence := adaptivePendingEvidenceFixture(reservation)
	evidence.UserID++
	repo := &adaptiveReconcilerReservationRepoStub{
		claimResult: &UsageReservationReconcileResult{Examined: 1, Claimed: []UsageBillingReservation{reservation}},
		captureFn: func(context.Context, *UsageReservationCaptureCommand) (*UsageReservationResult, error) {
			return nil, errors.New("capture must not be called")
		},
		releaseFn: func(context.Context, *UsageReservationReleaseCommand) (*UsageReservationResult, error) {
			return nil, errors.New("release must not be called")
		},
	}
	evidenceRepo := &adaptiveReconcilerEvidenceRepoStub{inspect: func(context.Context, string) (*AdaptiveReconciliationEvidenceSnapshot, error) {
		return &AdaptiveReconciliationEvidenceSnapshot{PendingUsage: evidence}, nil
	}}

	worker := NewAdaptiveReservationReconciler(repo, evidenceRepo, adaptiveReconcilerOptions(workerID))
	result, err := worker.RunOnce(context.Background())
	require.ErrorIs(t, err, ErrAdaptiveReconciliationEvidenceInvalid)
	require.Equal(t, 1, result.Failed)
	require.Empty(t, repo.captureCalls)
	require.Empty(t, repo.releaseCalls)
	require.Equal(t, uint64(1), worker.Health().Failures)
}

func TestAdaptiveReservationReconciler_BoundsBatchConcurrency(t *testing.T) {
	workerID := "worker-bounded"
	reservations := make([]UsageBillingReservation, 3)
	for i := range reservations {
		reservations[i] = adaptiveClaimFixture(workerID, "reservation-bounded-"+string(rune('a'+i)), false)
	}
	var active atomic.Int32
	var maximum atomic.Int32
	repo := &adaptiveReconcilerReservationRepoStub{
		claimResult: &UsageReservationReconcileResult{Examined: 3, Claimed: reservations},
	}
	repo.releaseFn = func(_ context.Context, cmd *UsageReservationReleaseCommand) (*UsageReservationResult, error) {
		current := active.Add(1)
		for {
			observed := maximum.Load()
			if current <= observed || maximum.CompareAndSwap(observed, current) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		active.Add(-1)
		released := adaptiveClaimFixture(workerID, cmd.ReservationID, false)
		released.Status = UsageReservationStatusReleased
		return &UsageReservationResult{Reservation: &released, Applied: true}, nil
	}
	evidenceRepo := &adaptiveReconcilerEvidenceRepoStub{inspect: func(context.Context, string) (*AdaptiveReconciliationEvidenceSnapshot, error) {
		return &AdaptiveReconciliationEvidenceSnapshot{}, nil
	}}
	options := adaptiveReconcilerOptions(workerID)
	options.BatchSize = 3
	options.Concurrency = 2
	worker := NewAdaptiveReservationReconciler(repo, evidenceRepo, options)

	result, err := worker.RunOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, 3, result.Released)
	require.Equal(t, int32(2), maximum.Load())
	require.Equal(t, 3, repo.claimCmd.Limit)
	require.Equal(t, options.ClaimTTL, repo.claimCmd.ClaimTTL)
}

func TestAdaptiveReservationReconciler_StopCancelsInFlightClaim(t *testing.T) {
	workerID := "worker-stop"
	claimCalled := make(chan struct{}, 1)
	repo := &adaptiveReconcilerReservationRepoStub{claimCalled: claimCalled}
	evidenceRepo := &adaptiveReconcilerEvidenceRepoStub{inspect: func(context.Context, string) (*AdaptiveReconciliationEvidenceSnapshot, error) {
		return nil, errors.New("unexpected Inspect call")
	}}
	worker := NewAdaptiveReservationReconciler(repo, evidenceRepo, adaptiveReconcilerOptions(workerID))
	worker.Start()
	select {
	case <-claimCalled:
	case <-time.After(time.Second):
		t.Fatal("worker did not start claim")
	}

	stopped := make(chan struct{})
	go func() {
		worker.Stop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("worker did not stop after cancellation")
	}
	require.False(t, worker.Health().Running)
}

func TestAdaptiveReservationReconciler_WaitsForStartupGate(t *testing.T) {
	claimCalled := make(chan struct{}, 1)
	repo := &adaptiveReconcilerReservationRepoStub{claimCalled: claimCalled}
	evidenceRepo := &adaptiveReconcilerEvidenceRepoStub{inspect: func(context.Context, string) (*AdaptiveReconciliationEvidenceSnapshot, error) {
		return &AdaptiveReconciliationEvidenceSnapshot{}, nil
	}}
	options := adaptiveReconcilerOptions("worker-startup-gate")
	options.StartGateFile = filepath.Join(t.TempDir(), "committed")
	worker := NewAdaptiveReservationReconciler(repo, evidenceRepo, options)
	worker.Start()
	t.Cleanup(worker.Stop)

	select {
	case <-claimCalled:
		t.Fatal("reconciler claimed reservations before deployment commit")
	case <-time.After(100 * time.Millisecond):
	}
	require.NoError(t, os.WriteFile(options.StartGateFile, []byte("committed\n"), 0o600))
	select {
	case <-claimCalled:
	case <-time.After(time.Second):
		t.Fatal("reconciler did not start after deployment commit")
	}
}
