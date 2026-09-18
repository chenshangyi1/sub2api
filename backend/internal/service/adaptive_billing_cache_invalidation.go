package service

import (
	"context"
	"errors"
	"fmt"
)

// AdaptiveReservationCacheInvalidator removes request-admission snapshots
// after a reservation transaction has committed.
type AdaptiveReservationCacheInvalidator interface {
	InvalidateAdaptiveReservation(ctx context.Context, result *UsageReservationResult) error
}

type adaptiveBillingCacheInvalidator interface {
	InvalidateUserBalance(ctx context.Context, userID int64) error
	InvalidateSubscription(ctx context.Context, userID, groupID int64) error
	PublishSubscriptionCacheInvalidation(ctx context.Context, cacheKey string) error
	InvalidateAPIKeyRateLimit(ctx context.Context, keyID int64) error
}

// AdaptiveBillingCacheInvalidationService coordinates all cache families that
// contain wallet, subscription, or API-key quota state.
type AdaptiveBillingCacheInvalidationService struct {
	billing adaptiveBillingCacheInvalidator
	auth    APIKeyAuthCacheInvalidator
}

var _ AdaptiveReservationCacheInvalidator = (*AdaptiveBillingCacheInvalidationService)(nil)

func NewAdaptiveBillingCacheInvalidationService(
	billing *BillingCacheService,
	auth APIKeyAuthCacheInvalidator,
) *AdaptiveBillingCacheInvalidationService {
	return &AdaptiveBillingCacheInvalidationService{billing: billing, auth: auth}
}

func (s *AdaptiveBillingCacheInvalidationService) InvalidateAdaptiveReservation(
	ctx context.Context,
	result *UsageReservationResult,
) error {
	if s == nil || result == nil || result.Reservation == nil {
		return nil
	}
	reservation := result.Reservation
	if reservation.UserID <= 0 || reservation.APIKeyID <= 0 {
		return fmt.Errorf("invalidate adaptive reservation caches: %w", ErrUsageReservationInvalid)
	}

	var errs []error
	if s.billing != nil {
		if err := s.billing.InvalidateUserBalance(ctx, reservation.UserID); err != nil {
			errs = append(errs, fmt.Errorf("invalidate user balance: %w", err))
		}
		if err := s.billing.InvalidateAPIKeyRateLimit(ctx, reservation.APIKeyID); err != nil {
			errs = append(errs, fmt.Errorf("invalidate api key rate limits: %w", err))
		}
		if reservation.FundingSource == UsageReservationFundingSubscription {
			if reservation.ParentGroupID == nil || *reservation.ParentGroupID <= 0 {
				errs = append(errs, errors.New("invalidate subscription: adaptive parent group is missing"))
			} else {
				groupID := *reservation.ParentGroupID
				if err := s.billing.InvalidateSubscription(ctx, reservation.UserID, groupID); err != nil {
					errs = append(errs, fmt.Errorf("invalidate subscription: %w", err))
				}
				if err := s.billing.PublishSubscriptionCacheInvalidation(ctx, subCacheKey(reservation.UserID, groupID)); err != nil {
					errs = append(errs, fmt.Errorf("publish subscription invalidation: %w", err))
				}
			}
		}
	}
	if s.auth != nil {
		// Auth snapshots contain both user balance and API-key quota usage. The
		// database outbox is the durable cross-instance fallback for this call.
		s.auth.InvalidateAuthCacheByUserID(ctx, reservation.UserID)
	}
	return errors.Join(errs...)
}
