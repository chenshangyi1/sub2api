package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type adaptiveReconciliationEvidenceRepository struct {
	db *sql.DB
}

func NewAdaptiveReconciliationEvidenceRepository(db *sql.DB) service.AdaptiveReconciliationEvidenceRepository {
	return &adaptiveReconciliationEvidenceRepository{db: db}
}

func (r *adaptiveReconciliationEvidenceRepository) Inspect(
	ctx context.Context,
	reservationID string,
) (*service.AdaptiveReconciliationEvidenceSnapshot, error) {
	if r == nil || r.db == nil || strings.TrimSpace(reservationID) == "" {
		return nil, service.ErrAdaptiveReconciliationEvidenceInvalid
	}
	snapshot := &service.AdaptiveReconciliationEvidenceSnapshot{}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, created_at, adaptive_reservation_id::text, user_id, api_key_id,
			subscription_id, billing_type, adaptive_parent_group_id, routed_group_id,
			adaptive_attempt_no, adaptive_pricing_snapshot_id, adaptive_evidence_hash,
			adaptive_base_cost, adaptive_management_fee_cost, adaptive_total_cost,
			adaptive_uncapped_base_cost, adaptive_platform_overage_cost
		FROM usage_logs
		WHERE adaptive_reservation_id = $1
		  AND adaptive_settlement_status = 'pending'
		ORDER BY created_at, id
		LIMIT 2
	`, strings.TrimSpace(reservationID))
	if err != nil {
		return nil, fmt.Errorf("query pending adaptive usage evidence: %w", err)
	}
	for rows.Next() {
		if snapshot.PendingUsage != nil {
			_ = rows.Close()
			return nil, service.ErrAdaptiveReconciliationEvidenceInvalid
		}
		evidence := &service.AdaptivePendingUsageEvidence{Success: true}
		var subscriptionID, parentGroupID sql.NullInt64
		if err := rows.Scan(
			&evidence.UsageLogID,
			&evidence.UsageLogCreatedAt,
			&evidence.ReservationID,
			&evidence.UserID,
			&evidence.APIKeyID,
			&subscriptionID,
			&evidence.BillingType,
			&parentGroupID,
			&evidence.LeafGroupID,
			&evidence.AttemptNo,
			&evidence.PricingSnapshotID,
			&evidence.EvidenceHash,
			&evidence.BaseCost,
			&evidence.ManagementFeeCost,
			&evidence.TotalCost,
			&evidence.UncappedBaseCost,
			&evidence.PlatformOverageBaseCost,
		); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan pending adaptive usage evidence: %w", err)
		}
		if subscriptionID.Valid {
			value := subscriptionID.Int64
			evidence.SubscriptionID = &value
		}
		if parentGroupID.Valid {
			value := parentGroupID.Int64
			evidence.ParentGroupID = &value
		}
		snapshot.PendingUsage = evidence
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("iterate pending adaptive usage evidence: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close pending adaptive usage evidence: %w", err)
	}

	attemptRows, err := r.db.QueryContext(ctx, `
		SELECT status, failure_evidence_hash
		FROM usage_billing_attempts
		WHERE reservation_id = $1
		ORDER BY attempt_no
	`, strings.TrimSpace(reservationID))
	if err != nil {
		return nil, fmt.Errorf("query adaptive attempt evidence: %w", err)
	}
	defer attemptRows.Close()
	for attemptRows.Next() {
		var status string
		var failureEvidence sql.NullString
		if err := attemptRows.Scan(&status, &failureEvidence); err != nil {
			return nil, fmt.Errorf("scan adaptive attempt evidence: %w", err)
		}
		snapshot.AttemptCount++
		switch strings.TrimSpace(status) {
		case "started":
			snapshot.StartedAttemptCount++
		case "succeeded":
			snapshot.SucceededAttemptCount++
		case "failed":
			snapshot.FailedAttemptCount++
			if failureEvidence.Valid {
				snapshot.LatestFailedEvidenceHash = strings.TrimSpace(failureEvidence.String)
			}
		default:
			return nil, service.ErrAdaptiveReconciliationEvidenceInvalid
		}
	}
	if err := attemptRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate adaptive attempt evidence: %w", err)
	}
	return snapshot, nil
}

var _ service.AdaptiveReconciliationEvidenceRepository = (*adaptiveReconciliationEvidenceRepository)(nil)
