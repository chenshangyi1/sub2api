package service

import (
	"context"
	"errors"
	"fmt"
)

var ErrAdaptivePoolNotFound = errors.New("adaptive pool not found")

// AdaptiveLeafRef is a configured leaf group in an Adaptive parent pool.
// Model capability and pricing are intentionally resolved from the leaf at
// planning time so this relation never becomes a second model catalog.
type AdaptiveLeafRef struct {
	LeafGroupID int64
	Enabled     bool
	SortOrder   int
}

// AdaptivePoolSnapshot is the durable topology input to model-first routing.
// ConfigGeneration is monotonic and must be frozen into each route plan.
type AdaptivePoolSnapshot struct {
	ParentGroupID    int64
	Platform         string
	Enabled          bool
	ConfigGeneration int64
	// UseManualIntelligenceOrder when true: intelligence mode ranks by
	// membership sort_order (admin calibration). When false (default):
	// intelligence ranks by rate_multiplier descending (高价=高智力).
	UseManualIntelligenceOrder bool
	// AllowHybrid lets leaf groups span platforms. Parent platform is plan
	// identity only; runtime capability checks remain authoritative.
	AllowHybrid bool
	Members     []AdaptiveLeafRef
}

// AdaptivePoolSnapshotRepository exposes the narrow read path needed by the
// gateway planner.
type AdaptivePoolSnapshotRepository interface {
	GetAdaptivePoolSnapshot(ctx context.Context, parentGroupID int64) (*AdaptivePoolSnapshot, error)
}

// AdaptivePoolAdminRepository owns topology replacement. Keeping it separate
// prevents the gateway from acquiring a configuration write capability.
type AdaptivePoolAdminRepository interface {
	AdaptivePoolSnapshotRepository
	ListAdaptivePoolSnapshots(ctx context.Context) ([]AdaptivePoolSnapshot, error)
	ReplaceAdaptivePool(ctx context.Context, input AdaptivePoolUpdate) (*AdaptivePoolSnapshot, error)
	DeleteAdaptivePool(ctx context.Context, parentGroupID int64) error
}

type AdaptivePoolUpdate struct {
	ParentGroupID int64
	Enabled       bool
	AllowHybrid   bool
	Members       []AdaptiveLeafRef
}

func (u AdaptivePoolUpdate) Validate() error {
	if u.ParentGroupID <= 0 {
		return errors.New("adaptive parent group id must be positive")
	}
	if u.Enabled && len(u.Members) == 0 {
		return errors.New("enabled adaptive pool requires at least one leaf group")
	}
	seen := make(map[int64]struct{}, len(u.Members))
	hasEnabled := false
	for _, member := range u.Members {
		if member.LeafGroupID <= 0 {
			return errors.New("adaptive leaf group id must be positive")
		}
		if member.LeafGroupID == u.ParentGroupID {
			return errors.New("adaptive parent group cannot reference itself")
		}
		if _, exists := seen[member.LeafGroupID]; exists {
			return fmt.Errorf("duplicate adaptive leaf group id: %d", member.LeafGroupID)
		}
		seen[member.LeafGroupID] = struct{}{}
		hasEnabled = hasEnabled || member.Enabled
	}
	if u.Enabled && !hasEnabled {
		return errors.New("enabled adaptive pool requires an enabled leaf group")
	}
	return nil
}
