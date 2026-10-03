package service

import (
	"context"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

const adaptiveReservationCacheInvalidationTimeout = 5 * time.Second

// CacheInvalidatingUsageBillingReservationRepository decorates the durable
// reservation repository with post-commit cache cleanup. Cache failures are
// warnings: returning them as operation failures would misrepresent an already
// committed financial mutation and invite an ambiguous retry. Auth snapshots
// also have a durable outbox; the shared billing Redis keys converge on the
// next successful invalidation or their TTL, so failures remain a rollout gate.
type CacheInvalidatingUsageBillingReservationRepository struct {
	delegate    UsageBillingReservationRepository
	invalidator AdaptiveReservationCacheInvalidator
}

var _ UsageBillingReservationRepository = (*CacheInvalidatingUsageBillingReservationRepository)(nil)

func NewCacheInvalidatingUsageBillingReservationRepository(
	delegate UsageBillingReservationRepository,
	invalidator AdaptiveReservationCacheInvalidator,
) UsageBillingReservationRepository {
	if delegate == nil {
		return nil
	}
	return &CacheInvalidatingUsageBillingReservationRepository{
		delegate: delegate, invalidator: invalidator,
	}
}

func (r *CacheInvalidatingUsageBillingReservationRepository) Reserve(
	ctx context.Context,
	cmd *UsageReservationReserveCommand,
) (*UsageReservationResult, error) {
	result, err := r.delegate.Reserve(ctx, cmd)
	if err == nil {
		r.invalidateAfterCommit(ctx, UsageReservationOperationReserve, result)
	}
	return result, err
}

func (r *CacheInvalidatingUsageBillingReservationRepository) MarkInFlight(
	ctx context.Context,
	cmd *UsageReservationMarkInFlightCommand,
) (*UsageReservationResult, error) {
	return r.delegate.MarkInFlight(ctx, cmd)
}

func (r *CacheInvalidatingUsageBillingReservationRepository) MarkAttemptFailed(
	ctx context.Context,
	cmd *UsageReservationAttemptFailedCommand,
) (*UsageReservationResult, error) {
	return r.delegate.MarkAttemptFailed(ctx, cmd)
}

func (r *CacheInvalidatingUsageBillingReservationRepository) Capture(
	ctx context.Context,
	cmd *UsageReservationCaptureCommand,
) (*UsageReservationResult, error) {
	result, err := r.delegate.Capture(ctx, cmd)
	if err == nil {
		r.invalidateAfterCommit(ctx, UsageReservationOperationCapture, result)
	}
	return result, err
}

func (r *CacheInvalidatingUsageBillingReservationRepository) Release(
	ctx context.Context,
	cmd *UsageReservationReleaseCommand,
) (*UsageReservationResult, error) {
	result, err := r.delegate.Release(ctx, cmd)
	if err == nil {
		r.invalidateAfterCommit(ctx, UsageReservationOperationRelease, result)
	}
	return result, err
}

func (r *CacheInvalidatingUsageBillingReservationRepository) Renew(
	ctx context.Context,
	cmd *UsageReservationRenewCommand,
) (*UsageReservationResult, error) {
	result, err := r.delegate.Renew(ctx, cmd)
	if err == nil {
		r.invalidateAfterCommit(ctx, UsageReservationOperationRenew, result)
	}
	return result, err
}

func (r *CacheInvalidatingUsageBillingReservationRepository) ReconcileExpired(
	ctx context.Context,
	cmd *UsageReservationReconcileCommand,
) (*UsageReservationReconcileResult, error) {
	return r.delegate.ReconcileExpired(ctx, cmd)
}

func (r *CacheInvalidatingUsageBillingReservationRepository) invalidateAfterCommit(
	ctx context.Context,
	operation string,
	result *UsageReservationResult,
) {
	if r == nil || r.invalidator == nil || result == nil || result.Reservation == nil {
		return
	}
	parent := context.Background()
	if ctx != nil {
		parent = context.WithoutCancel(ctx)
	}
	cacheCtx, cancel := context.WithTimeout(parent, adaptiveReservationCacheInvalidationTimeout)
	defer cancel()
	if err := r.invalidator.InvalidateAdaptiveReservation(cacheCtx, result); err != nil {
		logger.LegacyPrintf(
			"service.adaptive_billing_cache",
			"post-commit cache invalidation failed: operation=%s reservation_id=%s err=%v",
			operation, result.Reservation.ID, err,
		)
	}
}
