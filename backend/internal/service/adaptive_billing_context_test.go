package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

type adaptiveSettlementReservationStub struct {
	capture func(*UsageReservationCaptureCommand) (*UsageReservationResult, error)
}

func (r *adaptiveSettlementReservationStub) Reserve(context.Context, *UsageReservationReserveCommand) (*UsageReservationResult, error) {
	return nil, errors.New("unexpected Reserve")
}
func (r *adaptiveSettlementReservationStub) MarkInFlight(context.Context, *UsageReservationMarkInFlightCommand) (*UsageReservationResult, error) {
	return nil, errors.New("unexpected MarkInFlight")
}
func (r *adaptiveSettlementReservationStub) MarkAttemptFailed(context.Context, *UsageReservationAttemptFailedCommand) (*UsageReservationResult, error) {
	return nil, errors.New("unexpected MarkAttemptFailed")
}
func (r *adaptiveSettlementReservationStub) Capture(_ context.Context, cmd *UsageReservationCaptureCommand) (*UsageReservationResult, error) {
	return r.capture(cmd)
}
func (r *adaptiveSettlementReservationStub) Release(context.Context, *UsageReservationReleaseCommand) (*UsageReservationResult, error) {
	return nil, errors.New("unexpected Release")
}
func (r *adaptiveSettlementReservationStub) Renew(context.Context, *UsageReservationRenewCommand) (*UsageReservationResult, error) {
	return nil, errors.New("unexpected Renew")
}
func (r *adaptiveSettlementReservationStub) ReconcileExpired(context.Context, *UsageReservationReconcileCommand) (*UsageReservationReconcileResult, error) {
	return nil, errors.New("unexpected ReconcileExpired")
}

type adaptiveSettlementUsageStub struct {
	create func(*UsageLog) (bool, error)
}

func (r *adaptiveSettlementUsageStub) Create(_ context.Context, log *UsageLog) (bool, error) {
	return r.create(log)
}
func (r *adaptiveSettlementUsageStub) GetByID(context.Context, int64) (*UsageLog, error) {
	return nil, errors.New("unexpected GetByID")
}

func mustAdaptiveTestDecimal(v string) decimal.Decimal {
	d, err := decimal.NewFromString(v)
	if err != nil {
		panic(err)
	}
	return d
}

func mustAdaptiveTestTime() time.Time {
	return time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
}

func TestAdaptiveBillingContextValidateForSettlement(t *testing.T) {
	t.Parallel()

	ctx := validAdaptiveBillingContextForTest()
	ctx.ManagementFeeBPS = 0
	require.NoError(t, ctx.ValidateForSettlement())
	require.Equal(t, DefaultAdaptiveManagementFeeBPS, ctx.ManagementFeeBPS)
	require.True(t, ctx.IsCustomerRequest())
}

func TestAdaptiveBillingContextExplicitZeroFeeIsPreserved(t *testing.T) {
	ctx := validAdaptiveBillingContextForTest()
	ctx.ManagementFeeBPS = 0
	ctx.ManagementFeeBPSSet = true

	require.NoError(t, ctx.ValidateForSettlement())
	require.Zero(t, ctx.ManagementFeeBPS)
}

func TestAdaptiveBillingContextSubscriptionFundingRequiresSubscription(t *testing.T) {
	t.Parallel()

	ctx := validAdaptiveBillingContextForTest()
	ctx.FundingSource = UsageReservationFundingSubscription
	require.ErrorIs(t, ctx.ValidateForSettlement(), ErrAdaptiveBillingContextInvalid)

	subscriptionID := int64(44)
	ctx.SubscriptionID = &subscriptionID
	require.NoError(t, ctx.ValidateForSettlement())
}

func TestAdaptiveBillingContextProbeSkipsCustomerSettlement(t *testing.T) {
	t.Parallel()

	ctx := &AdaptiveBillingContext{Probe: true}
	require.False(t, ctx.IsCustomerRequest())
	require.ErrorIs(t, ctx.ValidateForSettlement(), ErrAdaptiveProbeSettlement)
}

func TestRecordUsageFailsClosedWhenAdaptiveCoordinatorMissing(t *testing.T) {
	t.Parallel()

	billing := validAdaptiveBillingContextForTest()
	err := settleAdaptiveCustomerUsage(context.Background(), nil, billing, &UsageLog{}, 1.0)
	require.ErrorIs(t, err, ErrAdaptiveBillingPathNotWired)

	err = settleAdaptiveCustomerUsage(context.Background(), nil, billing, &UsageLog{UserID: 1}, 0)
	require.ErrorIs(t, err, ErrAdaptiveBillingPathNotWired)
}

func TestSettleAdaptiveCustomerUsageCapturesFifteenPercentFee(t *testing.T) {
	t.Parallel()

	billing := validAdaptiveBillingContextForTest()
	billing.HeldBaseCost = mustAdaptiveTestDecimal("2.0000000000")
	billing.HeldManagementFee = mustAdaptiveTestDecimal("0.3000000000")
	billing.HeldTotal = mustAdaptiveTestDecimal("2.3000000000")
	billing.ManagementFeeBPS = DefaultAdaptiveManagementFeeBPS

	var capturedBase string
	var usageCreated bool
	coordinator := NewAdaptiveBillingCoordinator(
		&adaptiveSettlementReservationStub{
			capture: func(cmd *UsageReservationCaptureCommand) (*UsageReservationResult, error) {
				capturedBase = cmd.ActualBaseCost.StringFixed(AdaptiveBillingMoneyScale)
				return &UsageReservationResult{
					Applied: true,
					Reservation: &UsageBillingReservation{
						ID: billing.ReservationID, OwnerID: billing.OwnerID,
						FencingToken: billing.FencingToken, RowVersion: billing.RowVersion + 1,
						Status: UsageReservationStatusCaptured,
					},
				}, nil
			},
		},
		&adaptiveSettlementUsageStub{
			create: func(log *UsageLog) (bool, error) {
				usageCreated = true
				log.ID = 801
				log.CreatedAt = time.Date(2026, 9, 4, 13, 0, 0, 0, time.UTC)
				require.NotNil(t, log.AdaptiveManagementFeeCost)
				require.InDelta(t, 0.15, *log.AdaptiveManagementFeeCost, 1e-9)
				require.NotNil(t, log.AdaptiveTotalCost)
				require.InDelta(t, 1.15, *log.AdaptiveTotalCost, 1e-9)
				return true, nil
			},
		},
	)

	log := &UsageLog{UserID: 1, APIKeyID: 2, AccountID: 3, RequestID: "r1", Model: "gpt-test"}
	err := settleAdaptiveCustomerUsage(context.Background(), coordinator, billing, log, 1.0)
	require.NoError(t, err)
	require.True(t, usageCreated)
	require.Equal(t, "1.0000000000", capturedBase)
	require.NotNil(t, log.AdaptiveSettlementStatus)
	require.Equal(t, AdaptiveSettlementStatusCaptured, *log.AdaptiveSettlementStatus)
	require.InDelta(t, 1.15, log.ActualCost, 1e-9)
}

func TestRecordUsageSkipsCustomerBillingForAdaptiveProbe(t *testing.T) {
	t.Parallel()

	probe := &AdaptiveBillingContext{Probe: true}
	require.NoError(t, (&GatewayService{}).RecordUsage(context.Background(), &RecordUsageInput{AdaptiveBilling: probe}))
	require.NoError(t, (&OpenAIGatewayService{}).RecordUsage(context.Background(), &OpenAIRecordUsageInput{
		Result:          &OpenAIForwardResult{},
		AdaptiveBilling: probe,
	}))
}

func validAdaptiveBillingContextForTest() *AdaptiveBillingContext {
	return &AdaptiveBillingContext{
		ReservationID:     "8ef6ac46-74eb-40b4-9585-125b3b4ec6f6",
		LogicalRequestID:  "req-adaptive-1",
		OwnerID:           "gateway-instance-1",
		FencingToken:      3,
		RowVersion:        4,
		ParentGroupID:     101,
		LeafGroupID:       202,
		AttemptNo:         2,
		PricingGeneration: 17,
		ConfigGeneration:  9,
		PricingSnapshotID: "pricing-generation-17",
		FundingSource:     UsageReservationFundingBalance,
	}
}
