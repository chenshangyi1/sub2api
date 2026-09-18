package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

const (
	adaptiveReconcileDefaultInterval     = 2 * time.Second
	adaptiveReconcileDefaultBatchSize    = 100
	adaptiveReconcileDefaultConcurrency  = 8
	adaptiveReconcileDefaultCycleTimeout = 30 * time.Second
	adaptiveReconcileDefaultItemTimeout  = 5 * time.Second
	adaptiveReconcileDefaultHardHoldAge  = 5 * time.Minute

	adaptiveReconcileReleaseReason = "reconciled_without_usage_evidence"

	// adaptiveReconcileStuckAlertCycles is the number of consecutive failed
	// reconcile cycles for one reservation before an operator alert fires (H2).
	adaptiveReconcileStuckAlertCycles = 30

	AdaptiveReconciliationOutcomeDeferred = "deferred"

	// AdaptiveSettlementStatusRejected marks a pending usage row voided by the
	// reconciliation orphan fallback so the reservation can be released (H2).
	AdaptiveSettlementStatusRejected = "rejected"
)

var (
	ErrAdaptiveReconciliationEvidenceInvalid = errors.New("adaptive reconciliation evidence is invalid")
	ErrAdaptiveReconciliationStateConflict   = errors.New("adaptive reconciliation state conflicts with durable evidence")
)

// AdaptivePendingUsageEvidence is the immutable usage row needed to finish a
// capture after the request process exits between usage insert and settlement.
type AdaptivePendingUsageEvidence struct {
	UsageLogID              int64
	UsageLogCreatedAt       time.Time
	ReservationID           string
	UserID                  int64
	APIKeyID                int64
	SubscriptionID          *int64
	BillingType             int8
	ParentGroupID           *int64
	LeafGroupID             int64
	AttemptNo               int
	PricingSnapshotID       string
	EvidenceHash            string
	BaseCost                decimal.Decimal
	ManagementFeeCost       decimal.Decimal
	TotalCost               decimal.Decimal
	UncappedBaseCost        decimal.Decimal
	PlatformOverageBaseCost decimal.Decimal
	Success                 bool
}

// AdaptiveReconciliationEvidenceSnapshot combines pending customer usage with
// the attempt state required to prove that a release is valid.
type AdaptiveReconciliationEvidenceSnapshot struct {
	PendingUsage             *AdaptivePendingUsageEvidence
	AttemptCount             int
	StartedAttemptCount      int
	SucceededAttemptCount    int
	FailedAttemptCount       int
	LatestFailedEvidenceHash string
}

type AdaptiveReconciliationEvidenceRepository interface {
	Inspect(ctx context.Context, reservationID string) (*AdaptiveReconciliationEvidenceSnapshot, error)
}

type AdaptiveReservationReconcilerOptions struct {
	Interval      time.Duration
	BatchSize     int
	Concurrency   int
	CycleTimeout  time.Duration
	ItemTimeout   time.Duration
	ClaimTTL      time.Duration
	HardHoldAge   time.Duration
	WorkerID      string
	StartGateFile string
}

func DefaultAdaptiveReservationReconcilerOptions() AdaptiveReservationReconcilerOptions {
	return AdaptiveReservationReconcilerOptions{
		Interval:      adaptiveReconcileDefaultInterval,
		BatchSize:     adaptiveReconcileDefaultBatchSize,
		Concurrency:   adaptiveReconcileDefaultConcurrency,
		CycleTimeout:  adaptiveReconcileDefaultCycleTimeout,
		ItemTimeout:   adaptiveReconcileDefaultItemTimeout,
		ClaimTTL:      UsageReservationReconcileClaimTTL,
		HardHoldAge:   adaptiveReconcileDefaultHardHoldAge,
		WorkerID:      uuid.NewString(),
		StartGateFile: strings.TrimSpace(os.Getenv("SUB2API_ADAPTIVE_RECONCILIATION_GATE_FILE")),
	}
}

func (o AdaptiveReservationReconcilerOptions) normalized() AdaptiveReservationReconcilerOptions {
	defaults := DefaultAdaptiveReservationReconcilerOptions()
	if o.Interval <= 0 {
		o.Interval = defaults.Interval
	}
	if o.BatchSize <= 0 {
		o.BatchSize = defaults.BatchSize
	}
	if o.BatchSize > 1000 {
		o.BatchSize = 1000
	}
	if o.Concurrency <= 0 {
		o.Concurrency = defaults.Concurrency
	}
	if o.Concurrency > o.BatchSize {
		o.Concurrency = o.BatchSize
	}
	if o.CycleTimeout <= 0 {
		o.CycleTimeout = defaults.CycleTimeout
	}
	if o.ItemTimeout <= 0 {
		o.ItemTimeout = defaults.ItemTimeout
	}
	if o.ClaimTTL <= 0 {
		o.ClaimTTL = defaults.ClaimTTL
	}
	if o.HardHoldAge <= 0 {
		o.HardHoldAge = defaults.HardHoldAge
	}
	minimumClaimTTL := o.CycleTimeout + o.ItemTimeout + time.Second
	if o.ClaimTTL < minimumClaimTTL {
		o.ClaimTTL = minimumClaimTTL
	}
	if strings.TrimSpace(o.WorkerID) == "" {
		o.WorkerID = defaults.WorkerID
	} else {
		o.WorkerID = strings.TrimSpace(o.WorkerID)
	}
	if strings.TrimSpace(o.StartGateFile) == "" {
		o.StartGateFile = defaults.StartGateFile
	} else {
		o.StartGateFile = strings.TrimSpace(o.StartGateFile)
	}
	return o
}

type AdaptiveReservationReconciliationRun struct {
	Examined int
	Claimed  int
	Captured int
	Released int
	Deferred int
	Failed   int
}

type AdaptiveReservationReconcilerHealth struct {
	Running           bool          `json:"running"`
	Cycles            uint64        `json:"cycles"`
	Claimed           uint64        `json:"claimed"`
	Captured          uint64        `json:"captured"`
	Released          uint64        `json:"released"`
	Deferred          uint64        `json:"deferred"`
	Failures          uint64        `json:"failures"`
	FallbackReleases  uint64        `json:"fallback_releases"`
	StuckAlerts       uint64        `json:"stuck_alerts"`
	LastCycleDuration time.Duration `json:"last_cycle_duration"`
	LastError         string        `json:"last_error,omitempty"`
}

// adaptiveReservationVoidRepository is the optional capability used by the
// orphan-pending-usage fallback (H2). It is implemented by the production
// reservation repository; stubs without it fail closed instead of releasing.
type adaptiveReservationVoidRepository interface {
	VoidPendingAdaptiveUsage(ctx context.Context, reservationID string, usageLogID int64, usageLogCreatedAt time.Time, evidenceHash string) (int64, error)
}

type AdaptiveReservationReconciler struct {
	reservations UsageBillingReservationRepository
	evidence     AdaptiveReconciliationEvidenceRepository
	options      AdaptiveReservationReconcilerOptions

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	start  sync.Once
	stop   sync.Once
	cycle  sync.Mutex

	running          atomic.Bool
	cycles           atomic.Uint64
	claimed          atomic.Uint64
	captured         atomic.Uint64
	released         atomic.Uint64
	deferred         atomic.Uint64
	failures         atomic.Uint64
	fallbackReleases atomic.Uint64
	stuckAlerts      atomic.Uint64
	lastCycleNanos   atomic.Int64
	lastError        atomic.Value

	stuckMu   sync.Mutex
	stuckSeen map[string]int
}

func NewAdaptiveReservationReconciler(
	reservations UsageBillingReservationRepository,
	evidence AdaptiveReconciliationEvidenceRepository,
	options ...AdaptiveReservationReconcilerOptions,
) *AdaptiveReservationReconciler {
	resolved := DefaultAdaptiveReservationReconcilerOptions()
	if len(options) > 0 {
		resolved = options[0].normalized()
	}
	ctx, cancel := context.WithCancel(context.Background())
	worker := &AdaptiveReservationReconciler{
		reservations: reservations,
		evidence:     evidence,
		options:      resolved,
		ctx:          ctx,
		cancel:       cancel,
	}
	worker.lastError.Store("")
	return worker
}

// ProvideAdaptiveReservationReconciler starts the process worker. PostgreSQL
// SKIP LOCKED claims and fencing tokens coordinate multiple server instances.
func ProvideAdaptiveReservationReconciler(
	reservations UsageBillingReservationRepository,
	evidence AdaptiveReconciliationEvidenceRepository,
) *AdaptiveReservationReconciler {
	worker := NewAdaptiveReservationReconciler(reservations, evidence)
	worker.Start()
	return worker
}

func (w *AdaptiveReservationReconciler) Start() {
	if w == nil || w.reservations == nil || w.evidence == nil {
		return
	}
	w.start.Do(func() {
		w.running.Store(true)
		w.wg.Add(1)
		go w.run()
	})
}

func (w *AdaptiveReservationReconciler) Stop() {
	if w == nil {
		return
	}
	w.stop.Do(func() {
		w.cancel()
		w.wg.Wait()
		w.running.Store(false)
	})
}

func (w *AdaptiveReservationReconciler) run() {
	defer w.wg.Done()
	defer w.running.Store(false)
	if !w.waitForStartGate() {
		return
	}
	ticker := time.NewTicker(w.options.Interval)
	defer ticker.Stop()

	for {
		w.runScheduledCycle()
		select {
		case <-w.ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (w *AdaptiveReservationReconciler) waitForStartGate() bool {
	gateFile := strings.TrimSpace(w.options.StartGateFile)
	if gateFile == "" {
		return true
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	lastError := ""
	for {
		if _, err := os.Stat(gateFile); err == nil {
			slog.Info("adaptive reservation reconciliation startup gate opened", "worker_id", w.options.WorkerID, "gate_file", gateFile)
			return true
		} else if !errors.Is(err, os.ErrNotExist) && err.Error() != lastError {
			lastError = err.Error()
			slog.Warn("adaptive reservation reconciliation startup gate check failed", "worker_id", w.options.WorkerID, "gate_file", gateFile, "error", err)
		}
		select {
		case <-w.ctx.Done():
			return false
		case <-ticker.C:
		}
	}
}

func (w *AdaptiveReservationReconciler) runScheduledCycle() {
	ctx, cancel := context.WithTimeout(w.ctx, w.options.CycleTimeout)
	result, err := w.RunOnce(ctx)
	cancel()
	if err != nil && w.ctx.Err() == nil {
		slog.Warn("adaptive reservation reconciliation cycle failed",
			"worker_id", w.options.WorkerID,
			"claimed", result.Claimed,
			"captured", result.Captured,
			"released", result.Released,
			"deferred", result.Deferred,
			"failed", result.Failed,
			"error", err,
		)
		return
	}
	if result.Captured > 0 || result.Released > 0 || result.Deferred > 0 {
		slog.Info("adaptive reservation reconciliation cycle completed",
			"worker_id", w.options.WorkerID,
			"claimed", result.Claimed,
			"captured", result.Captured,
			"released", result.Released,
			"deferred", result.Deferred,
		)
	}
}

func (w *AdaptiveReservationReconciler) RunOnce(ctx context.Context) (AdaptiveReservationReconciliationRun, error) {
	result := AdaptiveReservationReconciliationRun{}
	if w == nil || w.reservations == nil || w.evidence == nil {
		return result, ErrAdaptiveReconciliationStateConflict
	}
	if ctx == nil {
		ctx = context.Background()
	}

	w.cycle.Lock()
	defer w.cycle.Unlock()
	startedAt := time.Now()
	defer func() {
		w.cycles.Add(1)
		w.lastCycleNanos.Store(time.Since(startedAt).Nanoseconds())
	}()

	cycleCtx, cancel := context.WithTimeout(ctx, w.options.CycleTimeout)
	defer cancel()
	claimed, err := w.reservations.ReconcileExpired(cycleCtx, &UsageReservationReconcileCommand{
		WorkerID: w.options.WorkerID,
		Limit:    w.options.BatchSize,
		ClaimTTL: w.options.ClaimTTL,
	})
	if err != nil {
		w.failures.Add(1)
		w.recordError(err)
		return result, fmt.Errorf("claim expired adaptive reservations: %w", err)
	}
	if claimed == nil {
		err = ErrAdaptiveReconciliationStateConflict
		w.failures.Add(1)
		w.recordError(err)
		return result, err
	}
	result.Examined = claimed.Examined
	result.Claimed = len(claimed.Claimed)
	w.claimed.Add(uint64(result.Claimed))
	if result.Claimed == 0 {
		w.lastError.Store("")
		return result, nil
	}

	var resultMu sync.Mutex
	var failuresMu sync.Mutex
	var firstFailure error
	semaphore := make(chan struct{}, w.options.Concurrency)
	var workers sync.WaitGroup

queueLoop:
	for i := range claimed.Claimed {
		select {
		case <-cycleCtx.Done():
			break queueLoop
		case semaphore <- struct{}{}:
		}
		reservation := claimed.Claimed[i]
		workers.Add(1)
		go func() {
			defer workers.Done()
			defer func() { <-semaphore }()
			itemCtx, itemCancel := context.WithTimeout(cycleCtx, w.options.ItemTimeout)
			outcome, itemErr := w.reconcileOne(itemCtx, &reservation)
			itemCancel()

			resultMu.Lock()
			switch outcome {
			case UsageReservationStatusCaptured:
				result.Captured++
			case UsageReservationStatusReleased:
				result.Released++
			case AdaptiveReconciliationOutcomeDeferred:
				result.Deferred++
			}
			if itemErr != nil {
				result.Failed++
			}
			resultMu.Unlock()
			if itemErr != nil {
				w.noteStuckReservation(reservation.ID)
				failuresMu.Lock()
				if firstFailure == nil {
					firstFailure = itemErr
				}
				failuresMu.Unlock()
				slog.Warn("adaptive reservation reconciliation item failed",
					"worker_id", w.options.WorkerID,
					"reservation_id", reservation.ID,
					"error", itemErr,
				)
			} else {
				w.clearStuckReservation(reservation.ID)
			}
		}()
	}
	workers.Wait()

	if unprocessed := result.Claimed - result.Captured - result.Released - result.Deferred - result.Failed; unprocessed > 0 {
		result.Failed += unprocessed
		if firstFailure == nil {
			firstFailure = cycleCtx.Err()
		}
	}
	w.captured.Add(uint64(result.Captured))
	w.released.Add(uint64(result.Released))
	w.deferred.Add(uint64(result.Deferred))
	if result.Failed > 0 {
		w.failures.Add(uint64(result.Failed))
		w.recordError(firstFailure)
		return result, fmt.Errorf("reconcile adaptive reservations: %d item(s) failed: %w", result.Failed, firstFailure)
	}
	w.lastError.Store("")
	return result, nil
}

func (w *AdaptiveReservationReconciler) reconcileOne(ctx context.Context, reservation *UsageBillingReservation) (string, error) {
	if err := w.validateClaim(reservation); err != nil {
		return "", err
	}
	// Bug #3 fix: Lock evidence at the start of reconciliation to prevent TOCTOU race
	snapshot, err := w.evidence.Inspect(ctx, reservation.ID)
	if err != nil {
		return "", fmt.Errorf("inspect reconciliation evidence: %w", err)
	}
	if snapshot == nil {
		return "", ErrAdaptiveReconciliationEvidenceInvalid
	}
	if snapshot.PendingUsage != nil {
		return w.capturePending(ctx, reservation, snapshot.PendingUsage)
	}

	if reservation.ReconcileFromStatus == UsageReservationStatusInFlight {
		if reservation.ActiveAttemptNo == nil || reservation.ActiveLeafGroupID == nil || reservation.AttemptStartedAt == nil || reservation.LeaseExpiresAt.IsZero() {
			return "", ErrAdaptiveReconciliationStateConflict
		}
		if time.Now().UTC().Before(reservation.LeaseExpiresAt.Add(w.options.HardHoldAge)) {
			return AdaptiveReconciliationOutcomeDeferred, nil
		}
		failureEvidence := hashUsageReservationParts(
			"adaptive-reconcile-expired-v1",
			reservation.ID,
			fmt.Sprintf("%d", *reservation.ActiveAttemptNo),
			fmt.Sprintf("%d", *reservation.ActiveLeafGroupID),
		)
		failed, failErr := w.reservations.MarkAttemptFailed(ctx, &UsageReservationAttemptFailedCommand{
			ReservationID: reservation.ID,
			OperationKey:  fmt.Sprintf("adaptive:reconcile:failed:%s:%d", reservation.ID, *reservation.ActiveAttemptNo),
			OwnerID:       reservation.OwnerID,
			FencingToken:  reservation.FencingToken,
			RowVersion:    reservation.RowVersion,
			AttemptNo:     *reservation.ActiveAttemptNo,
			EvidenceHash:  failureEvidence,
			FailureClass:  "precommit_cancelled",
		})
		if failErr != nil {
			return "", fmt.Errorf("mark expired adaptive attempt failed: %w", failErr)
		}
		if failed == nil || failed.Reservation == nil {
			return "", ErrAdaptiveReconciliationStateConflict
		}
		*reservation = *failed.Reservation

		// MarkAttemptFailed commits new durable evidence. Refresh the snapshot
		// before deciding whether the reservation is now safe to release.
		snapshot, err = w.evidence.Inspect(ctx, reservation.ID)
		if err != nil {
			return "", fmt.Errorf("re-inspect reconciliation evidence: %w", err)
		}
		if snapshot == nil {
			return "", ErrAdaptiveReconciliationEvidenceInvalid
		}
		// MarkAttemptFailed clears the active attempt. Pending usage discovered
		// after that transition cannot satisfy Capture's active-attempt fence.
		if snapshot.PendingUsage != nil {
			return "", ErrAdaptiveReconciliationStateConflict
		}
	} else if reservation.ReconcileFromStatus != UsageReservationStatusAuthorized ||
		reservation.ActiveAttemptNo != nil || reservation.ActiveLeafGroupID != nil || reservation.AttemptStartedAt != nil {
		return "", ErrAdaptiveReconciliationStateConflict
	}

	if snapshot.StartedAttemptCount != 0 || snapshot.SucceededAttemptCount != 0 ||
		snapshot.AttemptCount != snapshot.FailedAttemptCount {
		return "", ErrAdaptiveReconciliationStateConflict
	}
	releaseEvidence := strings.TrimSpace(snapshot.LatestFailedEvidenceHash)
	if snapshot.AttemptCount > 0 && !isUsageReservationSHA256(releaseEvidence) {
		return "", ErrAdaptiveReconciliationEvidenceInvalid
	}
	released, err := w.reservations.Release(ctx, &UsageReservationReleaseCommand{
		ReservationID: reservation.ID,
		OperationKey:  "adaptive:reconcile:release:" + reservation.ID,
		OwnerID:       reservation.OwnerID,
		FencingToken:  reservation.FencingToken,
		RowVersion:    reservation.RowVersion,
		Reason:        adaptiveReconcileReleaseReason,
		EvidenceHash:  releaseEvidence,
	})
	if err != nil {
		return "", fmt.Errorf("release expired adaptive reservation: %w", err)
	}
	if released == nil || released.Reservation == nil || released.Reservation.Status != UsageReservationStatusReleased {
		return "", ErrAdaptiveReconciliationStateConflict
	}
	return UsageReservationStatusReleased, nil
}

func (w *AdaptiveReservationReconciler) capturePending(
	ctx context.Context,
	reservation *UsageBillingReservation,
	evidence *AdaptivePendingUsageEvidence,
) (string, error) {
	if validateAdaptivePendingUsageEvidence(reservation, evidence) == nil {
		return w.capturePendingWith(ctx, reservation, evidence)
	}
	// H2 fallback: the pending usage can no longer bind to the active attempt
	// fence (active was cleared by a previous reconcile/gateway failure), but
	// the usage evidence itself is still fully verifiable. Without a fallback
	// this reservation loops in reconciling forever with funds frozen.
	if err := validateAdaptivePendingUsageEvidenceCore(reservation, evidence); err != nil {
		return "", err
	}
	return w.resolveOrphanedPendingUsage(ctx, reservation, evidence)
}

func (w *AdaptiveReservationReconciler) capturePendingWith(
	ctx context.Context,
	reservation *UsageBillingReservation,
	evidence *AdaptivePendingUsageEvidence,
) (string, error) {
	captured, err := w.reservations.Capture(ctx, &UsageReservationCaptureCommand{
		ReservationID:      reservation.ID,
		OperationKey:       "adaptive:reconcile:capture:" + reservation.ID,
		OwnerID:            reservation.OwnerID,
		FencingToken:       reservation.FencingToken,
		RowVersion:         reservation.RowVersion,
		ActualBaseCost:     evidence.UncappedBaseCost,
		WinningLeafGroupID: evidence.LeafGroupID,
		AttemptNo:          evidence.AttemptNo,
		UsageLogID:         evidence.UsageLogID,
		UsageLogCreatedAt:  evidence.UsageLogCreatedAt,
		EvidenceHash:       evidence.EvidenceHash,
	})
	if err != nil {
		return "", fmt.Errorf("capture expired adaptive reservation: %w", err)
	}
	if captured == nil || captured.Reservation == nil || captured.Reservation.Status != UsageReservationStatusCaptured {
		return "", ErrAdaptiveReconciliationStateConflict
	}
	return UsageReservationStatusCaptured, nil
}

// resolveOrphanedPendingUsage recovers a reservation whose pending usage can
// never be captured because the active-attempt fence no longer matches. It
// voids the orphaned pending usage first (so fail-attempt/release are not
// blocked), solidifies any leftover active attempt as precommit_cancelled,
// then releases the hold so customer funds are never permanently frozen (H2).
func (w *AdaptiveReservationReconciler) resolveOrphanedPendingUsage(
	ctx context.Context,
	reservation *UsageBillingReservation,
	evidence *AdaptivePendingUsageEvidence,
) (string, error) {
	if reservation == nil || evidence == nil {
		return "", ErrAdaptiveReconciliationEvidenceInvalid
	}
	// 1. Void the orphaned pending usage first. MarkAttemptFailed and Release
	//    both refuse any still-pending usage_logs row for this reservation.
	if err := w.voidPendingUsage(ctx, reservation, evidence); err != nil {
		return "", err
	}
	// 2. Solidify a leftover active attempt (if any) as precommit_cancelled so
	//    every attempt is terminal and the reservation has no live fence.
	if reservation.ActiveAttemptNo != nil && reservation.ActiveLeafGroupID != nil {
		solidifyEvidence := hashUsageReservationParts(
			"adaptive-reconcile-orphan-v1",
			reservation.ID,
			fmt.Sprintf("%d", *reservation.ActiveAttemptNo),
			fmt.Sprintf("%d", *reservation.ActiveLeafGroupID),
			evidence.EvidenceHash,
		)
		failed, failErr := w.reservations.MarkAttemptFailed(ctx, &UsageReservationAttemptFailedCommand{
			ReservationID: reservation.ID,
			OperationKey:  fmt.Sprintf("adaptive:reconcile:orphan:%s:%d", reservation.ID, *reservation.ActiveAttemptNo),
			OwnerID:       reservation.OwnerID,
			FencingToken:  reservation.FencingToken,
			RowVersion:    reservation.RowVersion,
			AttemptNo:     *reservation.ActiveAttemptNo,
			EvidenceHash:  solidifyEvidence,
			FailureClass:  "precommit_cancelled",
		})
		if failErr != nil {
			return "", fmt.Errorf("solidify orphaned adaptive attempt: %w", failErr)
		}
		if failed == nil || failed.Reservation == nil {
			return "", ErrAdaptiveReconciliationStateConflict
		}
		*reservation = *failed.Reservation
	}
	// 3. Re-inspect: the reservation must be fully terminal with no remaining
	//    pending usage before the hold is released.
	snapshot, err := w.evidence.Inspect(ctx, reservation.ID)
	if err != nil {
		return "", fmt.Errorf("re-inspect orphaned reconciliation evidence: %w", err)
	}
	if snapshot == nil || snapshot.PendingUsage != nil || snapshot.StartedAttemptCount != 0 ||
		snapshot.SucceededAttemptCount != 0 || snapshot.AttemptCount != snapshot.FailedAttemptCount {
		return "", ErrAdaptiveReconciliationStateConflict
	}
	releaseEvidence := strings.TrimSpace(snapshot.LatestFailedEvidenceHash)
	if snapshot.AttemptCount > 0 && !isUsageReservationSHA256(releaseEvidence) {
		return "", ErrAdaptiveReconciliationEvidenceInvalid
	}
	released, err := w.reservations.Release(ctx, &UsageReservationReleaseCommand{
		ReservationID: reservation.ID,
		OperationKey:  "adaptive:reconcile:orphan-release:" + reservation.ID,
		OwnerID:       reservation.OwnerID,
		FencingToken:  reservation.FencingToken,
		RowVersion:    reservation.RowVersion,
		Reason:        adaptiveReconcileReleaseReason,
		EvidenceHash:  releaseEvidence,
	})
	if err != nil {
		return "", fmt.Errorf("release orphaned adaptive reservation: %w", err)
	}
	if released == nil || released.Reservation == nil || released.Reservation.Status != UsageReservationStatusReleased {
		return "", ErrAdaptiveReconciliationStateConflict
	}
	w.fallbackReleases.Add(1)
	return UsageReservationStatusReleased, nil
}

func (w *AdaptiveReservationReconciler) voidPendingUsage(
	ctx context.Context,
	reservation *UsageBillingReservation,
	evidence *AdaptivePendingUsageEvidence,
) error {
	voidRepo, ok := w.reservations.(adaptiveReservationVoidRepository)
	if !ok {
		// Without the void capability (tests / unregistered backends) fail
		// closed: never release while a pending usage row still exists.
		return ErrAdaptiveReconciliationStateConflict
	}
	affected, err := voidRepo.VoidPendingAdaptiveUsage(ctx, reservation.ID, evidence.UsageLogID, evidence.UsageLogCreatedAt, evidence.EvidenceHash)
	if err != nil {
		return fmt.Errorf("void orphaned adaptive pending usage: %w", err)
	}
	if affected != 1 {
		return ErrAdaptiveReconciliationStateConflict
	}
	return nil
}

// noteStuckReservation counts consecutive failed reconcile cycles per
// reservation and raises an operator alert once the threshold is crossed (H2).
func (w *AdaptiveReservationReconciler) noteStuckReservation(reservationID string) {
	if strings.TrimSpace(reservationID) == "" {
		return
	}
	w.stuckMu.Lock()
	if w.stuckSeen == nil {
		w.stuckSeen = make(map[string]int)
	}
	w.stuckSeen[reservationID]++
	cycles := w.stuckSeen[reservationID]
	w.stuckMu.Unlock()
	if cycles == adaptiveReconcileStuckAlertCycles {
		w.stuckAlerts.Add(1)
		slog.Error("adaptive reservation reconciliation stuck",
			"worker_id", w.options.WorkerID,
			"reservation_id", reservationID,
			"consecutive_failed_cycles", cycles,
			"hint", "reservation is stuck in reconciling; manually inspect or force settle",
		)
	}
}

func (w *AdaptiveReservationReconciler) clearStuckReservation(reservationID string) {
	if strings.TrimSpace(reservationID) == "" {
		return
	}
	w.stuckMu.Lock()
	delete(w.stuckSeen, reservationID)
	w.stuckMu.Unlock()
}

func (w *AdaptiveReservationReconciler) validateClaim(reservation *UsageBillingReservation) error {
	if reservation == nil || reservation.ID == "" || reservation.Status != UsageReservationStatusReconciling ||
		reservation.OwnerID != w.options.WorkerID || reservation.FencingToken <= 0 || reservation.RowVersion <= 0 ||
		reservation.ReconciliationLeaseExpiresAt == nil {
		return ErrAdaptiveReconciliationStateConflict
	}
	return nil
}

func validateAdaptivePendingUsageEvidence(reservation *UsageBillingReservation, evidence *AdaptivePendingUsageEvidence) error {
	if err := validateAdaptivePendingUsageEvidenceCore(reservation, evidence); err != nil {
		return err
	}
	// The active-attempt fence: the pending usage must bind to the attempt the
	// reservation currently holds. When this no longer matches (attempt already
	// solidified/cleared) the caller may use the orphan fallback (H2).
	if reservation.ActiveLeafGroupID == nil || *reservation.ActiveLeafGroupID != evidence.LeafGroupID ||
		reservation.ActiveAttemptNo == nil || *reservation.ActiveAttemptNo != evidence.AttemptNo {
		return ErrAdaptiveReconciliationEvidenceInvalid
	}
	return nil
}

// validateAdaptivePendingUsageEvidenceCore verifies the immutable evidence
// fields (identity, money, hashes, attempt bounds) without the active-attempt
// fence, so the reconciler can distinguish verifiable-but-orphaned usage from
// genuinely malformed evidence (H2).
func validateAdaptivePendingUsageEvidenceCore(reservation *UsageBillingReservation, evidence *AdaptivePendingUsageEvidence) error {
	if reservation == nil || evidence == nil || evidence.UsageLogID <= 0 || evidence.UsageLogCreatedAt.IsZero() ||
		evidence.ReservationID != reservation.ID || evidence.UserID != reservation.UserID || evidence.APIKeyID != reservation.APIKeyID ||
		evidence.LeafGroupID <= 0 || evidence.AttemptNo < 1 || evidence.AttemptNo > AdaptiveMaxLeafAttempts ||
		evidence.PricingSnapshotID != reservation.PricingSnapshotID || !isUsageReservationSHA256(evidence.EvidenceHash) ||
		!optionalInt64Equal(evidence.SubscriptionID, reservation.SubscriptionID) ||
		!optionalInt64Equal(evidence.ParentGroupID, reservation.ParentGroupID) ||
		!evidence.Success {
		return ErrAdaptiveReconciliationEvidenceInvalid
	}
	expectedBillingType := int8(0)
	if reservation.FundingSource == UsageReservationFundingSubscription {
		expectedBillingType = 1
	} else if reservation.FundingSource != UsageReservationFundingBalance {
		return ErrAdaptiveReconciliationEvidenceInvalid
	}
	if evidence.BillingType != expectedBillingType || ValidateUsageReservationMoney(evidence.UncappedBaseCost) != nil {
		return ErrAdaptiveReconciliationEvidenceInvalid
	}
	settlement, err := CalculateAdaptiveManagementFeeSettlementDecimalWithBPS(
		evidence.UncappedBaseCost,
		reservation.HeldBaseCost,
		reservation.HeldManagementFee,
		reservation.HeldTotal,
		reservation.ManagementFeeBPS,
	)
	if err != nil || !evidence.BaseCost.Equal(settlement.CustomerCharge.BaseAmount) ||
		!evidence.ManagementFeeCost.Equal(settlement.CustomerCharge.FeeAmount) ||
		!evidence.TotalCost.Equal(settlement.CustomerCharge.CaptureAmount) ||
		!evidence.PlatformOverageBaseCost.Equal(settlement.PlatformOverageBaseAmount) {
		return ErrAdaptiveReconciliationEvidenceInvalid
	}
	return nil
}

func optionalInt64Equal(left, right *int64) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func (w *AdaptiveReservationReconciler) recordError(err error) {
	if w == nil || err == nil {
		return
	}
	w.lastError.Store(boundedAdaptiveReconciliationError(err))
}

func boundedAdaptiveReconciliationError(err error) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	if len(message) > 1024 {
		return message[:1024]
	}
	return message
}

func (w *AdaptiveReservationReconciler) Health() AdaptiveReservationReconcilerHealth {
	health := AdaptiveReservationReconcilerHealth{}
	if w == nil {
		return health
	}
	health.Running = w.running.Load()
	health.Cycles = w.cycles.Load()
	health.Claimed = w.claimed.Load()
	health.Captured = w.captured.Load()
	health.Released = w.released.Load()
	health.Deferred = w.deferred.Load()
	health.Failures = w.failures.Load()
	health.FallbackReleases = w.fallbackReleases.Load()
	health.StuckAlerts = w.stuckAlerts.Load()
	health.LastCycleDuration = time.Duration(w.lastCycleNanos.Load())
	if value := w.lastError.Load(); value != nil {
		health.LastError, _ = value.(string)
	}
	return health
}
