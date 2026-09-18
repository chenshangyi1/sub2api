package service

import (
	"errors"
	"math"
	"strings"

	"github.com/shopspring/decimal"
)

const (
	AdaptiveSettlementStatusPending  = "pending"
	AdaptiveSettlementStatusCaptured = "captured"
)

var (
	ErrAdaptiveBillingContextInvalid = errors.New("adaptive billing context is invalid")
	ErrAdaptiveProbeSettlement       = errors.New("adaptive probe has no customer settlement")
	ErrAdaptiveBillingPathNotWired   = errors.New("adaptive billing settlement path is not wired")
)

// AdaptiveBillingContext is the immutable request-level snapshot handed from
// the Adaptive planner to usage settlement. FencingToken and RowVersion must be
// refreshed from each successful reservation mutation before the next one.
type AdaptiveBillingContext struct {
	ReservationID          string
	LogicalRequestID       string
	OwnerID                string
	FencingToken           int64
	RowVersion             int64
	ParentGroupID          int64
	UserID                 int64
	LeafGroupID            int64
	UserRateMultiplier     float64
	UserRateMultiplierSet  bool
	AttemptNo              int
	PricingGeneration      int64
	ConfigGeneration       int64
	PricingSnapshotID      string
	ManagementFeeBPS       int32
	ManagementFeeBPSSet    bool
	HeldBaseCost           decimal.Decimal
	HeldManagementFee      decimal.Decimal
	HeldTotal              decimal.Decimal
	FundingSource          string
	SubscriptionID         *int64
	LastFailedEvidenceHash string
	Probe                  bool
}

// CloneAdaptiveBillingContext returns a deep copy of the billing context.
// Settlement works on the snapshot while the heartbeat may still renew the
// original under billingMu, so a concurrent Renew writing RowVersion/
// FencingToken can never race with Capture reads on the snapshot.
func CloneAdaptiveBillingContext(src *AdaptiveBillingContext) *AdaptiveBillingContext {
	if src == nil {
		return nil
	}
	dst := *src
	if src.SubscriptionID != nil {
		subID := *src.SubscriptionID
		dst.SubscriptionID = &subID
	}
	return &dst
}

func (c *AdaptiveBillingContext) Normalize() {
	if c == nil {
		return
	}
	c.ReservationID = strings.TrimSpace(c.ReservationID)
	c.LogicalRequestID = strings.TrimSpace(c.LogicalRequestID)
	c.OwnerID = strings.TrimSpace(c.OwnerID)
	c.PricingSnapshotID = strings.TrimSpace(c.PricingSnapshotID)
	c.FundingSource = strings.ToLower(strings.TrimSpace(c.FundingSource))
	// Explicit 0% (ManagementFeeBPSSet) must stay 0; never Normalize 0→1500.
	if c.ManagementFeeBPS == 0 && !c.ManagementFeeBPSSet && !c.Probe {
		c.ManagementFeeBPS = DefaultAdaptiveManagementFeeBPS
	}
	c.ManagementFeeBPSSet = true
}

func (c *AdaptiveBillingContext) IsCustomerRequest() bool {
	return c != nil && !c.Probe
}

func (c *AdaptiveBillingContext) ValidateForSettlement() error {
	if c == nil {
		return ErrAdaptiveBillingContextInvalid
	}
	c.Normalize()
	if c.Probe {
		return ErrAdaptiveProbeSettlement
	}
	if c.ReservationID == "" || c.LogicalRequestID == "" || c.OwnerID == "" ||
		c.FencingToken <= 0 || c.RowVersion <= 0 || c.ParentGroupID <= 0 ||
		c.LeafGroupID <= 0 || c.AttemptNo <= 0 || c.PricingGeneration < 0 ||
		c.ConfigGeneration < 0 || c.PricingSnapshotID == "" {
		return ErrAdaptiveBillingContextInvalid
	}
	if !isAdaptiveManagementFeeBPS(c.ManagementFeeBPS) ||
		(c.UserRateMultiplierSet && (c.UserRateMultiplier < 0 || math.IsNaN(c.UserRateMultiplier) || math.IsInf(c.UserRateMultiplier, 0))) ||
		c.HeldBaseCost.IsNegative() ||
		c.HeldManagementFee.IsNegative() || c.HeldTotal.IsNegative() ||
		!c.HeldBaseCost.Add(c.HeldManagementFee).Equal(c.HeldTotal) {
		return ErrAdaptiveBillingContextInvalid
	}
	switch c.FundingSource {
	case UsageReservationFundingBalance:
		if c.SubscriptionID != nil {
			return ErrAdaptiveBillingContextInvalid
		}
	case UsageReservationFundingSubscription:
		if c.SubscriptionID == nil || *c.SubscriptionID <= 0 {
			return ErrAdaptiveBillingContextInvalid
		}
	default:
		return ErrAdaptiveBillingContextInvalid
	}
	return nil
}
