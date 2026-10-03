package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

const (
	UsageReservationFundingBalance      = "balance"
	UsageReservationFundingSubscription = "subscription"

	UsageReservationStatusAuthorized  = "authorized"
	UsageReservationStatusHeld        = UsageReservationStatusAuthorized
	UsageReservationStatusInFlight    = "in_flight"
	UsageReservationStatusReconciling = "reconciling"
	UsageReservationStatusCaptured    = "captured"
	UsageReservationStatusReleased    = "released"

	UsageReservationOperationReserve   = "reserve"
	UsageReservationOperationCapture   = "capture"
	UsageReservationOperationRelease   = "release"
	UsageReservationOperationRenew     = "renew"
	UsageReservationOperationReconcile = "reconcile"

	UsageReservationMoneyScale        int32 = AdaptiveBillingMoneyScale
	UsageReservationDefaultLease            = 120 * time.Second
	UsageReservationRenewInterval           = 30 * time.Second
	UsageReservationReconcileClaimTTL       = 120 * time.Second
)

var usageReservationMaxMoney = decimal.RequireFromString("9999999999.9999999999")

var (
	ErrUsageReservationInvalid                 = errors.New("usage reservation command is invalid")
	ErrUsageReservationIDRequired              = errors.New("usage reservation id is required")
	ErrUsageReservationIdempotencyKeyRequired  = errors.New("usage reservation idempotency key is required")
	ErrUsageReservationOperationKeyRequired    = errors.New("usage reservation operation key is required")
	ErrUsageReservationFingerprintConflict     = errors.New("usage reservation fingerprint conflict")
	ErrUsageReservationNotFound                = errors.New("usage reservation not found")
	ErrUsageReservationNotHeld                 = errors.New("usage reservation is not held")
	ErrUsageReservationFenceConflict           = errors.New("usage reservation fencing token conflict")
	ErrUsageReservationVersionConflict         = errors.New("usage reservation row version conflict")
	ErrUsageReservationOwnerConflict           = errors.New("usage reservation owner conflict")
	ErrUsageReservationLeaseExpired            = errors.New("usage reservation lease is expired")
	ErrUsageReservationInsufficientBalance     = errors.New("insufficient balance for usage reservation")
	ErrUsageReservationAPIKeyUnavailable       = errors.New("api key is unavailable for usage reservation")
	ErrUsageReservationAPIKeyQuota             = errors.New("api key quota is insufficient for usage reservation")
	ErrUsageReservationAPIKeyRateLimit         = errors.New("api key spend limit is insufficient for usage reservation")
	ErrUsageReservationSubscriptionUnavailable = errors.New("subscription is unavailable for usage reservation")
	ErrUsageReservationSubscriptionLimit       = errors.New("subscription limit is insufficient for usage reservation")
	ErrUsageReservationCaptureExceedsHold      = errors.New("usage reservation capture exceeds hold")
	ErrUsageReservationAttemptConflict         = errors.New("usage reservation attempt conflict")
	ErrUsageReservationAttemptLimit            = errors.New("usage reservation attempt limit exceeded")
	ErrUsageReservationEvidenceRequired        = errors.New("usage reservation evidence is required")
	ErrUsageReservationEvidenceConflict        = errors.New("usage reservation evidence conflict")
)

// UsageBillingReservation is the durable snapshot of one pre-billed request.
// ManagementFeeBPS is immutable after Reserve so later configuration changes
// cannot alter an in-flight request's settlement.
type UsageBillingReservation struct {
	ID                           string
	IdempotencyKeyHash           string
	RequestFingerprint           string
	LogicalRequestID             string
	UserID                       int64
	APIKeyID                     int64
	ParentGroupID                *int64
	CanonicalModel               string
	PricingSnapshotID            string
	PricingGeneration            int64
	ConfigGeneration             int64
	SubscriptionID               *int64
	FundingSource                string
	Status                       string
	ManagementFeeBPS             int32
	EstimatedBaseCost            decimal.Decimal
	HeldBaseCost                 decimal.Decimal
	HeldManagementFee            decimal.Decimal
	HeldTotal                    decimal.Decimal
	UncappedBaseCost             decimal.Decimal
	CapturedBaseCost             decimal.Decimal
	CapturedManagementFee        decimal.Decimal
	CapturedTotal                decimal.Decimal
	PlatformOverageBaseCost      decimal.Decimal
	WinningLeafGroupID           *int64
	WinningAttemptNo             *int
	UsageLogID                   *int64
	UsageLogCreatedAt            *time.Time
	UsageEvidenceHash            string
	ActiveLeafGroupID            *int64
	ActiveAttemptNo              *int
	AttemptStartedAt             *time.Time
	ReconcileFromStatus          string
	OwnerID                      string
	FencingToken                 int64
	RowVersion                   int64
	LeaseExpiresAt               time.Time
	ReconciliationLeaseExpiresAt *time.Time
	CapturedAt                   *time.Time
	ReleasedAt                   *time.Time
	ReleaseReason                string
	CreatedAt                    time.Time
	UpdatedAt                    time.Time
}

type UsageReservationReserveCommand struct {
	ReservationID       string
	IdempotencyKey      string
	RequestFingerprint  string
	LogicalRequestID    string
	OwnerID             string
	UserID              int64
	APIKeyID            int64
	ParentGroupID       *int64
	CanonicalModel      string
	PricingSnapshotID   string
	PricingGeneration   int64
	ConfigGeneration    int64
	SubscriptionID      *int64
	FundingSource       string
	EstimatedBaseCost   decimal.Decimal
	ManagementFeeBPS    int32
	ManagementFeeBPSSet bool
	LeaseExpiresAt      time.Time
	LeaseTTL            time.Duration
}

func (c *UsageReservationReserveCommand) Normalize() {
	if c == nil {
		return
	}
	c.ReservationID = strings.TrimSpace(c.ReservationID)
	c.IdempotencyKey = strings.TrimSpace(c.IdempotencyKey)
	c.LogicalRequestID = strings.TrimSpace(c.LogicalRequestID)
	if c.LogicalRequestID == "" {
		c.LogicalRequestID = c.IdempotencyKey
	}
	c.OwnerID = strings.TrimSpace(c.OwnerID)
	c.CanonicalModel = strings.TrimSpace(c.CanonicalModel)
	c.PricingSnapshotID = strings.TrimSpace(c.PricingSnapshotID)
	c.FundingSource = strings.ToLower(strings.TrimSpace(c.FundingSource))
	// Explicit 0% must stay 0; only unset BPS falls back to the 15% default.
	if c.ManagementFeeBPS == 0 && !c.ManagementFeeBPSSet {
		c.ManagementFeeBPS = DefaultAdaptiveManagementFeeBPS
	}
	c.ManagementFeeBPSSet = true
	c.EstimatedBaseCost = normalizeUsageReservationHoldAmount(c.EstimatedBaseCost)
	if !c.LeaseExpiresAt.IsZero() {
		c.LeaseExpiresAt = c.LeaseExpiresAt.UTC()
	}
	if c.LeaseTTL <= 0 {
		c.LeaseTTL = UsageReservationDefaultLease
	}
	if strings.TrimSpace(c.RequestFingerprint) == "" {
		c.RequestFingerprint = hashUsageReservationParts(
			fmt.Sprintf("%d", c.UserID),
			fmt.Sprintf("%d", c.APIKeyID),
			c.LogicalRequestID,
			optionalInt64String(c.SubscriptionID),
			optionalInt64String(c.ParentGroupID),
			c.CanonicalModel,
			c.PricingSnapshotID,
			fmt.Sprintf("%d", c.PricingGeneration),
			fmt.Sprintf("%d", c.ConfigGeneration),
			c.FundingSource,
			c.EstimatedBaseCost.StringFixed(UsageReservationMoneyScale),
			fmt.Sprintf("%d", c.ManagementFeeBPS),
		)
	} else {
		c.RequestFingerprint = strings.TrimSpace(c.RequestFingerprint)
	}
}

func (c *UsageReservationReserveCommand) Validate() error {
	if c == nil || c.UserID <= 0 || c.APIKeyID <= 0 || c.ParentGroupID == nil || *c.ParentGroupID <= 0 ||
		c.OwnerID == "" || len(c.OwnerID) > 128 {
		return ErrUsageReservationInvalid
	}
	if c.IdempotencyKey == "" {
		return ErrUsageReservationIdempotencyKeyRequired
	}
	if c.FundingSource != UsageReservationFundingBalance && c.FundingSource != UsageReservationFundingSubscription {
		return ErrUsageReservationInvalid
	}
	if c.FundingSource == UsageReservationFundingSubscription && (c.SubscriptionID == nil || *c.SubscriptionID <= 0) {
		return ErrUsageReservationInvalid
	}
	if c.FundingSource == UsageReservationFundingBalance && c.SubscriptionID != nil {
		return ErrUsageReservationInvalid
	}
	hold, holdErr := CalculateAdaptiveManagementFeeHoldDecimalWithBPS(c.EstimatedBaseCost, c.ManagementFeeBPS)
	if !isAdaptiveManagementFeeBPS(c.ManagementFeeBPS) || !isUsageReservationMoney(c.EstimatedBaseCost) || holdErr != nil || !isUsageReservationMoney(hold) || c.LeaseTTL < 30*time.Second || c.LeaseTTL > 10*time.Minute ||
		!isUsageReservationSHA256(c.RequestFingerprint) || c.LogicalRequestID == "" || len(c.LogicalRequestID) > 128 ||
		c.CanonicalModel == "" || len(c.CanonicalModel) > 128 || c.PricingSnapshotID == "" || len(c.PricingSnapshotID) > 128 {
		return ErrUsageReservationInvalid
	}
	return nil
}

type UsageReservationCaptureCommand struct {
	ReservationID      string
	OperationKey       string
	RequestFingerprint string
	OwnerID            string
	FencingToken       int64
	RowVersion         int64
	ActualBaseCost     decimal.Decimal
	WinningLeafGroupID int64
	AttemptNo          int
	UsageLogID         int64
	UsageLogCreatedAt  time.Time
	EvidenceHash       string
}

func (c *UsageReservationCaptureCommand) Normalize() {
	if c == nil {
		return
	}
	c.ReservationID = strings.TrimSpace(c.ReservationID)
	c.OperationKey = strings.TrimSpace(c.OperationKey)
	c.OwnerID = strings.TrimSpace(c.OwnerID)
	c.EvidenceHash = strings.TrimSpace(c.EvidenceHash)
	if !c.UsageLogCreatedAt.IsZero() {
		c.UsageLogCreatedAt = c.UsageLogCreatedAt.UTC()
	}
	c.ActualBaseCost = normalizeUsageReservationAmount(c.ActualBaseCost)
	if strings.TrimSpace(c.RequestFingerprint) == "" {
		c.RequestFingerprint = hashUsageReservationParts(
			c.ReservationID,
			c.OwnerID,
			fmt.Sprintf("%d", c.FencingToken),
			fmt.Sprintf("%d", c.RowVersion),
			c.ActualBaseCost.StringFixed(UsageReservationMoneyScale),
			fmt.Sprintf("%d", c.WinningLeafGroupID),
			fmt.Sprintf("%d", c.AttemptNo),
			fmt.Sprintf("%d", c.UsageLogID),
			c.UsageLogCreatedAt.Format(time.RFC3339Nano),
			c.EvidenceHash,
		)
	} else {
		c.RequestFingerprint = strings.TrimSpace(c.RequestFingerprint)
	}
}

func (c *UsageReservationCaptureCommand) Validate() error {
	if c == nil || c.ReservationID == "" {
		return ErrUsageReservationIDRequired
	}
	if c.OperationKey == "" {
		return ErrUsageReservationOperationKeyRequired
	}
	if c.OwnerID == "" || len(c.OwnerID) > 128 || c.FencingToken <= 0 || c.RowVersion <= 0 || !isUsageReservationMoney(c.ActualBaseCost) || c.WinningLeafGroupID <= 0 || c.AttemptNo < 1 || c.AttemptNo > AdaptiveMaxLeafAttempts || c.UsageLogID <= 0 || c.UsageLogCreatedAt.IsZero() || !isUsageReservationSHA256(c.EvidenceHash) || !isUsageReservationSHA256(c.RequestFingerprint) {
		return ErrUsageReservationInvalid
	}
	return nil
}

type UsageReservationMarkInFlightCommand struct {
	ReservationID      string
	OperationKey       string
	RequestFingerprint string
	OwnerID            string
	FencingToken       int64
	RowVersion         int64
	AttemptNo          int
	LeafGroupID        int64
	EvidenceHash       string
}

func (c *UsageReservationMarkInFlightCommand) Normalize() {
	if c == nil {
		return
	}
	c.ReservationID = strings.TrimSpace(c.ReservationID)
	c.OperationKey = strings.TrimSpace(c.OperationKey)
	c.OwnerID = strings.TrimSpace(c.OwnerID)
	c.EvidenceHash = strings.TrimSpace(c.EvidenceHash)
	if strings.TrimSpace(c.RequestFingerprint) == "" {
		c.RequestFingerprint = hashUsageReservationParts(c.ReservationID, c.OwnerID,
			fmt.Sprintf("%d", c.FencingToken), fmt.Sprintf("%d", c.RowVersion),
			fmt.Sprintf("%d", c.AttemptNo), fmt.Sprintf("%d", c.LeafGroupID), c.EvidenceHash)
	} else {
		c.RequestFingerprint = strings.TrimSpace(c.RequestFingerprint)
	}
}

func (c *UsageReservationMarkInFlightCommand) Validate() error {
	if c == nil || c.ReservationID == "" {
		return ErrUsageReservationIDRequired
	}
	if c.OperationKey == "" {
		return ErrUsageReservationOperationKeyRequired
	}
	if c.OwnerID == "" || len(c.OwnerID) > 128 || c.FencingToken <= 0 || c.RowVersion <= 0 || c.AttemptNo < 1 || c.AttemptNo > AdaptiveMaxLeafAttempts || c.LeafGroupID <= 0 || !isUsageReservationSHA256(c.EvidenceHash) || !isUsageReservationSHA256(c.RequestFingerprint) {
		return ErrUsageReservationInvalid
	}
	return nil
}

type UsageReservationAttemptFailedCommand struct {
	ReservationID      string
	OperationKey       string
	RequestFingerprint string
	OwnerID            string
	FencingToken       int64
	RowVersion         int64
	AttemptNo          int
	EvidenceHash       string
	FailureClass       string
}

func (c *UsageReservationAttemptFailedCommand) Normalize() {
	if c == nil {
		return
	}
	c.ReservationID = strings.TrimSpace(c.ReservationID)
	c.OperationKey = strings.TrimSpace(c.OperationKey)
	c.OwnerID = strings.TrimSpace(c.OwnerID)
	c.EvidenceHash = strings.TrimSpace(c.EvidenceHash)
	c.FailureClass = strings.TrimSpace(c.FailureClass)
	if strings.TrimSpace(c.RequestFingerprint) == "" {
		c.RequestFingerprint = hashUsageReservationParts(c.ReservationID, c.OwnerID,
			fmt.Sprintf("%d", c.FencingToken), fmt.Sprintf("%d", c.RowVersion),
			fmt.Sprintf("%d", c.AttemptNo), c.EvidenceHash, c.FailureClass)
	} else {
		c.RequestFingerprint = strings.TrimSpace(c.RequestFingerprint)
	}
}

func (c *UsageReservationAttemptFailedCommand) Validate() error {
	if c == nil || c.ReservationID == "" {
		return ErrUsageReservationIDRequired
	}
	if c.OperationKey == "" {
		return ErrUsageReservationOperationKeyRequired
	}
	if c.OwnerID == "" || len(c.OwnerID) > 128 || c.FencingToken <= 0 || c.RowVersion <= 0 || c.AttemptNo < 1 || c.AttemptNo > AdaptiveMaxLeafAttempts || !isUsageReservationSHA256(c.EvidenceHash) || !isUsageReservationSHA256(c.RequestFingerprint) || !isUsageReservationPrecommitFailure(c.FailureClass) {
		return ErrUsageReservationInvalid
	}
	return nil
}

type UsageReservationReleaseCommand struct {
	ReservationID      string
	OperationKey       string
	RequestFingerprint string
	OwnerID            string
	FencingToken       int64
	RowVersion         int64
	Reason             string
	EvidenceHash       string
}

func (c *UsageReservationReleaseCommand) Normalize() {
	if c == nil {
		return
	}
	c.ReservationID = strings.TrimSpace(c.ReservationID)
	c.OperationKey = strings.TrimSpace(c.OperationKey)
	c.OwnerID = strings.TrimSpace(c.OwnerID)
	c.Reason = strings.TrimSpace(c.Reason)
	c.EvidenceHash = strings.TrimSpace(c.EvidenceHash)
	if strings.TrimSpace(c.RequestFingerprint) == "" {
		c.RequestFingerprint = hashUsageReservationParts(
			c.ReservationID,
			c.OwnerID,
			fmt.Sprintf("%d", c.FencingToken),
			fmt.Sprintf("%d", c.RowVersion),
			c.Reason,
			c.EvidenceHash,
		)
	} else {
		c.RequestFingerprint = strings.TrimSpace(c.RequestFingerprint)
	}
}

func (c *UsageReservationReleaseCommand) Validate() error {
	if c == nil || c.ReservationID == "" {
		return ErrUsageReservationIDRequired
	}
	if c.OperationKey == "" {
		return ErrUsageReservationOperationKeyRequired
	}
	if c.OwnerID == "" || len(c.OwnerID) > 128 || c.FencingToken <= 0 || c.RowVersion <= 0 || !isUsageReservationSHA256(c.RequestFingerprint) || (c.EvidenceHash != "" && !isUsageReservationSHA256(c.EvidenceHash)) || len(c.Reason) > 128 {
		return ErrUsageReservationInvalid
	}
	return nil
}

type UsageReservationRenewCommand struct {
	ReservationID      string
	OperationKey       string
	RequestFingerprint string
	OwnerID            string
	FencingToken       int64
	RowVersion         int64
	AdditionalBaseCost decimal.Decimal
	LeaseExpiresAt     time.Time
	LeaseTTL           time.Duration
}

func (c *UsageReservationRenewCommand) Normalize() {
	if c == nil {
		return
	}
	c.ReservationID = strings.TrimSpace(c.ReservationID)
	c.OperationKey = strings.TrimSpace(c.OperationKey)
	c.OwnerID = strings.TrimSpace(c.OwnerID)
	c.AdditionalBaseCost = normalizeUsageReservationHoldAmount(c.AdditionalBaseCost)
	if !c.LeaseExpiresAt.IsZero() {
		c.LeaseExpiresAt = c.LeaseExpiresAt.UTC()
	}
	if c.LeaseTTL <= 0 {
		c.LeaseTTL = UsageReservationDefaultLease
	}
	if strings.TrimSpace(c.RequestFingerprint) == "" {
		c.RequestFingerprint = hashUsageReservationParts(
			c.ReservationID,
			c.OwnerID,
			fmt.Sprintf("%d", c.FencingToken),
			fmt.Sprintf("%d", c.RowVersion),
			c.AdditionalBaseCost.StringFixed(UsageReservationMoneyScale),
			fmt.Sprintf("%d", c.LeaseTTL/time.Second),
		)
	} else {
		c.RequestFingerprint = strings.TrimSpace(c.RequestFingerprint)
	}
}

func (c *UsageReservationRenewCommand) Validate() error {
	if c == nil || c.ReservationID == "" {
		return ErrUsageReservationIDRequired
	}
	if c.OperationKey == "" {
		return ErrUsageReservationOperationKeyRequired
	}
	if c.OwnerID == "" || len(c.OwnerID) > 128 || c.FencingToken <= 0 || c.RowVersion <= 0 || !isUsageReservationMoney(c.AdditionalBaseCost) || c.LeaseTTL < 30*time.Second || c.LeaseTTL > 10*time.Minute || !isUsageReservationSHA256(c.RequestFingerprint) {
		return ErrUsageReservationInvalid
	}
	return nil
}

type UsageReservationResult struct {
	Applied                       bool
	Reservation                   *UsageBillingReservation
	AvailableBalanceAfter         *decimal.Decimal
	AdaptiveReservedBalanceAfter  *decimal.Decimal
	SubscriptionReservedAfter     *decimal.Decimal
	SubscriptionDailyUsageAfter   *decimal.Decimal
	SubscriptionWeeklyUsageAfter  *decimal.Decimal
	SubscriptionMonthlyUsageAfter *decimal.Decimal
	APIKeyReservedQuotaAfter      *decimal.Decimal
	APIKeyQuotaUsedAfter          *decimal.Decimal
	APIKeyReserved5hAfter         *decimal.Decimal
	APIKeyReserved1dAfter         *decimal.Decimal
	APIKeyReserved7dAfter         *decimal.Decimal
}

type UsageReservationReconcileCommand struct {
	WorkerID string
	Limit    int
	ClaimTTL time.Duration
}

func (c *UsageReservationReconcileCommand) Normalize() {
	if c == nil {
		return
	}
	c.WorkerID = strings.TrimSpace(c.WorkerID)
	if c.Limit <= 0 {
		c.Limit = 100
	}
	if c.Limit > 1000 {
		c.Limit = 1000
	}
	if c.ClaimTTL <= 0 {
		c.ClaimTTL = UsageReservationReconcileClaimTTL
	}
}

func (c *UsageReservationReconcileCommand) Validate() error {
	if c == nil || c.WorkerID == "" || c.Limit <= 0 || c.ClaimTTL <= 0 {
		return ErrUsageReservationInvalid
	}
	return nil
}

type UsageReservationReconcileResult struct {
	Examined int
	Claimed  []UsageBillingReservation
}

type UsageBillingReservationRepository interface {
	Reserve(ctx context.Context, cmd *UsageReservationReserveCommand) (*UsageReservationResult, error)
	MarkInFlight(ctx context.Context, cmd *UsageReservationMarkInFlightCommand) (*UsageReservationResult, error)
	MarkAttemptFailed(ctx context.Context, cmd *UsageReservationAttemptFailedCommand) (*UsageReservationResult, error)
	Capture(ctx context.Context, cmd *UsageReservationCaptureCommand) (*UsageReservationResult, error)
	Release(ctx context.Context, cmd *UsageReservationReleaseCommand) (*UsageReservationResult, error)
	Renew(ctx context.Context, cmd *UsageReservationRenewCommand) (*UsageReservationResult, error)
	ReconcileExpired(ctx context.Context, cmd *UsageReservationReconcileCommand) (*UsageReservationReconcileResult, error)
}

func CalculateUsageManagementFee(base decimal.Decimal, bps int32) decimal.Decimal {
	result, err := CalculateAdaptiveManagementFeeDecimalWithBPS(base, bps)
	if err != nil {
		return decimal.Zero
	}
	return result.FeeAmount
}

// CalculateUsageReservationHold applies the same conservative upper-bound math
// used by Adaptive request admission.
func CalculateUsageReservationHold(maxCandidateUpperBound decimal.Decimal, bps int32) decimal.Decimal {
	hold, err := CalculateAdaptiveManagementFeeHoldDecimalWithBPS(maxCandidateUpperBound, bps)
	if err != nil {
		return decimal.Zero
	}
	return hold
}

// ValidateUsageReservationMoney enforces the NUMERIC(20,10) per-request
// boundary used by reservations, ledgers, and usage evidence.
func ValidateUsageReservationMoney(value decimal.Decimal) error {
	if !isUsageReservationMoney(value) {
		return ErrUsageReservationInvalid
	}
	return nil
}

func HashUsageReservationKey(value string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(value)))
	return hex.EncodeToString(sum[:])
}

func normalizeUsageReservationAmount(value decimal.Decimal) decimal.Decimal {
	return value.Round(UsageReservationMoneyScale)
}

func normalizeUsageReservationHoldAmount(value decimal.Decimal) decimal.Decimal {
	return value.RoundCeil(UsageReservationMoneyScale)
}

func hashUsageReservationParts(parts ...string) string {
	return HashUsageReservationKey(strings.Join(parts, "|"))
}

func optionalInt64String(value *int64) string {
	if value == nil {
		return "0"
	}
	return fmt.Sprintf("%d", *value)
}

func isUsageReservationSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func isUsageReservationMoney(value decimal.Decimal) bool {
	return !value.IsNegative() && !value.GreaterThan(usageReservationMaxMoney)
}

func isUsageReservationPrecommitFailure(value string) bool {
	switch value {
	case "precommit_transport", "precommit_upstream", "precommit_policy", "precommit_cancelled":
		return true
	default:
		return false
	}
}
