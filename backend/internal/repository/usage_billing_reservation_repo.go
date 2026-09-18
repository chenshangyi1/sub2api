package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type usageBillingReservationRepository struct {
	db *sql.DB
}

const (
	usageReservationSetLockTimeoutSQL      = "SET LOCAL lock_timeout = '2s'"
	usageReservationSetStatementTimeoutSQL = "SET LOCAL statement_timeout = '10s'"
)

func NewUsageBillingReservationRepository(db *sql.DB) service.UsageBillingReservationRepository {
	return &usageBillingReservationRepository{db: db}
}

func beginUsageReservationTx(ctx context.Context, db *sql.DB) (*sql.Tx, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, usageReservationSetLockTimeoutSQL); err != nil {
		_ = tx.Rollback()
		return nil, fmt.Errorf("set usage reservation lock timeout: %w", err)
	}
	if _, err := tx.ExecContext(ctx, usageReservationSetStatementTimeoutSQL); err != nil {
		_ = tx.Rollback()
		return nil, fmt.Errorf("set usage reservation statement timeout: %w", err)
	}
	return tx, nil
}

const usageReservationSelectColumns = `
	id, idempotency_key_hash, request_fingerprint, logical_request_id, user_id, api_key_id,
	parent_group_id, canonical_model, pricing_snapshot_id, pricing_generation, config_generation,
	subscription_id, funding_source, status, management_fee_bps,
	estimated_base_cost, held_base_cost, held_management_fee, held_total,
	uncapped_base_cost, captured_base_cost, captured_management_fee, captured_total,
	platform_overage_base_cost, winning_leaf_group_id, winning_attempt_no,
	usage_log_id, usage_log_created_at, usage_evidence_hash,
	active_leaf_group_id, active_attempt_no, attempt_started_at, reconcile_from_status,
	owner_id, lease_epoch, row_version, lease_expires_at,
	reconciliation_lease_expires_at, captured_at, released_at, release_reason,
	created_at, updated_at`

type usageReservationRowScanner interface {
	Scan(dest ...any) error
}

type reservationFinancialSnapshot struct {
	availableBalance         *decimal.Decimal
	adaptiveReservedBalance  *decimal.Decimal
	subscriptionReserved     *decimal.Decimal
	subscriptionDailyUsage   *decimal.Decimal
	subscriptionWeeklyUsage  *decimal.Decimal
	subscriptionMonthlyUsage *decimal.Decimal
	apiKeyReservedQuota      *decimal.Decimal
	apiKeyQuotaUsed          *decimal.Decimal
	apiKeyReserved5h         *decimal.Decimal
	apiKeyReserved1d         *decimal.Decimal
	apiKeyReserved7d         *decimal.Decimal
}

func (s reservationFinancialSnapshot) result(reservation *service.UsageBillingReservation, applied bool) *service.UsageReservationResult {
	return &service.UsageReservationResult{
		Applied:                       applied,
		Reservation:                   reservation,
		AvailableBalanceAfter:         s.availableBalance,
		AdaptiveReservedBalanceAfter:  s.adaptiveReservedBalance,
		SubscriptionReservedAfter:     s.subscriptionReserved,
		SubscriptionDailyUsageAfter:   s.subscriptionDailyUsage,
		SubscriptionWeeklyUsageAfter:  s.subscriptionWeeklyUsage,
		SubscriptionMonthlyUsageAfter: s.subscriptionMonthlyUsage,
		APIKeyReservedQuotaAfter:      s.apiKeyReservedQuota,
		APIKeyQuotaUsedAfter:          s.apiKeyQuotaUsed,
		APIKeyReserved5hAfter:         s.apiKeyReserved5h,
		APIKeyReserved1dAfter:         s.apiKeyReserved1d,
		APIKeyReserved7dAfter:         s.apiKeyReserved7d,
	}
}

func (r *usageBillingReservationRepository) Reserve(ctx context.Context, cmd *service.UsageReservationReserveCommand) (_ *service.UsageReservationResult, err error) {
	if r == nil || r.db == nil {
		return nil, errors.New("usage billing reservation repository db is nil")
	}
	if cmd == nil {
		return nil, service.ErrUsageReservationInvalid
	}
	cmd.Normalize()
	if err := cmd.Validate(); err != nil {
		return nil, err
	}

	heldBase := cmd.EstimatedBaseCost.RoundCeil(service.UsageReservationMoneyScale)
	heldTotal := service.CalculateUsageReservationHold(cmd.EstimatedBaseCost, cmd.ManagementFeeBPS)
	heldFee := heldTotal.Sub(heldBase).Round(service.UsageReservationMoneyScale)
	if heldTotal.IsNegative() || heldFee.IsNegative() {
		return nil, service.ErrUsageReservationInvalid
	}

	reservationID := cmd.ReservationID
	if reservationID == "" {
		reservationID = uuid.NewString()
	} else if _, parseErr := uuid.Parse(reservationID); parseErr != nil {
		return nil, service.ErrUsageReservationInvalid
	}
	keyHash := service.HashUsageReservationKey(cmd.IdempotencyKey)

	tx, err := beginUsageReservationTx(ctx, r.db)
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	reservation, inserted, err := insertUsageReservation(ctx, tx, reservationID, keyHash, cmd, heldBase, heldFee, heldTotal)
	if err != nil {
		return nil, err
	}
	if !inserted {
		reservation, err = lockUsageReservationByIdempotencyKey(ctx, tx, cmd.APIKeyID, keyHash)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, service.ErrUsageReservationLeaseExpired
		}
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(reservation.RequestFingerprint) != strings.TrimSpace(cmd.RequestFingerprint) {
			return nil, service.ErrUsageReservationFingerprintConflict
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		tx = nil
		return (reservationFinancialSnapshot{}).result(reservation, false), nil
	}

	snapshot, err := reserveUsageFunding(ctx, tx, reservation, heldTotal)
	if err != nil {
		return nil, err
	}
	apiSnapshot, err := reserveUsageAPIKeyConstraints(ctx, tx, reservation.UserID, reservation.APIKeyID, reservation.ParentGroupID, heldTotal)
	if err != nil {
		return nil, err
	}
	snapshot.merge(apiSnapshot)

	if err := insertUsageReservationLedgerPair(ctx, tx, reservation, service.UsageReservationOperationReserve,
		keyHash, cmd.RequestFingerprint, heldBase, heldFee, heldBase, heldFee, decimal.Zero, decimal.Zero, snapshot); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	tx = nil
	return snapshot.result(reservation, true), nil
}

func (r *usageBillingReservationRepository) MarkInFlight(ctx context.Context, cmd *service.UsageReservationMarkInFlightCommand) (_ *service.UsageReservationResult, err error) {
	if r == nil || r.db == nil || cmd == nil {
		return nil, service.ErrUsageReservationInvalid
	}
	cmd.Normalize()
	if err := cmd.Validate(); err != nil {
		return nil, err
	}
	tx, err := beginUsageReservationTx(ctx, r.db)
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()
	reservation, err := lockUsageReservation(ctx, tx, cmd.ReservationID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrUsageReservationNotFound
	}
	if err != nil {
		return nil, err
	}

	replayed, err := replayStartedAttempt(ctx, tx, reservation, cmd)
	if err != nil {
		return nil, err
	}
	if replayed {
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		tx = nil
		return (&reservationFinancialSnapshot{}).result(reservation, false), nil
	}
	if err := validateUsageReservationMutation(reservation, cmd.OwnerID, cmd.FencingToken, cmd.RowVersion,
		service.UsageReservationStatusAuthorized); err != nil {
		return nil, err
	}
	var attemptCount, failedAttemptOne int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*), COUNT(*) FILTER (WHERE attempt_no = 1 AND status = 'failed')
		FROM usage_billing_attempts WHERE reservation_id = $1
	`, reservation.ID).Scan(&attemptCount, &failedAttemptOne); err != nil {
		return nil, err
	}
	if (cmd.AttemptNo == 1 && attemptCount != 0) ||
		(cmd.AttemptNo == 2 && (attemptCount != 1 || failedAttemptOne != 1)) {
		return nil, service.ErrUsageReservationAttemptConflict
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO usage_billing_attempts (
			reservation_id, attempt_no, leaf_group_id, status,
			start_operation_key_hash, start_fingerprint, start_evidence_hash
		) VALUES ($1, $2, $3, 'started', $4, $5, $6)
	`, reservation.ID, cmd.AttemptNo, cmd.LeafGroupID, service.HashUsageReservationKey(cmd.OperationKey),
		cmd.RequestFingerprint, cmd.EvidenceHash); err != nil {
		return nil, err
	}
	updated, err := scanUsageReservation(tx.QueryRowContext(ctx, `
		UPDATE usage_billing_reservations
		SET status = 'in_flight', active_leaf_group_id = $1, active_attempt_no = $2,
			attempt_started_at = clock_timestamp(), row_version = row_version + 1, updated_at = clock_timestamp()
		WHERE id = $3 AND status = 'authorized' AND owner_id = $4
		  AND lease_epoch = $5 AND row_version = $6 AND lease_expires_at > clock_timestamp()
		RETURNING `+usageReservationSelectColumns,
		cmd.LeafGroupID, cmd.AttemptNo, reservation.ID, cmd.OwnerID, cmd.FencingToken, cmd.RowVersion))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrUsageReservationLeaseExpired
	}
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	tx = nil
	return (&reservationFinancialSnapshot{}).result(updated, true), nil
}

func (r *usageBillingReservationRepository) MarkAttemptFailed(ctx context.Context, cmd *service.UsageReservationAttemptFailedCommand) (_ *service.UsageReservationResult, err error) {
	if r == nil || r.db == nil || cmd == nil {
		return nil, service.ErrUsageReservationInvalid
	}
	cmd.Normalize()
	if err := cmd.Validate(); err != nil {
		return nil, err
	}
	tx, err := beginUsageReservationTx(ctx, r.db)
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()
	reservation, err := lockUsageReservation(ctx, tx, cmd.ReservationID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrUsageReservationNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := lockAndRejectUsageReservationEvidence(ctx, tx, reservation.ID, &cmd.AttemptNo); err != nil {
		return nil, err
	}
	replayed, err := replayFailedAttempt(ctx, tx, reservation, cmd)
	if err != nil {
		return nil, err
	}
	if replayed {
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		tx = nil
		return (&reservationFinancialSnapshot{}).result(reservation, false), nil
	}
	if err := validateUsageReservationMutation(reservation, cmd.OwnerID, cmd.FencingToken, cmd.RowVersion,
		service.UsageReservationStatusInFlight, service.UsageReservationStatusReconciling); err != nil {
		return nil, err
	}
	// Bug #2 fix: Allow marking historical attempts as failed, not just active_attempt_no
	if reservation.ActiveAttemptNo == nil || cmd.AttemptNo > *reservation.ActiveAttemptNo {
		return nil, service.ErrUsageReservationAttemptConflict
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE usage_billing_attempts
		SET status = 'failed', failure_operation_key_hash = $1, failure_fingerprint = $2,
			failure_evidence_hash = $3, failure_class = $4, finished_at = NOW(), updated_at = NOW()
		WHERE reservation_id = $5 AND attempt_no = $6 AND status = 'started'
	`, service.HashUsageReservationKey(cmd.OperationKey), cmd.RequestFingerprint, cmd.EvidenceHash,
		cmd.FailureClass, reservation.ID, cmd.AttemptNo)
	if err != nil {
		return nil, err
	}
	if affected, affectedErr := result.RowsAffected(); affectedErr != nil {
		return nil, affectedErr
	} else if affected != 1 {
		return nil, service.ErrUsageReservationAttemptConflict
	}
	updated, err := scanUsageReservation(tx.QueryRowContext(ctx, `
		UPDATE usage_billing_reservations
		SET status = CASE WHEN status = 'in_flight' THEN 'authorized' ELSE status END,
			reconcile_from_status = CASE WHEN status = 'reconciling' THEN 'authorized' ELSE NULL END,
			active_leaf_group_id = NULL, active_attempt_no = NULL, attempt_started_at = NULL,
			row_version = row_version + 1, updated_at = clock_timestamp()
		WHERE id = $1 AND owner_id = $2 AND lease_epoch = $3 AND row_version = $4
		  AND ((status = 'in_flight' AND lease_expires_at > clock_timestamp())
		    OR (status = 'reconciling' AND reconciliation_lease_expires_at > clock_timestamp()))
		RETURNING `+usageReservationSelectColumns,
		reservation.ID, cmd.OwnerID, cmd.FencingToken, cmd.RowVersion))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrUsageReservationLeaseExpired
	}
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	tx = nil
	return (&reservationFinancialSnapshot{}).result(updated, true), nil
}

func (r *usageBillingReservationRepository) Capture(ctx context.Context, cmd *service.UsageReservationCaptureCommand) (_ *service.UsageReservationResult, err error) {
	if r == nil || r.db == nil {
		return nil, errors.New("usage billing reservation repository db is nil")
	}
	if cmd == nil {
		return nil, service.ErrUsageReservationInvalid
	}
	cmd.Normalize()
	if err := cmd.Validate(); err != nil {
		return nil, err
	}

	tx, err := beginUsageReservationTx(ctx, r.db)
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	reservation, err := lockUsageReservation(ctx, tx, cmd.ReservationID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrUsageReservationNotFound
	}
	if err != nil {
		return nil, err
	}
	if replay, err := replayUsageReservationOperation(ctx, tx, reservation, cmd.OperationKey, cmd.RequestFingerprint,
		service.UsageReservationOperationCapture, cmd.OwnerID, cmd.FencingToken, cmd.RowVersion); replay != nil || err != nil {
		if err == nil {
			if commitErr := tx.Commit(); commitErr != nil {
				return nil, commitErr
			}
			tx = nil
		}
		return replay, err
	}
	if err := validateUsageReservationMutation(reservation, cmd.OwnerID, cmd.FencingToken, cmd.RowVersion,
		service.UsageReservationStatusInFlight, service.UsageReservationStatusReconciling); err != nil {
		return nil, err
	}

	settlement, feeErr := calculateReservationCapture(cmd.ActualBaseCost, reservation)
	if feeErr != nil {
		return nil, feeErr
	}
	if err := captureUsageReservationEvidence(ctx, tx, reservation, cmd, settlement); err != nil {
		return nil, err
	}

	snapshot, err := captureUsageFunding(ctx, tx, reservation, settlement.CustomerCharge.CaptureAmount)
	if err != nil {
		return nil, err
	}
	apiSnapshot, err := captureUsageAPIKeyConstraints(ctx, tx, reservation.APIKeyID, reservation.HeldTotal, settlement.CustomerCharge.CaptureAmount)
	if err != nil {
		return nil, err
	}
	snapshot.merge(apiSnapshot)

	updated, err := markUsageReservationCaptured(ctx, tx, reservation, cmd, settlement)
	if err != nil {
		return nil, err
	}
	if err := insertUsageReservationLedgerPair(ctx, tx, updated, service.UsageReservationOperationCapture,
		service.HashUsageReservationKey(cmd.OperationKey), cmd.RequestFingerprint,
		settlement.CustomerCharge.BaseAmount, settlement.CustomerCharge.FeeAmount,
		reservation.HeldBaseCost.Neg(), reservation.HeldManagementFee.Neg(),
		settlement.CustomerCharge.BaseAmount, settlement.CustomerCharge.FeeAmount, snapshot); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	tx = nil
	return snapshot.result(updated, true), nil
}

func (r *usageBillingReservationRepository) Release(ctx context.Context, cmd *service.UsageReservationReleaseCommand) (_ *service.UsageReservationResult, err error) {
	if r == nil || r.db == nil {
		return nil, errors.New("usage billing reservation repository db is nil")
	}
	if cmd == nil {
		return nil, service.ErrUsageReservationInvalid
	}
	cmd.Normalize()
	if err := cmd.Validate(); err != nil {
		return nil, err
	}

	tx, err := beginUsageReservationTx(ctx, r.db)
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	reservation, err := lockUsageReservation(ctx, tx, cmd.ReservationID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrUsageReservationNotFound
	}
	if err != nil {
		return nil, err
	}
	if replay, err := replayUsageReservationOperation(ctx, tx, reservation, cmd.OperationKey, cmd.RequestFingerprint,
		service.UsageReservationOperationRelease, cmd.OwnerID, cmd.FencingToken, cmd.RowVersion); replay != nil || err != nil {
		if err == nil {
			if commitErr := tx.Commit(); commitErr != nil {
				return nil, commitErr
			}
			tx = nil
		}
		return replay, err
	}
	if err := validateUsageReservationMutation(reservation, cmd.OwnerID, cmd.FencingToken, cmd.RowVersion,
		service.UsageReservationStatusAuthorized, service.UsageReservationStatusReconciling); err != nil {
		return nil, err
	}
	if err := validateUsageReservationReleaseEvidence(ctx, tx, reservation, cmd.EvidenceHash); err != nil {
		return nil, err
	}

	snapshot, err := releaseUsageFunding(ctx, tx, reservation)
	if err != nil {
		return nil, err
	}
	apiSnapshot, err := releaseUsageAPIKeyConstraints(ctx, tx, reservation.APIKeyID, reservation.HeldTotal)
	if err != nil {
		return nil, err
	}
	snapshot.merge(apiSnapshot)

	updated, err := markUsageReservationReleased(ctx, tx, reservation, cmd)
	if err != nil {
		return nil, err
	}
	if err := insertUsageReservationLedgerPair(ctx, tx, updated, service.UsageReservationOperationRelease,
		service.HashUsageReservationKey(cmd.OperationKey), cmd.RequestFingerprint,
		reservation.HeldBaseCost, reservation.HeldManagementFee,
		reservation.HeldBaseCost.Neg(), reservation.HeldManagementFee.Neg(),
		decimal.Zero, decimal.Zero, snapshot); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	tx = nil
	return snapshot.result(updated, true), nil
}

func (r *usageBillingReservationRepository) Renew(ctx context.Context, cmd *service.UsageReservationRenewCommand) (_ *service.UsageReservationResult, err error) {
	if r == nil || r.db == nil {
		return nil, errors.New("usage billing reservation repository db is nil")
	}
	if cmd == nil {
		return nil, service.ErrUsageReservationInvalid
	}
	cmd.Normalize()
	if err := cmd.Validate(); err != nil {
		return nil, err
	}

	tx, err := beginUsageReservationTx(ctx, r.db)
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	reservation, err := lockUsageReservation(ctx, tx, cmd.ReservationID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrUsageReservationNotFound
	}
	if err != nil {
		return nil, err
	}
	if replay, err := replayUsageReservationOperation(ctx, tx, reservation, cmd.OperationKey, cmd.RequestFingerprint,
		service.UsageReservationOperationRenew, cmd.OwnerID, cmd.FencingToken, cmd.RowVersion); replay != nil || err != nil {
		if err == nil {
			if commitErr := tx.Commit(); commitErr != nil {
				return nil, commitErr
			}
			tx = nil
		}
		return replay, err
	}
	if err := validateUsageReservationMutation(reservation, cmd.OwnerID, cmd.FencingToken, cmd.RowVersion,
		service.UsageReservationStatusAuthorized, service.UsageReservationStatusInFlight); err != nil {
		return nil, err
	}

	newEstimatedBase := reservation.EstimatedBaseCost.Add(cmd.AdditionalBaseCost).RoundCeil(service.UsageReservationMoneyScale)
	newHeldTotal := service.CalculateUsageReservationHold(newEstimatedBase, reservation.ManagementFeeBPS)
	if validationErr := service.ValidateUsageReservationMoney(newEstimatedBase); validationErr != nil {
		return nil, validationErr
	}
	if validationErr := service.ValidateUsageReservationMoney(newHeldTotal); validationErr != nil {
		return nil, service.ErrUsageReservationInvalid
	}
	newHeldBase := newEstimatedBase
	newHeldFee := newHeldTotal.Sub(newHeldBase).Round(service.UsageReservationMoneyScale)
	additionalBaseHold := newHeldBase.Sub(reservation.HeldBaseCost)
	additionalFeeHold := newHeldFee.Sub(reservation.HeldManagementFee)
	additionalTotalHold := newHeldTotal.Sub(reservation.HeldTotal)
	if additionalBaseHold.IsNegative() || additionalFeeHold.IsNegative() || additionalTotalHold.IsNegative() {
		return nil, service.ErrUsageReservationInvalid
	}

	snapshot := reservationFinancialSnapshot{}
	if additionalTotalHold.IsPositive() {
		snapshot, err = reserveUsageFunding(ctx, tx, reservation, additionalTotalHold)
		if err != nil {
			return nil, err
		}
		apiSnapshot, apiErr := reserveUsageAPIKeyConstraints(ctx, tx, reservation.UserID, reservation.APIKeyID, reservation.ParentGroupID, additionalTotalHold)
		if apiErr != nil {
			return nil, apiErr
		}
		snapshot.merge(apiSnapshot)
	}

	updated, err := markUsageReservationRenewed(ctx, tx, reservation, cmd, newEstimatedBase, newHeldBase, newHeldFee, newHeldTotal)
	if err != nil {
		return nil, err
	}
	if err := insertUsageReservationLedgerPair(ctx, tx, updated, service.UsageReservationOperationRenew,
		service.HashUsageReservationKey(cmd.OperationKey), cmd.RequestFingerprint,
		additionalBaseHold, additionalFeeHold, additionalBaseHold, additionalFeeHold,
		decimal.Zero, decimal.Zero, snapshot); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	tx = nil
	return snapshot.result(updated, true), nil
}

// ReconcileExpired only fences and claims expired reservations. It does not
// release funds; the caller must collect attempt/outbox/usage evidence and then
// settle through Capture or Release using the returned owner/epoch/version.
func (r *usageBillingReservationRepository) ReconcileExpired(ctx context.Context, cmd *service.UsageReservationReconcileCommand) (_ *service.UsageReservationReconcileResult, err error) {
	if r == nil || r.db == nil {
		return nil, errors.New("usage billing reservation repository db is nil")
	}
	if cmd == nil {
		return nil, service.ErrUsageReservationInvalid
	}
	cmd.Normalize()
	if err := cmd.Validate(); err != nil {
		return nil, err
	}
	claimSeconds := int64((cmd.ClaimTTL + time.Second - 1) / time.Second)
	if claimSeconds < 1 {
		claimSeconds = 1
	}

	tx, err := beginUsageReservationTx(ctx, r.db)
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	rows, err := tx.QueryContext(ctx, `
		WITH candidates AS (
			SELECT id
			FROM usage_billing_reservations
			WHERE (status IN ('authorized', 'in_flight') AND lease_expires_at <= clock_timestamp())
			   OR (status = 'reconciling' AND reconciliation_lease_expires_at <= clock_timestamp())
			ORDER BY COALESCE(reconciliation_lease_expires_at, lease_expires_at), id
			LIMIT $2
			FOR UPDATE SKIP LOCKED
		), claimed AS (
			UPDATE usage_billing_reservations AS r
			SET status = 'reconciling',
				reconcile_from_status = CASE WHEN r.status = 'reconciling' THEN r.reconcile_from_status ELSE r.status END,
				owner_id = $1,
				lease_epoch = r.lease_epoch + 1,
				row_version = r.row_version + 1,
				reconciliation_lease_expires_at = clock_timestamp() + ($3 * INTERVAL '1 second'),
				updated_at = clock_timestamp()
			FROM candidates AS c
			WHERE r.id = c.id
			-- The locked candidates are expired, so a surviving worker must be
			-- able to take ownership after the previous process disappears.
			RETURNING r.*
		)
		SELECT `+usageReservationSelectColumns+`
		FROM claimed
		ORDER BY lease_expires_at, id
	`, cmd.WorkerID, cmd.Limit, claimSeconds)
	if err != nil {
		return nil, err
	}
	result := &service.UsageReservationReconcileResult{Claimed: make([]service.UsageBillingReservation, 0, cmd.Limit)}
	for rows.Next() {
		reservation, scanErr := scanUsageReservation(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result.Claimed = append(result.Claimed, *reservation)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for i := range result.Claimed {
		reservation := &result.Claimed[i]
		operationKey := service.HashUsageReservationKey(fmt.Sprintf("reconcile:%s:%d", reservation.ID, reservation.FencingToken))
		fingerprint := service.HashUsageReservationKey(fmt.Sprintf("%s|%s|%d|%d", reservation.ID, cmd.WorkerID, reservation.FencingToken, reservation.RowVersion))
		if ledgerErr := insertUsageReservationLedgerPair(ctx, tx, reservation, service.UsageReservationOperationReconcile,
			operationKey, fingerprint, decimal.Zero, decimal.Zero, decimal.Zero, decimal.Zero, decimal.Zero, decimal.Zero,
			reservationFinancialSnapshot{}); ledgerErr != nil {
			return nil, ledgerErr
		}
	}
	result.Examined = len(result.Claimed)
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	tx = nil
	return result, nil
}

// VoidPendingAdaptiveUsage marks an orphaned pending usage row as rejected so a
// reservation whose active-attempt fence no longer matches can be released
// instead of deadlocking in reconciling (H2). It is deliberately conservative:
// the row must still be pending with a zero actual cost and the exact evidence
// hash, and returns the number of rows affected for the caller to verify.
func (r *usageBillingReservationRepository) VoidPendingAdaptiveUsage(
	ctx context.Context,
	reservationID string,
	usageLogID int64,
	usageLogCreatedAt time.Time,
	evidenceHash string,
) (int64, error) {
	if r == nil || r.db == nil {
		return 0, errors.New("usage billing reservation repository db is nil")
	}
	if strings.TrimSpace(reservationID) == "" || usageLogID <= 0 || usageLogCreatedAt.IsZero() || strings.TrimSpace(evidenceHash) == "" {
		return 0, service.ErrUsageReservationInvalid
	}
	result, err := r.db.ExecContext(ctx, `
		UPDATE usage_logs
		SET adaptive_settlement_status = $1
		WHERE id = $2 AND created_at = $3
		  AND adaptive_reservation_id = $4
		  AND adaptive_evidence_hash = $5
		  AND adaptive_settlement_status = 'pending'
		  AND COALESCE(actual_cost, 0) = 0
	`, service.AdaptiveSettlementStatusRejected, usageLogID, usageLogCreatedAt.UTC(),
		strings.TrimSpace(reservationID), strings.TrimSpace(evidenceHash))
	if err != nil {
		return 0, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	return affected, nil
}

func insertUsageReservation(
	ctx context.Context,
	tx *sql.Tx,
	reservationID string,
	keyHash string,
	cmd *service.UsageReservationReserveCommand,
	heldBase, heldFee, heldTotal decimal.Decimal,
) (*service.UsageBillingReservation, bool, error) {
	row := tx.QueryRowContext(ctx, `
		INSERT INTO usage_billing_reservations (
			id, idempotency_key_hash, request_fingerprint, logical_request_id, user_id, api_key_id,
			parent_group_id, canonical_model, pricing_snapshot_id, pricing_generation, config_generation,
			subscription_id, funding_source, status, management_fee_bps,
			estimated_base_cost, held_base_cost, held_management_fee, held_total,
			owner_id, lease_expires_at
		)
		SELECT $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11,
			$12, $13, 'authorized', $14, $15, $16, $17, $18, $19,
			clock_timestamp() + ($20 * INTERVAL '1 second')
		ON CONFLICT (api_key_id, idempotency_key_hash) DO NOTHING
		RETURNING `+usageReservationSelectColumns,
		reservationID, keyHash, cmd.RequestFingerprint, cmd.LogicalRequestID, cmd.UserID, cmd.APIKeyID,
		cmd.ParentGroupID, cmd.CanonicalModel, cmd.PricingSnapshotID, cmd.PricingGeneration, cmd.ConfigGeneration,
		cmd.SubscriptionID, cmd.FundingSource, cmd.ManagementFeeBPS, cmd.EstimatedBaseCost,
		heldBase, heldFee, heldTotal, cmd.OwnerID, int64(cmd.LeaseTTL/time.Second),
	)
	reservation, err := scanUsageReservation(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	return reservation, err == nil, err
}

func lockUsageReservation(ctx context.Context, tx *sql.Tx, reservationID string) (*service.UsageBillingReservation, error) {
	return scanUsageReservation(tx.QueryRowContext(ctx, `
		SELECT `+usageReservationSelectColumns+`
		FROM usage_billing_reservations
		WHERE id = $1
		FOR UPDATE
	`, reservationID))
}

func lockUsageReservationByIdempotencyKey(ctx context.Context, tx *sql.Tx, apiKeyID int64, keyHash string) (*service.UsageBillingReservation, error) {
	return scanUsageReservation(tx.QueryRowContext(ctx, `
		SELECT `+usageReservationSelectColumns+`
		FROM usage_billing_reservations
		WHERE api_key_id = $1 AND idempotency_key_hash = $2
		FOR UPDATE
	`, apiKeyID, keyHash))
}

func scanUsageReservation(scanner usageReservationRowScanner) (*service.UsageBillingReservation, error) {
	reservation := &service.UsageBillingReservation{}
	var subscriptionID sql.NullInt64
	var parentGroupID, winningLeafGroupID, winningAttemptNo, activeLeafGroupID, activeAttemptNo sql.NullInt64
	var usageLogID sql.NullInt64
	var usageLogCreatedAt, reconciliationLease, attemptStartedAt, capturedAt, releasedAt sql.NullTime
	var usageEvidenceHash, reconcileFromStatus, releaseReason sql.NullString
	err := scanner.Scan(
		&reservation.ID,
		&reservation.IdempotencyKeyHash,
		&reservation.RequestFingerprint,
		&reservation.LogicalRequestID,
		&reservation.UserID,
		&reservation.APIKeyID,
		&parentGroupID,
		&reservation.CanonicalModel,
		&reservation.PricingSnapshotID,
		&reservation.PricingGeneration,
		&reservation.ConfigGeneration,
		&subscriptionID,
		&reservation.FundingSource,
		&reservation.Status,
		&reservation.ManagementFeeBPS,
		&reservation.EstimatedBaseCost,
		&reservation.HeldBaseCost,
		&reservation.HeldManagementFee,
		&reservation.HeldTotal,
		&reservation.UncappedBaseCost,
		&reservation.CapturedBaseCost,
		&reservation.CapturedManagementFee,
		&reservation.CapturedTotal,
		&reservation.PlatformOverageBaseCost,
		&winningLeafGroupID,
		&winningAttemptNo,
		&usageLogID,
		&usageLogCreatedAt,
		&usageEvidenceHash,
		&activeLeafGroupID,
		&activeAttemptNo,
		&attemptStartedAt,
		&reconcileFromStatus,
		&reservation.OwnerID,
		&reservation.FencingToken,
		&reservation.RowVersion,
		&reservation.LeaseExpiresAt,
		&reconciliationLease,
		&capturedAt,
		&releasedAt,
		&releaseReason,
		&reservation.CreatedAt,
		&reservation.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	reservation.IdempotencyKeyHash = strings.TrimSpace(reservation.IdempotencyKeyHash)
	reservation.RequestFingerprint = strings.TrimSpace(reservation.RequestFingerprint)
	if subscriptionID.Valid {
		value := subscriptionID.Int64
		reservation.SubscriptionID = &value
	}
	if parentGroupID.Valid {
		value := parentGroupID.Int64
		reservation.ParentGroupID = &value
	}
	if winningLeafGroupID.Valid {
		value := winningLeafGroupID.Int64
		reservation.WinningLeafGroupID = &value
	}
	if winningAttemptNo.Valid {
		value := int(winningAttemptNo.Int64)
		reservation.WinningAttemptNo = &value
	}
	if usageLogID.Valid {
		value := usageLogID.Int64
		reservation.UsageLogID = &value
	}
	if usageLogCreatedAt.Valid {
		value := usageLogCreatedAt.Time
		reservation.UsageLogCreatedAt = &value
	}
	if usageEvidenceHash.Valid {
		reservation.UsageEvidenceHash = strings.TrimSpace(usageEvidenceHash.String)
	}
	if activeLeafGroupID.Valid {
		value := activeLeafGroupID.Int64
		reservation.ActiveLeafGroupID = &value
	}
	if activeAttemptNo.Valid {
		value := int(activeAttemptNo.Int64)
		reservation.ActiveAttemptNo = &value
	}
	if attemptStartedAt.Valid {
		value := attemptStartedAt.Time
		reservation.AttemptStartedAt = &value
	}
	if reconcileFromStatus.Valid {
		reservation.ReconcileFromStatus = reconcileFromStatus.String
	}
	if reconciliationLease.Valid {
		value := reconciliationLease.Time
		reservation.ReconciliationLeaseExpiresAt = &value
	}
	if capturedAt.Valid {
		value := capturedAt.Time
		reservation.CapturedAt = &value
	}
	if releasedAt.Valid {
		value := releasedAt.Time
		reservation.ReleasedAt = &value
	}
	if releaseReason.Valid {
		reservation.ReleaseReason = releaseReason.String
	}
	return reservation, nil
}

func validateUsageReservationMutation(reservation *service.UsageBillingReservation, ownerID string, fence, rowVersion int64, allowedStatuses ...string) error {
	if reservation == nil {
		return service.ErrUsageReservationNotFound
	}
	statusAllowed := false
	for _, allowed := range allowedStatuses {
		if reservation.Status == allowed {
			statusAllowed = true
			break
		}
	}
	if !statusAllowed {
		return service.ErrUsageReservationNotHeld
	}
	if reservation.OwnerID != ownerID {
		return service.ErrUsageReservationOwnerConflict
	}
	if reservation.FencingToken != fence {
		return service.ErrUsageReservationFenceConflict
	}
	if reservation.RowVersion != rowVersion {
		return service.ErrUsageReservationVersionConflict
	}
	return nil
}

func replayStartedAttempt(ctx context.Context, tx *sql.Tx, reservation *service.UsageBillingReservation, cmd *service.UsageReservationMarkInFlightCommand) (bool, error) {
	var attemptNo int
	var leafGroupID int64
	var status, operationKeyHash, fingerprint, evidenceHash string
	err := tx.QueryRowContext(ctx, `
		SELECT attempt_no, leaf_group_id, status, start_operation_key_hash, start_fingerprint, start_evidence_hash
		FROM usage_billing_attempts
		WHERE reservation_id = $1 AND (attempt_no = $2 OR start_operation_key_hash = $3)
		ORDER BY attempt_no
		LIMIT 1
		FOR UPDATE
	`, reservation.ID, cmd.AttemptNo, service.HashUsageReservationKey(cmd.OperationKey)).Scan(
		&attemptNo, &leafGroupID, &status, &operationKeyHash, &fingerprint, &evidenceHash)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if attemptNo != cmd.AttemptNo || leafGroupID != cmd.LeafGroupID || status != "started" ||
		strings.TrimSpace(operationKeyHash) != service.HashUsageReservationKey(cmd.OperationKey) ||
		strings.TrimSpace(fingerprint) != cmd.RequestFingerprint || strings.TrimSpace(evidenceHash) != cmd.EvidenceHash {
		return false, service.ErrUsageReservationAttemptConflict
	}
	if reservation.ActiveAttemptNo == nil || reservation.ActiveLeafGroupID == nil ||
		*reservation.ActiveAttemptNo != cmd.AttemptNo || *reservation.ActiveLeafGroupID != cmd.LeafGroupID {
		return false, service.ErrUsageReservationAttemptConflict
	}
	if err := validateUsageReservationReplay(ctx, tx, reservation, cmd.OwnerID, cmd.FencingToken,
		cmd.RowVersion, true, service.UsageReservationStatusInFlight); err != nil {
		return false, err
	}
	return true, nil
}

func replayFailedAttempt(ctx context.Context, tx *sql.Tx, reservation *service.UsageBillingReservation, cmd *service.UsageReservationAttemptFailedCommand) (bool, error) {
	var status string
	var operationKeyHash, fingerprint, evidenceHash, failureClass sql.NullString
	err := tx.QueryRowContext(ctx, `
		SELECT status, failure_operation_key_hash, failure_fingerprint, failure_evidence_hash, failure_class
		FROM usage_billing_attempts
		WHERE reservation_id = $1 AND attempt_no = $2
		FOR UPDATE
	`, reservation.ID, cmd.AttemptNo).Scan(&status, &operationKeyHash, &fingerprint, &evidenceHash, &failureClass)
	if errors.Is(err, sql.ErrNoRows) {
		return false, service.ErrUsageReservationAttemptConflict
	}
	if err != nil {
		return false, err
	}
	if status == "started" {
		return false, nil
	}
	if status != "failed" || !operationKeyHash.Valid || !fingerprint.Valid || !evidenceHash.Valid || !failureClass.Valid ||
		strings.TrimSpace(operationKeyHash.String) != service.HashUsageReservationKey(cmd.OperationKey) ||
		strings.TrimSpace(fingerprint.String) != cmd.RequestFingerprint ||
		strings.TrimSpace(evidenceHash.String) != cmd.EvidenceHash || failureClass.String != cmd.FailureClass {
		return false, service.ErrUsageReservationAttemptConflict
	}
	if reservation.ActiveAttemptNo != nil || reservation.ActiveLeafGroupID != nil || reservation.AttemptStartedAt != nil {
		return false, service.ErrUsageReservationAttemptConflict
	}
	if err := validateUsageReservationReplay(ctx, tx, reservation, cmd.OwnerID, cmd.FencingToken,
		cmd.RowVersion, true, service.UsageReservationStatusAuthorized, service.UsageReservationStatusReconciling); err != nil {
		return false, err
	}
	return true, nil
}

func validateUsageReservationReplay(
	ctx context.Context,
	tx *sql.Tx,
	reservation *service.UsageBillingReservation,
	ownerID string,
	fence, baseRowVersion int64,
	requireLiveLease bool,
	allowedStatuses ...string,
) error {
	if reservation.OwnerID != ownerID {
		return service.ErrUsageReservationOwnerConflict
	}
	if reservation.FencingToken != fence {
		return service.ErrUsageReservationFenceConflict
	}
	if reservation.RowVersion != baseRowVersion+1 {
		return service.ErrUsageReservationVersionConflict
	}
	statusAllowed := false
	for _, status := range allowedStatuses {
		if reservation.Status == status {
			statusAllowed = true
			break
		}
	}
	if !statusAllowed {
		return service.ErrUsageReservationNotHeld
	}
	if !requireLiveLease {
		return nil
	}

	var leaseValid bool
	err := tx.QueryRowContext(ctx, `
		SELECT CASE
			WHEN status IN ('authorized', 'in_flight') THEN lease_expires_at > clock_timestamp()
			WHEN status = 'reconciling' THEN reconciliation_lease_expires_at > clock_timestamp()
			ELSE FALSE
		END
		FROM usage_billing_reservations
		WHERE id = $1
	`, reservation.ID).Scan(&leaseValid)
	if err != nil {
		return err
	}
	if !leaseValid {
		return service.ErrUsageReservationLeaseExpired
	}
	return nil
}

func calculateReservationCapture(
	base decimal.Decimal,
	reservation *service.UsageBillingReservation,
) (service.AdaptiveManagementFeeSettlement, error) {
	return service.CalculateAdaptiveManagementFeeSettlementDecimalWithBPS(
		base,
		reservation.HeldBaseCost,
		reservation.HeldManagementFee,
		reservation.HeldTotal,
		reservation.ManagementFeeBPS,
	)
}

func captureUsageReservationEvidence(
	ctx context.Context,
	tx *sql.Tx,
	reservation *service.UsageBillingReservation,
	cmd *service.UsageReservationCaptureCommand,
	settlement service.AdaptiveManagementFeeSettlement,
) error {
	if reservation.ActiveAttemptNo == nil || reservation.ActiveLeafGroupID == nil ||
		*reservation.ActiveAttemptNo != cmd.AttemptNo || *reservation.ActiveLeafGroupID != cmd.WinningLeafGroupID {
		return service.ErrUsageReservationEvidenceConflict
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE usage_logs
		SET actual_cost = $1, adaptive_settlement_status = 'captured'
		WHERE id = $2 AND created_at = $3
		  AND adaptive_reservation_id = $4
		  AND adaptive_settlement_status = 'pending'
		  AND COALESCE(actual_cost, 0) = 0
		  AND adaptive_evidence_hash = $5
		  AND routed_group_id = $6
		  AND adaptive_attempt_no = $7
		  AND adaptive_base_cost = $8
		  AND adaptive_management_fee_cost = $9
		  AND adaptive_total_cost = $1
		  AND adaptive_pricing_snapshot_id = $10
		  AND adaptive_uncapped_base_cost = $11
		  AND adaptive_platform_overage_cost = $12
		  AND user_id = $13
		  AND api_key_id = $14
		  AND subscription_id IS NOT DISTINCT FROM $15
		  AND billing_type = $16
		  AND adaptive_parent_group_id IS NOT DISTINCT FROM $17
	`, settlement.CustomerCharge.CaptureAmount, cmd.UsageLogID, cmd.UsageLogCreatedAt, reservation.ID,
		cmd.EvidenceHash, cmd.WinningLeafGroupID, cmd.AttemptNo,
		settlement.CustomerCharge.BaseAmount, settlement.CustomerCharge.FeeAmount,
		reservation.PricingSnapshotID, settlement.UncappedBaseAmount, settlement.PlatformOverageBaseAmount,
		reservation.UserID, reservation.APIKeyID, reservation.SubscriptionID,
		usageReservationBillingType(reservation.FundingSource), reservation.ParentGroupID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return service.ErrUsageReservationEvidenceConflict
	}
	result, err = tx.ExecContext(ctx, `
		UPDATE usage_billing_attempts
		SET status = 'succeeded', usage_log_id = $1, usage_log_created_at = $2,
			usage_evidence_hash = $3, finished_at = NOW(), updated_at = NOW()
		WHERE reservation_id = $4 AND attempt_no = $5 AND leaf_group_id = $6 AND status = 'started'
	`, cmd.UsageLogID, cmd.UsageLogCreatedAt, cmd.EvidenceHash, reservation.ID, cmd.AttemptNo, cmd.WinningLeafGroupID)
	if err != nil {
		return err
	}
	if affected, err = result.RowsAffected(); err != nil {
		return err
	} else if affected != 1 {
		return service.ErrUsageReservationEvidenceConflict
	}
	return nil
}

func validateUsageReservationReleaseEvidence(ctx context.Context, tx *sql.Tx, reservation *service.UsageBillingReservation, evidenceHash string) error {
	if err := lockAndRejectUsageReservationEvidence(ctx, tx, reservation.ID, nil); err != nil {
		return err
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT status, failure_evidence_hash
		FROM usage_billing_attempts
		WHERE reservation_id = $1
		ORDER BY attempt_no
		FOR UPDATE
	`, reservation.ID)
	if err != nil {
		return err
	}
	defer rows.Close()

	total := 0
	latestEvidence := ""
	for rows.Next() {
		var status string
		var failureEvidence sql.NullString
		if err := rows.Scan(&status, &failureEvidence); err != nil {
			return err
		}
		total++
		if status != "failed" || !failureEvidence.Valid {
			return service.ErrUsageReservationEvidenceConflict
		}
		latestEvidence = strings.TrimSpace(failureEvidence.String)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if total > 0 {
		if strings.TrimSpace(evidenceHash) == "" {
			return service.ErrUsageReservationEvidenceRequired
		}
		if strings.TrimSpace(latestEvidence) != strings.TrimSpace(evidenceHash) {
			return service.ErrUsageReservationEvidenceConflict
		}
	}
	return nil
}

func lockAndRejectUsageReservationEvidence(
	ctx context.Context,
	tx *sql.Tx,
	reservationID string,
	attemptNo *int,
) error {
	var attemptFilter any
	if attemptNo != nil {
		attemptFilter = *attemptNo
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT id
		FROM usage_logs
		WHERE adaptive_reservation_id = $1
		  AND adaptive_settlement_status = 'pending'
		  AND ($2::integer IS NULL OR adaptive_attempt_no = $2)
		ORDER BY created_at, id
		FOR UPDATE
	`, reservationID, attemptFilter)
	if err != nil {
		return err
	}
	defer rows.Close()
	if rows.Next() {
		return service.ErrUsageReservationEvidenceConflict
	}
	return rows.Err()
}

func usageReservationBillingType(fundingSource string) int8 {
	if fundingSource == service.UsageReservationFundingSubscription {
		return service.BillingTypeSubscription
	}
	return service.BillingTypeBalance
}

func (s *reservationFinancialSnapshot) merge(other reservationFinancialSnapshot) {
	if other.availableBalance != nil {
		s.availableBalance = other.availableBalance
	}
	if other.adaptiveReservedBalance != nil {
		s.adaptiveReservedBalance = other.adaptiveReservedBalance
	}
	if other.subscriptionReserved != nil {
		s.subscriptionReserved = other.subscriptionReserved
		s.subscriptionDailyUsage = other.subscriptionDailyUsage
		s.subscriptionWeeklyUsage = other.subscriptionWeeklyUsage
		s.subscriptionMonthlyUsage = other.subscriptionMonthlyUsage
	}
	if other.apiKeyReservedQuota != nil {
		s.apiKeyReservedQuota = other.apiKeyReservedQuota
		s.apiKeyQuotaUsed = other.apiKeyQuotaUsed
		s.apiKeyReserved5h = other.apiKeyReserved5h
		s.apiKeyReserved1d = other.apiKeyReserved1d
		s.apiKeyReserved7d = other.apiKeyReserved7d
	}
}

func reserveUsageFunding(ctx context.Context, tx *sql.Tx, reservation *service.UsageBillingReservation, amount decimal.Decimal) (reservationFinancialSnapshot, error) {
	if reservation.FundingSource == service.UsageReservationFundingBalance {
		var balance, adaptiveReserved decimal.Decimal
		err := tx.QueryRowContext(ctx, `
			UPDATE users
			SET balance = balance - $1,
				adaptive_reserved_balance = adaptive_reserved_balance + $1,
				updated_at = NOW()
			WHERE id = $2 AND deleted_at IS NULL AND status = $3 AND balance >= $1
			RETURNING balance, adaptive_reserved_balance
		`, amount, reservation.UserID, service.StatusActive).Scan(&balance, &adaptiveReserved)
		if err == nil {
			return reservationFinancialSnapshot{availableBalance: &balance, adaptiveReservedBalance: &adaptiveReserved}, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return reservationFinancialSnapshot{}, err
		}
		var exists bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE id = $1 AND deleted_at IS NULL)`, reservation.UserID).Scan(&exists); err != nil {
			return reservationFinancialSnapshot{}, err
		}
		if !exists {
			return reservationFinancialSnapshot{}, service.ErrUserNotFound
		}
		return reservationFinancialSnapshot{}, service.ErrUsageReservationInsufficientBalance
	}

	if reservation.SubscriptionID == nil {
		return reservationFinancialSnapshot{}, service.ErrUsageReservationInvalid
	}
	var reserved, daily, weekly, monthly decimal.Decimal
	err := tx.QueryRowContext(ctx, `
		UPDATE user_subscriptions AS us
		SET daily_usage_usd = CASE
				WHEN us.daily_window_start IS NOT NULL
				 AND us.expires_at > us.starts_at + INTERVAL '1 day'
				 AND us.daily_window_start + INTERVAL '24 hours' <= NOW() THEN 0
				ELSE us.daily_usage_usd END,
			weekly_usage_usd = CASE
				WHEN us.weekly_window_start IS NOT NULL
				 AND us.weekly_window_start + INTERVAL '7 days' <= NOW() THEN 0
				ELSE us.weekly_usage_usd END,
			monthly_usage_usd = CASE
				WHEN us.monthly_window_start IS NOT NULL
				 AND us.monthly_window_start + INTERVAL '30 days' <= NOW() THEN 0
				ELSE us.monthly_usage_usd END,
			daily_window_start = CASE
				WHEN us.daily_window_start IS NULL THEN NOW()
				WHEN us.expires_at > us.starts_at + INTERVAL '1 day'
				 AND us.daily_window_start + INTERVAL '24 hours' <= NOW() THEN NOW()
				ELSE us.daily_window_start END,
			weekly_window_start = CASE
				WHEN us.weekly_window_start IS NULL OR us.weekly_window_start + INTERVAL '7 days' <= NOW() THEN NOW()
				ELSE us.weekly_window_start END,
			monthly_window_start = CASE
				WHEN us.monthly_window_start IS NULL OR us.monthly_window_start + INTERVAL '30 days' <= NOW() THEN NOW()
				ELSE us.monthly_window_start END,
			reserved_usage_usd = us.reserved_usage_usd + $1,
			updated_at = NOW()
		FROM groups AS g
		WHERE us.id = $2
		  AND us.user_id = $3
		  AND us.group_id = g.id
		  AND us.deleted_at IS NULL
		  AND g.deleted_at IS NULL
		  AND us.status = $4
		  AND ($5::bigint IS NULL OR us.group_id = $5)
		  AND us.starts_at <= NOW()
		  AND us.expires_at > NOW()
		  AND (g.daily_limit_usd IS NULL OR g.daily_limit_usd <= 0 OR
			(CASE WHEN us.daily_window_start IS NOT NULL
				AND us.expires_at > us.starts_at + INTERVAL '1 day'
				AND us.daily_window_start + INTERVAL '24 hours' <= NOW()
			 THEN 0 ELSE us.daily_usage_usd END) + us.reserved_usage_usd + $1 <= g.daily_limit_usd)
		  AND (g.weekly_limit_usd IS NULL OR g.weekly_limit_usd <= 0 OR
			(CASE WHEN us.weekly_window_start IS NOT NULL AND us.weekly_window_start + INTERVAL '7 days' <= NOW()
			 THEN 0 ELSE us.weekly_usage_usd END) + us.reserved_usage_usd + $1 <= g.weekly_limit_usd)
		  AND (g.monthly_limit_usd IS NULL OR g.monthly_limit_usd <= 0 OR
			(CASE WHEN us.monthly_window_start IS NOT NULL AND us.monthly_window_start + INTERVAL '30 days' <= NOW()
			 THEN 0 ELSE us.monthly_usage_usd END) + us.reserved_usage_usd + $1 <= g.monthly_limit_usd)
		RETURNING us.reserved_usage_usd, us.daily_usage_usd, us.weekly_usage_usd, us.monthly_usage_usd
	`, amount, *reservation.SubscriptionID, reservation.UserID, service.SubscriptionStatusActive, reservation.ParentGroupID).
		Scan(&reserved, &daily, &weekly, &monthly)
	if err == nil {
		return reservationFinancialSnapshot{
			subscriptionReserved: &reserved, subscriptionDailyUsage: &daily,
			subscriptionWeeklyUsage: &weekly, subscriptionMonthlyUsage: &monthly,
		}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return reservationFinancialSnapshot{}, err
	}
	return reservationFinancialSnapshot{}, classifySubscriptionReservationFailure(ctx, tx, *reservation.SubscriptionID, reservation.UserID, amount)
}

func captureUsageFunding(ctx context.Context, tx *sql.Tx, reservation *service.UsageBillingReservation, actual decimal.Decimal) (reservationFinancialSnapshot, error) {
	if reservation.FundingSource == service.UsageReservationFundingBalance {
		var balance, adaptiveReserved decimal.Decimal
		err := tx.QueryRowContext(ctx, `
			UPDATE users
			SET balance = balance + ($1 - $2),
				adaptive_reserved_balance = adaptive_reserved_balance - $1,
				updated_at = NOW()
			WHERE id = $3 AND adaptive_reserved_balance >= $1
			RETURNING balance, adaptive_reserved_balance
		`, reservation.HeldTotal, actual, reservation.UserID).Scan(&balance, &adaptiveReserved)
		if errors.Is(err, sql.ErrNoRows) {
			return reservationFinancialSnapshot{}, errors.New("usage reservation adaptive balance invariant violated")
		}
		if err != nil {
			return reservationFinancialSnapshot{}, err
		}
		return reservationFinancialSnapshot{availableBalance: &balance, adaptiveReservedBalance: &adaptiveReserved}, nil
	}

	if reservation.SubscriptionID == nil {
		return reservationFinancialSnapshot{}, service.ErrUsageReservationInvalid
	}
	var reserved, daily, weekly, monthly decimal.Decimal
	err := tx.QueryRowContext(ctx, `
		UPDATE user_subscriptions
		SET daily_usage_usd = (CASE
				WHEN daily_window_start IS NOT NULL
				 AND expires_at > starts_at + INTERVAL '1 day'
				 AND daily_window_start + INTERVAL '24 hours' <= NOW() THEN 0
				ELSE daily_usage_usd END) + $2,
			weekly_usage_usd = (CASE
				WHEN weekly_window_start IS NOT NULL AND weekly_window_start + INTERVAL '7 days' <= NOW() THEN 0
				ELSE weekly_usage_usd END) + $2,
			monthly_usage_usd = (CASE
				WHEN monthly_window_start IS NOT NULL AND monthly_window_start + INTERVAL '30 days' <= NOW() THEN 0
				ELSE monthly_usage_usd END) + $2,
			daily_window_start = CASE
				WHEN daily_window_start IS NULL THEN NOW()
				WHEN expires_at > starts_at + INTERVAL '1 day'
				 AND daily_window_start + INTERVAL '24 hours' <= NOW() THEN NOW()
				ELSE daily_window_start END,
			weekly_window_start = CASE
				WHEN weekly_window_start IS NULL OR weekly_window_start + INTERVAL '7 days' <= NOW() THEN NOW()
				ELSE weekly_window_start END,
			monthly_window_start = CASE
				WHEN monthly_window_start IS NULL OR monthly_window_start + INTERVAL '30 days' <= NOW() THEN NOW()
				ELSE monthly_window_start END,
			reserved_usage_usd = reserved_usage_usd - $1,
			updated_at = NOW()
		WHERE id = $3 AND user_id = $4 AND reserved_usage_usd >= $1
		RETURNING reserved_usage_usd, daily_usage_usd, weekly_usage_usd, monthly_usage_usd
	`, reservation.HeldTotal, actual, *reservation.SubscriptionID, reservation.UserID).
		Scan(&reserved, &daily, &weekly, &monthly)
	if errors.Is(err, sql.ErrNoRows) {
		return reservationFinancialSnapshot{}, errors.New("usage reservation subscription hold invariant violated")
	}
	if err != nil {
		return reservationFinancialSnapshot{}, err
	}
	return reservationFinancialSnapshot{
		subscriptionReserved: &reserved, subscriptionDailyUsage: &daily,
		subscriptionWeeklyUsage: &weekly, subscriptionMonthlyUsage: &monthly,
	}, nil
}

func releaseUsageFunding(ctx context.Context, tx *sql.Tx, reservation *service.UsageBillingReservation) (reservationFinancialSnapshot, error) {
	if reservation.FundingSource == service.UsageReservationFundingBalance {
		var balance, adaptiveReserved decimal.Decimal
		err := tx.QueryRowContext(ctx, `
			UPDATE users
			SET balance = balance + $1,
				adaptive_reserved_balance = adaptive_reserved_balance - $1,
				updated_at = NOW()
			WHERE id = $2 AND adaptive_reserved_balance >= $1
			RETURNING balance, adaptive_reserved_balance
		`, reservation.HeldTotal, reservation.UserID).Scan(&balance, &adaptiveReserved)
		if errors.Is(err, sql.ErrNoRows) {
			return reservationFinancialSnapshot{}, errors.New("usage reservation adaptive balance invariant violated")
		}
		if err != nil {
			return reservationFinancialSnapshot{}, err
		}
		return reservationFinancialSnapshot{availableBalance: &balance, adaptiveReservedBalance: &adaptiveReserved}, nil
	}
	if reservation.SubscriptionID == nil {
		return reservationFinancialSnapshot{}, service.ErrUsageReservationInvalid
	}
	var reserved, daily, weekly, monthly decimal.Decimal
	err := tx.QueryRowContext(ctx, `
		UPDATE user_subscriptions
		SET reserved_usage_usd = reserved_usage_usd - $1, updated_at = NOW()
		WHERE id = $2 AND user_id = $3 AND reserved_usage_usd >= $1
		RETURNING reserved_usage_usd, daily_usage_usd, weekly_usage_usd, monthly_usage_usd
	`, reservation.HeldTotal, *reservation.SubscriptionID, reservation.UserID).
		Scan(&reserved, &daily, &weekly, &monthly)
	if errors.Is(err, sql.ErrNoRows) {
		return reservationFinancialSnapshot{}, errors.New("usage reservation subscription hold invariant violated")
	}
	if err != nil {
		return reservationFinancialSnapshot{}, err
	}
	return reservationFinancialSnapshot{
		subscriptionReserved: &reserved, subscriptionDailyUsage: &daily,
		subscriptionWeeklyUsage: &weekly, subscriptionMonthlyUsage: &monthly,
	}, nil
}

func classifySubscriptionReservationFailure(ctx context.Context, tx *sql.Tx, subscriptionID, userID int64, amount decimal.Decimal) error {
	var available, limitBlocked bool
	err := tx.QueryRowContext(ctx, `
		SELECT
			us.user_id = $2 AND us.deleted_at IS NULL AND us.status = $3
				AND us.starts_at <= NOW() AND us.expires_at > NOW(),
			COALESCE((g.daily_limit_usd > 0 AND
			 (CASE WHEN us.daily_window_start IS NOT NULL
				AND us.expires_at > us.starts_at + INTERVAL '1 day'
				AND us.daily_window_start + INTERVAL '24 hours' <= NOW()
			  THEN 0 ELSE us.daily_usage_usd END) + us.reserved_usage_usd + $4 > g.daily_limit_usd)
			OR (g.weekly_limit_usd > 0 AND
			 (CASE WHEN us.weekly_window_start IS NOT NULL AND us.weekly_window_start + INTERVAL '7 days' <= NOW()
			  THEN 0 ELSE us.weekly_usage_usd END) + us.reserved_usage_usd + $4 > g.weekly_limit_usd)
			OR (g.monthly_limit_usd > 0 AND
			 (CASE WHEN us.monthly_window_start IS NOT NULL AND us.monthly_window_start + INTERVAL '30 days' <= NOW()
			  THEN 0 ELSE us.monthly_usage_usd END) + us.reserved_usage_usd + $4 > g.monthly_limit_usd), FALSE)
		FROM user_subscriptions AS us
		JOIN groups AS g ON g.id = us.group_id AND g.deleted_at IS NULL
		WHERE us.id = $1
		FOR UPDATE OF us
	`, subscriptionID, userID, service.SubscriptionStatusActive, amount).Scan(&available, &limitBlocked)
	if errors.Is(err, sql.ErrNoRows) {
		return service.ErrUsageReservationSubscriptionUnavailable
	}
	if err != nil {
		return err
	}
	if !available {
		return service.ErrUsageReservationSubscriptionUnavailable
	}
	if limitBlocked {
		return service.ErrUsageReservationSubscriptionLimit
	}
	return service.ErrUsageReservationSubscriptionUnavailable
}

func reserveUsageAPIKeyConstraints(ctx context.Context, tx *sql.Tx, userID, apiKeyID int64, parentGroupID *int64, amount decimal.Decimal) (reservationFinancialSnapshot, error) {
	var reservedQuota, quotaUsed, reserved5h, reserved1d, reserved7d decimal.Decimal
	err := tx.QueryRowContext(ctx, `
		UPDATE api_keys
		SET usage_5h = CASE WHEN window_5h_start IS NULL OR window_5h_start + INTERVAL '5 hours' <= NOW() THEN 0 ELSE usage_5h END,
			usage_1d = CASE WHEN window_1d_start IS NULL OR window_1d_start + INTERVAL '24 hours' <= NOW() THEN 0 ELSE usage_1d END,
			usage_7d = CASE WHEN window_7d_start IS NULL OR window_7d_start + INTERVAL '7 days' <= NOW() THEN 0 ELSE usage_7d END,
			window_5h_start = CASE WHEN window_5h_start IS NULL OR window_5h_start + INTERVAL '5 hours' <= NOW() THEN NOW() ELSE window_5h_start END,
			window_1d_start = CASE WHEN window_1d_start IS NULL OR window_1d_start + INTERVAL '24 hours' <= NOW() THEN date_trunc('day', NOW()) ELSE window_1d_start END,
			window_7d_start = CASE WHEN window_7d_start IS NULL OR window_7d_start + INTERVAL '7 days' <= NOW() THEN date_trunc('day', NOW()) ELSE window_7d_start END,
			reserved_quota_usd = reserved_quota_usd + $1,
			reserved_usage_5h_usd = reserved_usage_5h_usd + $1,
			reserved_usage_1d_usd = reserved_usage_1d_usd + $1,
			reserved_usage_7d_usd = reserved_usage_7d_usd + $1,
			updated_at = NOW()
		WHERE id = $2
		  AND user_id = $3
		  AND deleted_at IS NULL
		  AND status = $4
		  AND ($5::bigint IS NULL OR group_id = $5)
		  AND (expires_at IS NULL OR expires_at > NOW())
		  AND (quota <= 0 OR quota_used + reserved_quota_usd + $1 <= quota)
		  AND (rate_limit_5h <= 0 OR
			(CASE WHEN window_5h_start IS NULL OR window_5h_start + INTERVAL '5 hours' <= NOW() THEN 0 ELSE usage_5h END)
			+ reserved_usage_5h_usd + $1 <= rate_limit_5h)
		  AND (rate_limit_1d <= 0 OR
			(CASE WHEN window_1d_start IS NULL OR window_1d_start + INTERVAL '24 hours' <= NOW() THEN 0 ELSE usage_1d END)
			+ reserved_usage_1d_usd + $1 <= rate_limit_1d)
		  AND (rate_limit_7d <= 0 OR
			(CASE WHEN window_7d_start IS NULL OR window_7d_start + INTERVAL '7 days' <= NOW() THEN 0 ELSE usage_7d END)
			+ reserved_usage_7d_usd + $1 <= rate_limit_7d)
		RETURNING reserved_quota_usd, quota_used,
			reserved_usage_5h_usd, reserved_usage_1d_usd, reserved_usage_7d_usd
	`, amount, apiKeyID, userID, service.StatusAPIKeyActive, parentGroupID).
		Scan(&reservedQuota, &quotaUsed, &reserved5h, &reserved1d, &reserved7d)
	if err == nil {
		return reservationFinancialSnapshot{
			apiKeyReservedQuota: &reservedQuota, apiKeyQuotaUsed: &quotaUsed,
			apiKeyReserved5h: &reserved5h, apiKeyReserved1d: &reserved1d, apiKeyReserved7d: &reserved7d,
		}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return reservationFinancialSnapshot{}, err
	}
	return reservationFinancialSnapshot{}, classifyAPIKeyReservationFailure(ctx, tx, userID, apiKeyID, amount)
}

func captureUsageAPIKeyConstraints(ctx context.Context, tx *sql.Tx, apiKeyID int64, held, actual decimal.Decimal) (reservationFinancialSnapshot, error) {
	var reservedQuota, quotaUsed, reserved5h, reserved1d, reserved7d decimal.Decimal
	err := tx.QueryRowContext(ctx, `
		UPDATE api_keys
		SET quota_used = CASE WHEN quota > 0 THEN quota_used + $2 ELSE quota_used END,
			usage_5h = (CASE WHEN window_5h_start IS NULL OR window_5h_start + INTERVAL '5 hours' <= NOW() THEN 0 ELSE usage_5h END)
				+ CASE WHEN rate_limit_5h > 0 THEN $2 ELSE 0 END,
			usage_1d = (CASE WHEN window_1d_start IS NULL OR window_1d_start + INTERVAL '24 hours' <= NOW() THEN 0 ELSE usage_1d END)
				+ CASE WHEN rate_limit_1d > 0 THEN $2 ELSE 0 END,
			usage_7d = (CASE WHEN window_7d_start IS NULL OR window_7d_start + INTERVAL '7 days' <= NOW() THEN 0 ELSE usage_7d END)
				+ CASE WHEN rate_limit_7d > 0 THEN $2 ELSE 0 END,
			window_5h_start = CASE WHEN window_5h_start IS NULL OR window_5h_start + INTERVAL '5 hours' <= NOW() THEN NOW() ELSE window_5h_start END,
			window_1d_start = CASE WHEN window_1d_start IS NULL OR window_1d_start + INTERVAL '24 hours' <= NOW() THEN date_trunc('day', NOW()) ELSE window_1d_start END,
			window_7d_start = CASE WHEN window_7d_start IS NULL OR window_7d_start + INTERVAL '7 days' <= NOW() THEN date_trunc('day', NOW()) ELSE window_7d_start END,
			reserved_quota_usd = reserved_quota_usd - $1,
			reserved_usage_5h_usd = reserved_usage_5h_usd - $1,
			reserved_usage_1d_usd = reserved_usage_1d_usd - $1,
			reserved_usage_7d_usd = reserved_usage_7d_usd - $1,
			status = CASE WHEN status = $3 AND quota > 0 AND quota_used + $2 >= quota THEN $4 ELSE status END,
			updated_at = NOW()
		WHERE id = $5
		  AND reserved_quota_usd >= $1
		  AND reserved_usage_5h_usd >= $1
		  AND reserved_usage_1d_usd >= $1
		  AND reserved_usage_7d_usd >= $1
		RETURNING reserved_quota_usd, quota_used,
			reserved_usage_5h_usd, reserved_usage_1d_usd, reserved_usage_7d_usd
	`, held, actual, service.StatusAPIKeyActive, service.StatusAPIKeyQuotaExhausted, apiKeyID).
		Scan(&reservedQuota, &quotaUsed, &reserved5h, &reserved1d, &reserved7d)
	if errors.Is(err, sql.ErrNoRows) {
		return reservationFinancialSnapshot{}, errors.New("usage reservation api key hold invariant violated")
	}
	if err != nil {
		return reservationFinancialSnapshot{}, err
	}
	return reservationFinancialSnapshot{
		apiKeyReservedQuota: &reservedQuota, apiKeyQuotaUsed: &quotaUsed,
		apiKeyReserved5h: &reserved5h, apiKeyReserved1d: &reserved1d, apiKeyReserved7d: &reserved7d,
	}, nil
}

func releaseUsageAPIKeyConstraints(ctx context.Context, tx *sql.Tx, apiKeyID int64, held decimal.Decimal) (reservationFinancialSnapshot, error) {
	var reservedQuota, quotaUsed, reserved5h, reserved1d, reserved7d decimal.Decimal
	err := tx.QueryRowContext(ctx, `
		UPDATE api_keys
		SET reserved_quota_usd = reserved_quota_usd - $1,
			reserved_usage_5h_usd = reserved_usage_5h_usd - $1,
			reserved_usage_1d_usd = reserved_usage_1d_usd - $1,
			reserved_usage_7d_usd = reserved_usage_7d_usd - $1,
			updated_at = NOW()
		WHERE id = $2
		  AND reserved_quota_usd >= $1
		  AND reserved_usage_5h_usd >= $1
		  AND reserved_usage_1d_usd >= $1
		  AND reserved_usage_7d_usd >= $1
		RETURNING reserved_quota_usd, quota_used,
			reserved_usage_5h_usd, reserved_usage_1d_usd, reserved_usage_7d_usd
	`, held, apiKeyID).Scan(&reservedQuota, &quotaUsed, &reserved5h, &reserved1d, &reserved7d)
	if errors.Is(err, sql.ErrNoRows) {
		return reservationFinancialSnapshot{}, errors.New("usage reservation api key hold invariant violated")
	}
	if err != nil {
		return reservationFinancialSnapshot{}, err
	}
	return reservationFinancialSnapshot{
		apiKeyReservedQuota: &reservedQuota, apiKeyQuotaUsed: &quotaUsed,
		apiKeyReserved5h: &reserved5h, apiKeyReserved1d: &reserved1d, apiKeyReserved7d: &reserved7d,
	}, nil
}

func classifyAPIKeyReservationFailure(ctx context.Context, tx *sql.Tx, userID, apiKeyID int64, amount decimal.Decimal) error {
	var available, quotaBlocked, rateBlocked bool
	err := tx.QueryRowContext(ctx, `
		SELECT
			user_id = $2 AND deleted_at IS NULL AND status = $3 AND (expires_at IS NULL OR expires_at > NOW()),
			quota > 0 AND quota_used + reserved_quota_usd + $4 > quota,
			(rate_limit_5h > 0 AND
			 (CASE WHEN window_5h_start IS NULL OR window_5h_start + INTERVAL '5 hours' <= NOW() THEN 0 ELSE usage_5h END)
			 + reserved_usage_5h_usd + $4 > rate_limit_5h)
			OR (rate_limit_1d > 0 AND
			 (CASE WHEN window_1d_start IS NULL OR window_1d_start + INTERVAL '24 hours' <= NOW() THEN 0 ELSE usage_1d END)
			 + reserved_usage_1d_usd + $4 > rate_limit_1d)
			OR (rate_limit_7d > 0 AND
			 (CASE WHEN window_7d_start IS NULL OR window_7d_start + INTERVAL '7 days' <= NOW() THEN 0 ELSE usage_7d END)
			 + reserved_usage_7d_usd + $4 > rate_limit_7d)
		FROM api_keys
		WHERE id = $1
		FOR UPDATE
	`, apiKeyID, userID, service.StatusAPIKeyActive, amount).Scan(&available, &quotaBlocked, &rateBlocked)
	if errors.Is(err, sql.ErrNoRows) {
		return service.ErrUsageReservationAPIKeyUnavailable
	}
	if err != nil {
		return err
	}
	if !available {
		return service.ErrUsageReservationAPIKeyUnavailable
	}
	if quotaBlocked {
		return service.ErrUsageReservationAPIKeyQuota
	}
	if rateBlocked {
		return service.ErrUsageReservationAPIKeyRateLimit
	}
	return service.ErrUsageReservationAPIKeyUnavailable
}

func markUsageReservationCaptured(
	ctx context.Context,
	tx *sql.Tx,
	reservation *service.UsageBillingReservation,
	cmd *service.UsageReservationCaptureCommand,
	settlement service.AdaptiveManagementFeeSettlement,
) (*service.UsageBillingReservation, error) {
	updated, err := scanUsageReservation(tx.QueryRowContext(ctx, `
		UPDATE usage_billing_reservations
		SET status = 'captured',
			uncapped_base_cost = $1,
			captured_base_cost = $2,
			captured_management_fee = $3,
			captured_total = $4,
			platform_overage_base_cost = $5,
			winning_leaf_group_id = $6,
			winning_attempt_no = $7,
			usage_log_id = $8,
			usage_log_created_at = $9,
			usage_evidence_hash = $10,
			active_leaf_group_id = NULL,
			active_attempt_no = NULL,
			attempt_started_at = NULL,
			captured_at = clock_timestamp(),
			reconcile_from_status = NULL,
			reconciliation_lease_expires_at = NULL,
			row_version = row_version + 1,
			updated_at = clock_timestamp()
		WHERE id = $11
		  AND owner_id = $12
		  AND lease_epoch = $13
		  AND row_version = $14
		  AND (
			(status = 'in_flight' AND lease_expires_at > clock_timestamp())
			OR (status = 'reconciling' AND reconciliation_lease_expires_at > clock_timestamp())
		  )
		RETURNING `+usageReservationSelectColumns,
		settlement.UncappedBaseAmount,
		settlement.CustomerCharge.BaseAmount, settlement.CustomerCharge.FeeAmount,
		settlement.CustomerCharge.CaptureAmount, settlement.PlatformOverageBaseAmount,
		cmd.WinningLeafGroupID, cmd.AttemptNo, cmd.UsageLogID, cmd.UsageLogCreatedAt, cmd.EvidenceHash, reservation.ID,
		cmd.OwnerID, cmd.FencingToken, cmd.RowVersion,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrUsageReservationLeaseExpired
	}
	return updated, err
}

func markUsageReservationReleased(
	ctx context.Context,
	tx *sql.Tx,
	reservation *service.UsageBillingReservation,
	cmd *service.UsageReservationReleaseCommand,
) (*service.UsageBillingReservation, error) {
	updated, err := scanUsageReservation(tx.QueryRowContext(ctx, `
		UPDATE usage_billing_reservations
		SET status = 'released',
			released_at = clock_timestamp(),
			release_reason = NULLIF($1, ''),
			reconcile_from_status = NULL,
			reconciliation_lease_expires_at = NULL,
			row_version = row_version + 1,
			updated_at = clock_timestamp()
		WHERE id = $2
		  AND owner_id = $3
		  AND lease_epoch = $4
		  AND row_version = $5
		  AND (
			(status = 'authorized' AND lease_expires_at > clock_timestamp())
			OR (status = 'reconciling' AND reconciliation_lease_expires_at > clock_timestamp())
		  )
		RETURNING `+usageReservationSelectColumns,
		cmd.Reason, reservation.ID, cmd.OwnerID, cmd.FencingToken, cmd.RowVersion,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrUsageReservationLeaseExpired
	}
	return updated, err
}

func markUsageReservationRenewed(
	ctx context.Context,
	tx *sql.Tx,
	reservation *service.UsageBillingReservation,
	cmd *service.UsageReservationRenewCommand,
	estimatedBase, heldBase, heldFee, heldTotal decimal.Decimal,
) (*service.UsageBillingReservation, error) {
	updated, err := scanUsageReservation(tx.QueryRowContext(ctx, `
		UPDATE usage_billing_reservations
		SET estimated_base_cost = $1,
			held_base_cost = $2,
			held_management_fee = $3,
			held_total = $4,
			lease_expires_at = clock_timestamp() + ($5 * INTERVAL '1 second'),
			row_version = row_version + 1,
			updated_at = NOW()
		WHERE id = $6
		  AND status IN ('authorized', 'in_flight')
		  AND owner_id = $7
		  AND lease_epoch = $8
		  AND row_version = $9
		  AND lease_expires_at > clock_timestamp()
		RETURNING `+usageReservationSelectColumns,
		estimatedBase, heldBase, heldFee, heldTotal, int64(cmd.LeaseTTL/time.Second),
		reservation.ID, cmd.OwnerID, cmd.FencingToken, cmd.RowVersion,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrUsageReservationLeaseExpired
	}
	return updated, err
}

func replayUsageReservationOperation(
	ctx context.Context,
	tx *sql.Tx,
	reservation *service.UsageBillingReservation,
	operationKey, fingerprint, operation, ownerID string,
	fence, baseRowVersion int64,
) (*service.UsageReservationResult, error) {
	keyHash := service.HashUsageReservationKey(operationKey)
	var storedOperation, storedFingerprint string
	var componentCount int
	var storedFence, storedRowVersion int64
	err := tx.QueryRowContext(ctx, `
		SELECT MIN(operation), MIN(operation_fingerprint), COUNT(*),
			MIN(lease_epoch), MIN(row_version)
		FROM usage_billing_ledger
		WHERE reservation_id = $1 AND operation_key_hash = $2
		GROUP BY reservation_id, operation_key_hash
	`, reservation.ID, keyHash).Scan(
		&storedOperation, &storedFingerprint, &componentCount, &storedFence, &storedRowVersion,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if componentCount != 2 {
		return nil, errors.New("usage reservation ledger component invariant violated")
	}
	if strings.TrimSpace(storedOperation) != operation || strings.TrimSpace(storedFingerprint) != strings.TrimSpace(fingerprint) {
		return nil, service.ErrUsageReservationFingerprintConflict
	}
	if storedFence != fence {
		return nil, service.ErrUsageReservationFenceConflict
	}
	if storedRowVersion != baseRowVersion+1 {
		return nil, service.ErrUsageReservationVersionConflict
	}
	allowedStatuses := []string{}
	requireLiveLease := false
	switch operation {
	case service.UsageReservationOperationCapture:
		allowedStatuses = []string{service.UsageReservationStatusCaptured}
	case service.UsageReservationOperationRelease:
		allowedStatuses = []string{service.UsageReservationStatusReleased}
	case service.UsageReservationOperationRenew:
		allowedStatuses = []string{service.UsageReservationStatusAuthorized, service.UsageReservationStatusInFlight}
		requireLiveLease = true
	default:
		return nil, service.ErrUsageReservationInvalid
	}
	if err := validateUsageReservationReplay(ctx, tx, reservation, ownerID, fence, baseRowVersion,
		requireLiveLease, allowedStatuses...); err != nil {
		return nil, err
	}

	var available, adaptiveReserved, subReserved, subDaily, subWeekly, subMonthly decimal.NullDecimal
	var apiReserved, apiUsed, api5h, api1d, api7d decimal.NullDecimal
	err = tx.QueryRowContext(ctx, `
		SELECT available_balance_after, adaptive_reserved_balance_after,
			subscription_reserved_after, subscription_daily_usage_after,
			subscription_weekly_usage_after, subscription_monthly_usage_after,
			api_key_reserved_quota_after, api_key_quota_used_after,
			api_key_reserved_5h_after, api_key_reserved_1d_after, api_key_reserved_7d_after
		FROM usage_billing_ledger
		WHERE reservation_id = $1 AND operation_key_hash = $2 AND component = 'base'
	`, reservation.ID, keyHash).Scan(
		&available, &adaptiveReserved, &subReserved, &subDaily, &subWeekly, &subMonthly,
		&apiReserved, &apiUsed, &api5h, &api1d, &api7d,
	)
	if err != nil {
		return nil, err
	}
	snapshot := reservationFinancialSnapshot{
		availableBalance: nullDecimalPointer(available), adaptiveReservedBalance: nullDecimalPointer(adaptiveReserved),
		subscriptionReserved: nullDecimalPointer(subReserved), subscriptionDailyUsage: nullDecimalPointer(subDaily),
		subscriptionWeeklyUsage: nullDecimalPointer(subWeekly), subscriptionMonthlyUsage: nullDecimalPointer(subMonthly),
		apiKeyReservedQuota: nullDecimalPointer(apiReserved), apiKeyQuotaUsed: nullDecimalPointer(apiUsed),
		apiKeyReserved5h: nullDecimalPointer(api5h), apiKeyReserved1d: nullDecimalPointer(api1d),
		apiKeyReserved7d: nullDecimalPointer(api7d),
	}
	return snapshot.result(reservation, false), nil
}

func insertUsageReservationLedgerPair(
	ctx context.Context,
	tx *sql.Tx,
	reservation *service.UsageBillingReservation,
	operation, operationKeyHash, fingerprint string,
	baseAmount, feeAmount decimal.Decimal,
	baseHoldDelta, feeHoldDelta decimal.Decimal,
	baseCaptureDelta, feeCaptureDelta decimal.Decimal,
	snapshot reservationFinancialSnapshot,
) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO usage_billing_ledger (
			reservation_id, operation, component, operation_key_hash,
			operation_fingerprint, funding_source, amount, hold_delta, capture_delta,
			lease_epoch, row_version, available_balance_after, adaptive_reserved_balance_after,
			subscription_reserved_after, subscription_daily_usage_after,
			subscription_weekly_usage_after, subscription_monthly_usage_after,
			api_key_reserved_quota_after, api_key_quota_used_after,
			api_key_reserved_5h_after, api_key_reserved_1d_after, api_key_reserved_7d_after
		)
		SELECT $1, $2, component, $3, $4, $5, amount, hold_delta, capture_delta,
			$6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18
		FROM (VALUES
			('base', $19::numeric, $20::numeric, $21::numeric),
			('management_fee', $22::numeric, $23::numeric, $24::numeric)
		) AS entries(component, amount, hold_delta, capture_delta)
	`, reservation.ID, operation, operationKeyHash, fingerprint, reservation.FundingSource,
		reservation.FencingToken, reservation.RowVersion,
		nullableDecimalValue(snapshot.availableBalance), nullableDecimalValue(snapshot.adaptiveReservedBalance),
		nullableDecimalValue(snapshot.subscriptionReserved), nullableDecimalValue(snapshot.subscriptionDailyUsage),
		nullableDecimalValue(snapshot.subscriptionWeeklyUsage), nullableDecimalValue(snapshot.subscriptionMonthlyUsage),
		nullableDecimalValue(snapshot.apiKeyReservedQuota), nullableDecimalValue(snapshot.apiKeyQuotaUsed),
		nullableDecimalValue(snapshot.apiKeyReserved5h), nullableDecimalValue(snapshot.apiKeyReserved1d),
		nullableDecimalValue(snapshot.apiKeyReserved7d),
		baseAmount, baseHoldDelta, baseCaptureDelta,
		feeAmount, feeHoldDelta, feeCaptureDelta,
	)
	return err
}

func nullDecimalPointer(value decimal.NullDecimal) *decimal.Decimal {
	if !value.Valid {
		return nil
	}
	copy := value.Decimal
	return &copy
}

func nullableDecimalValue(value *decimal.Decimal) any {
	if value == nil {
		return nil
	}
	return *value
}
