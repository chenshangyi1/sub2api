package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

var ErrAdaptiveBillingAuthorizationReplay = errors.New("adaptive billing authorization already exists")

type AdaptiveUsageLogRepository interface {
	Create(ctx context.Context, log *UsageLog) (inserted bool, err error)
	GetByID(ctx context.Context, id int64) (*UsageLog, error)
}

// AdaptiveBillingCoordinator owns the request-level reservation state machine.
// The router must persist MarkInFlight before starting each upstream attempt.
type AdaptiveBillingCoordinator struct {
	reservations UsageBillingReservationRepository
	usageLogs    AdaptiveUsageLogRepository
}

func NewAdaptiveBillingCoordinator(reservations UsageBillingReservationRepository, usageLogs AdaptiveUsageLogRepository) *AdaptiveBillingCoordinator {
	return &AdaptiveBillingCoordinator{reservations: reservations, usageLogs: usageLogs}
}

func (c *AdaptiveBillingCoordinator) Authorize(ctx context.Context, cmd *UsageReservationReserveCommand) (*AdaptiveBillingContext, *UsageReservationResult, error) {
	if c == nil || c.reservations == nil || cmd == nil {
		return nil, nil, ErrAdaptiveBillingContextInvalid
	}
	result, err := c.reservations.Reserve(ctx, cmd)
	if err != nil {
		return nil, nil, err
	}
	billing := adaptiveBillingContextFromReservation(result)
	if billing == nil {
		return nil, result, ErrAdaptiveBillingContextInvalid
	}
	if !result.Applied {
		return billing, result, ErrAdaptiveBillingAuthorizationReplay
	}
	return billing, result, nil
}

func (c *AdaptiveBillingCoordinator) MarkInFlight(ctx context.Context, billing *AdaptiveBillingContext, leafGroupID int64, attemptNo int) (*UsageReservationResult, error) {
	if err := validateAdaptiveReservationContext(billing); err != nil || leafGroupID <= 0 || attemptNo < 1 || attemptNo > AdaptiveMaxLeafAttempts {
		return nil, ErrAdaptiveBillingContextInvalid
	}
	evidenceHash := hashUsageReservationParts(
		"attempt-start-v1", billing.ReservationID, billing.LogicalRequestID,
		strconv.FormatInt(leafGroupID, 10), strconv.Itoa(attemptNo), billing.PricingSnapshotID,
	)
	result, err := c.reservations.MarkInFlight(ctx, &UsageReservationMarkInFlightCommand{
		ReservationID: billing.ReservationID,
		OperationKey:  fmt.Sprintf("adaptive:start:%s:%d", billing.ReservationID, attemptNo),
		OwnerID:       billing.OwnerID,
		FencingToken:  billing.FencingToken,
		RowVersion:    billing.RowVersion,
		AttemptNo:     attemptNo,
		LeafGroupID:   leafGroupID,
		EvidenceHash:  evidenceHash,
	})
	if err != nil {
		return nil, err
	}
	updateAdaptiveBillingContext(billing, result)
	billing.LeafGroupID = leafGroupID
	billing.AttemptNo = attemptNo
	return result, nil
}

func (c *AdaptiveBillingCoordinator) MarkAttemptFailed(ctx context.Context, billing *AdaptiveBillingContext, failureClass string) (*UsageReservationResult, error) {
	if err := billing.ValidateForSettlement(); err != nil || strings.TrimSpace(failureClass) == "" {
		return nil, ErrAdaptiveBillingContextInvalid
	}
	evidenceHash := hashUsageReservationParts(
		"attempt-failed-v1", billing.ReservationID, strconv.Itoa(billing.AttemptNo),
		strconv.FormatInt(billing.LeafGroupID, 10), strings.TrimSpace(failureClass),
	)
	result, err := c.reservations.MarkAttemptFailed(ctx, &UsageReservationAttemptFailedCommand{
		ReservationID: billing.ReservationID,
		OperationKey:  fmt.Sprintf("adaptive:failed:%s:%d", billing.ReservationID, billing.AttemptNo),
		OwnerID:       billing.OwnerID,
		FencingToken:  billing.FencingToken,
		RowVersion:    billing.RowVersion,
		AttemptNo:     billing.AttemptNo,
		EvidenceHash:  evidenceHash,
		FailureClass:  strings.TrimSpace(failureClass),
	})
	if err != nil {
		return nil, err
	}
	updateAdaptiveBillingContext(billing, result)
	billing.LastFailedEvidenceHash = evidenceHash
	billing.LeafGroupID = 0
	billing.AttemptNo = 0
	return result, nil
}

func (c *AdaptiveBillingCoordinator) Capture(ctx context.Context, billing *AdaptiveBillingContext, usageLog *UsageLog, actualBaseCost decimal.Decimal) (*AdaptiveManagementFeeResult, *UsageReservationResult, error) {
	if c == nil || c.reservations == nil || c.usageLogs == nil || usageLog == nil {
		return nil, nil, ErrAdaptiveBillingContextInvalid
	}
	if err := billing.ValidateForSettlement(); err != nil {
		return nil, nil, err
	}
	settlement, err := CalculateAdaptiveManagementFeeSettlementDecimalWithBPS(
		actualBaseCost,
		billing.HeldBaseCost,
		billing.HeldManagementFee,
		billing.HeldTotal,
		billing.ManagementFeeBPS,
	)
	if err != nil {
		return nil, nil, err
	}
	evidenceHash := adaptiveUsageEvidenceHash(billing, usageLog, settlement)
	populatePendingAdaptiveUsageLog(usageLog, billing, settlement, evidenceHash)
	if err := usageLog.ValidatePendingAdaptiveUsageEvidence(); err != nil {
		return &settlement.CustomerCharge, nil, err
	}
	inserted, err := c.usageLogs.Create(ctx, usageLog)
	if err != nil {
		return nil, nil, err
	}
	if usageLog.ID <= 0 || usageLog.CreatedAt.IsZero() {
		return nil, nil, ErrUsageReservationEvidenceRequired
	}
	if !inserted {
		persisted, getErr := c.usageLogs.GetByID(ctx, usageLog.ID)
		if getErr != nil {
			return &settlement.CustomerCharge, nil, errors.Join(ErrAdaptiveUsageEvidenceConflict, getErr)
		}
		if bindingErr := ValidateAdaptiveUsageEvidenceBinding(usageLog, persisted); bindingErr != nil {
			return &settlement.CustomerCharge, nil, bindingErr
		}
		applyPersistedAdaptiveUsageLogState(usageLog, persisted)
	}

	captureCommand := &UsageReservationCaptureCommand{
		ReservationID:      billing.ReservationID,
		OperationKey:       "adaptive:capture:" + billing.ReservationID,
		OwnerID:            billing.OwnerID,
		FencingToken:       billing.FencingToken,
		RowVersion:         billing.RowVersion,
		ActualBaseCost:     settlement.UncappedBaseAmount,
		WinningLeafGroupID: billing.LeafGroupID,
		AttemptNo:          billing.AttemptNo,
		UsageLogID:         usageLog.ID,
		UsageLogCreatedAt:  usageLog.CreatedAt,
		EvidenceHash:       evidenceHash,
	}
	result, err := c.reservations.Capture(ctx, captureCommand)
	if err != nil {
		if persisted, getErr := c.usageLogs.GetByID(ctx, usageLog.ID); getErr == nil {
			if bindingErr := ValidateAdaptiveUsageEvidenceBinding(usageLog, persisted); bindingErr != nil {
				return &settlement.CustomerCharge, nil, errors.Join(err, bindingErr)
			}
			applyPersistedAdaptiveUsageLogState(usageLog, persisted)
		}
		return &settlement.CustomerCharge, nil, err
	}
	updateAdaptiveBillingContext(billing, result)
	usageLog.ActualCost = settlement.CustomerCharge.CaptureAmount.InexactFloat64()
	status := AdaptiveSettlementStatusCaptured
	usageLog.AdaptiveSettlementStatus = &status
	return &settlement.CustomerCharge, result, nil
}

func applyPersistedAdaptiveUsageLogState(target, persisted *UsageLog) {
	if target == nil || persisted == nil {
		return
	}
	target.ID = persisted.ID
	target.CreatedAt = persisted.CreatedAt
	target.ActualCost = persisted.ActualCost
	if persisted.AdaptiveSettlementStatus == nil {
		target.AdaptiveSettlementStatus = nil
		return
	}
	status := *persisted.AdaptiveSettlementStatus
	target.AdaptiveSettlementStatus = &status
}

func (c *AdaptiveBillingCoordinator) Release(ctx context.Context, billing *AdaptiveBillingContext, reason, evidenceHash string) (*UsageReservationResult, error) {
	if err := validateAdaptiveReservationContext(billing); err != nil {
		return nil, err
	}
	result, err := c.reservations.Release(ctx, &UsageReservationReleaseCommand{
		ReservationID: billing.ReservationID,
		OperationKey:  "adaptive:release:" + billing.ReservationID,
		OwnerID:       billing.OwnerID,
		FencingToken:  billing.FencingToken,
		RowVersion:    billing.RowVersion,
		Reason:        strings.TrimSpace(reason),
		EvidenceHash:  strings.TrimSpace(evidenceHash),
	})
	if err != nil {
		return nil, err
	}
	updateAdaptiveBillingContext(billing, result)
	return result, nil
}

func (c *AdaptiveBillingCoordinator) Renew(ctx context.Context, billing *AdaptiveBillingContext, additionalBaseCost decimal.Decimal, leaseTTL time.Duration) (*UsageReservationResult, error) {
	if err := validateAdaptiveReservationContext(billing); err != nil {
		return nil, err
	}
	result, err := c.reservations.Renew(ctx, &UsageReservationRenewCommand{
		ReservationID:      billing.ReservationID,
		OperationKey:       fmt.Sprintf("adaptive:renew:%s:%d", billing.ReservationID, billing.RowVersion),
		OwnerID:            billing.OwnerID,
		FencingToken:       billing.FencingToken,
		RowVersion:         billing.RowVersion,
		AdditionalBaseCost: additionalBaseCost,
		LeaseTTL:           leaseTTL,
	})
	if err != nil {
		return nil, err
	}
	updateAdaptiveBillingContext(billing, result)
	return result, nil
}

func adaptiveBillingContextFromReservation(result *UsageReservationResult) *AdaptiveBillingContext {
	if result == nil || result.Reservation == nil {
		return nil
	}
	r := result.Reservation
	billing := &AdaptiveBillingContext{
		ReservationID:       r.ID,
		LogicalRequestID:    r.LogicalRequestID,
		OwnerID:             r.OwnerID,
		FencingToken:        r.FencingToken,
		RowVersion:          r.RowVersion,
		PricingGeneration:   r.PricingGeneration,
		ConfigGeneration:    r.ConfigGeneration,
		PricingSnapshotID:   r.PricingSnapshotID,
		ManagementFeeBPS:    r.ManagementFeeBPS,
		ManagementFeeBPSSet: true,
		UserID:              r.UserID,
		HeldBaseCost:        r.HeldBaseCost,
		HeldManagementFee:   r.HeldManagementFee,
		HeldTotal:           r.HeldTotal,
		FundingSource:       r.FundingSource,
		SubscriptionID:      r.SubscriptionID,
	}
	if r.ParentGroupID != nil {
		billing.ParentGroupID = *r.ParentGroupID
	}
	if r.ActiveLeafGroupID != nil {
		billing.LeafGroupID = *r.ActiveLeafGroupID
	}
	if r.ActiveAttemptNo != nil {
		billing.AttemptNo = *r.ActiveAttemptNo
	}
	return billing
}

func updateAdaptiveBillingContext(billing *AdaptiveBillingContext, result *UsageReservationResult) {
	if billing == nil || result == nil || result.Reservation == nil {
		return
	}
	billing.OwnerID = result.Reservation.OwnerID
	billing.FencingToken = result.Reservation.FencingToken
	billing.RowVersion = result.Reservation.RowVersion
}

func validateAdaptiveReservationContext(billing *AdaptiveBillingContext) error {
	if billing == nil || billing.Probe {
		return ErrAdaptiveBillingContextInvalid
	}
	billing.Normalize()
	if billing.ReservationID == "" || billing.LogicalRequestID == "" || billing.OwnerID == "" || billing.FencingToken <= 0 || billing.RowVersion <= 0 {
		return ErrAdaptiveBillingContextInvalid
	}
	return nil
}

func adaptiveUsageEvidenceHash(billing *AdaptiveBillingContext, log *UsageLog, settlement AdaptiveManagementFeeSettlement) string {
	fee := settlement.CustomerCharge
	return hashUsageReservationParts(
		"adaptive-usage-v1", billing.ReservationID, billing.LogicalRequestID,
		strconv.FormatInt(log.UserID, 10), strconv.FormatInt(log.APIKeyID, 10), strconv.FormatInt(log.AccountID, 10),
		strings.TrimSpace(log.RequestID), strings.TrimSpace(log.Model),
		strconv.Itoa(log.InputTokens), strconv.Itoa(log.OutputTokens), strconv.Itoa(log.CacheCreationTokens), strconv.Itoa(log.CacheReadTokens),
		fee.BaseAmount.StringFixed(AdaptiveBillingMoneyScale), fee.FeeAmount.StringFixed(AdaptiveBillingMoneyScale), fee.CaptureAmount.StringFixed(AdaptiveBillingMoneyScale),
		settlement.UncappedBaseAmount.StringFixed(AdaptiveBillingMoneyScale), settlement.PlatformOverageBaseAmount.StringFixed(AdaptiveBillingMoneyScale),
		strconv.FormatInt(billing.LeafGroupID, 10), strconv.Itoa(billing.AttemptNo), billing.PricingSnapshotID,
	)
}

func populatePendingAdaptiveUsageLog(log *UsageLog, billing *AdaptiveBillingContext, settlement AdaptiveManagementFeeSettlement, evidenceHash string) {
	fee := settlement.CustomerCharge
	base := fee.BaseAmount.InexactFloat64()
	managementFee := fee.FeeAmount.InexactFloat64()
	total := fee.CaptureAmount.InexactFloat64()
	uncappedBase := settlement.UncappedBaseAmount.InexactFloat64()
	platformOverage := settlement.PlatformOverageBaseAmount.InexactFloat64()
	parentGroupID := billing.ParentGroupID
	leafGroupID := billing.LeafGroupID
	attemptNo := billing.AttemptNo
	pricingSnapshotID := billing.PricingSnapshotID
	reservationID := billing.ReservationID
	status := AdaptiveSettlementStatusPending

	log.ActualCost = 0
	log.AdaptiveBaseCost = &base
	log.AdaptiveManagementFeeCost = &managementFee
	log.AdaptiveTotalCost = &total
	log.AdaptiveUncappedBaseCost = &uncappedBase
	log.AdaptivePlatformOverageCost = &platformOverage
	log.AdaptiveParentGroupID = &parentGroupID
	log.RoutedGroupID = &leafGroupID
	log.AdaptiveAttemptNo = &attemptNo
	log.AdaptivePricingSnapshotID = &pricingSnapshotID
	log.AdaptiveReservationID = &reservationID
	log.AdaptiveEvidenceHash = &evidenceHash
	log.AdaptiveSettlementStatus = &status
}
