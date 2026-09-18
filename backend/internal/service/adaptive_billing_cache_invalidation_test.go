package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type adaptiveBillingCacheInvalidationStub struct {
	adaptiveBillingCacheInvalidator
	balanceUserIDs     []int64
	subscriptions      [][2]int64
	publishedKeys      []string
	rateLimitAPIKeyIDs []int64
	balanceErr         error
	subscriptionErr    error
	publishErr         error
	rateLimitErr       error
}

func (s *adaptiveBillingCacheInvalidationStub) InvalidateUserBalance(_ context.Context, userID int64) error {
	s.balanceUserIDs = append(s.balanceUserIDs, userID)
	return s.balanceErr
}

func (s *adaptiveBillingCacheInvalidationStub) InvalidateSubscription(_ context.Context, userID, groupID int64) error {
	s.subscriptions = append(s.subscriptions, [2]int64{userID, groupID})
	return s.subscriptionErr
}

func (s *adaptiveBillingCacheInvalidationStub) PublishSubscriptionCacheInvalidation(_ context.Context, cacheKey string) error {
	s.publishedKeys = append(s.publishedKeys, cacheKey)
	return s.publishErr
}

func (s *adaptiveBillingCacheInvalidationStub) InvalidateAPIKeyRateLimit(_ context.Context, apiKeyID int64) error {
	s.rateLimitAPIKeyIDs = append(s.rateLimitAPIKeyIDs, apiKeyID)
	return s.rateLimitErr
}

type adaptiveAuthCacheInvalidationStub struct {
	userIDs []int64
}

func (*adaptiveAuthCacheInvalidationStub) InvalidateAuthCacheByKey(context.Context, string) {}

func (s *adaptiveAuthCacheInvalidationStub) InvalidateAuthCacheByUserID(_ context.Context, userID int64) {
	s.userIDs = append(s.userIDs, userID)
}

func (*adaptiveAuthCacheInvalidationStub) InvalidateAuthCacheByGroupID(context.Context, int64) {}

func TestAdaptiveBillingCacheInvalidationService_BalanceFunding(t *testing.T) {
	billing := &adaptiveBillingCacheInvalidationStub{}
	auth := &adaptiveAuthCacheInvalidationStub{}
	svc := &AdaptiveBillingCacheInvalidationService{billing: billing, auth: auth}

	err := svc.InvalidateAdaptiveReservation(context.Background(), &UsageReservationResult{
		Reservation: &UsageBillingReservation{
			UserID: 41, APIKeyID: 73, FundingSource: UsageReservationFundingBalance,
		},
	})

	require.NoError(t, err)
	require.Equal(t, []int64{41}, billing.balanceUserIDs)
	require.Equal(t, []int64{73}, billing.rateLimitAPIKeyIDs)
	require.Empty(t, billing.subscriptions)
	require.Empty(t, billing.publishedKeys)
	require.Equal(t, []int64{41}, auth.userIDs)
}

func TestAdaptiveBillingCacheInvalidationService_SubscriptionFunding(t *testing.T) {
	billing := &adaptiveBillingCacheInvalidationStub{}
	auth := &adaptiveAuthCacheInvalidationStub{}
	svc := &AdaptiveBillingCacheInvalidationService{billing: billing, auth: auth}
	groupID := int64(19)

	err := svc.InvalidateAdaptiveReservation(context.Background(), &UsageReservationResult{
		Reservation: &UsageBillingReservation{
			UserID: 41, APIKeyID: 73, ParentGroupID: &groupID,
			FundingSource: UsageReservationFundingSubscription,
		},
	})

	require.NoError(t, err)
	require.Equal(t, [][2]int64{{41, 19}}, billing.subscriptions)
	require.Equal(t, []string{subCacheKey(41, 19)}, billing.publishedKeys)
	require.Equal(t, []int64{41}, auth.userIDs)
}

func TestAdaptiveBillingCacheInvalidationService_AttemptsEveryCacheOnErrors(t *testing.T) {
	billing := &adaptiveBillingCacheInvalidationStub{
		balanceErr: errors.New("balance"), subscriptionErr: errors.New("subscription"),
		publishErr: errors.New("publish"), rateLimitErr: errors.New("rate limit"),
	}
	auth := &adaptiveAuthCacheInvalidationStub{}
	svc := &AdaptiveBillingCacheInvalidationService{billing: billing, auth: auth}
	groupID := int64(19)

	err := svc.InvalidateAdaptiveReservation(context.Background(), &UsageReservationResult{
		Reservation: &UsageBillingReservation{
			UserID: 41, APIKeyID: 73, ParentGroupID: &groupID,
			FundingSource: UsageReservationFundingSubscription,
		},
	})

	require.ErrorContains(t, err, "invalidate user balance")
	require.ErrorContains(t, err, "invalidate api key rate limits")
	require.ErrorContains(t, err, "invalidate subscription")
	require.ErrorContains(t, err, "publish subscription invalidation")
	require.Equal(t, []int64{41}, auth.userIDs)
}
