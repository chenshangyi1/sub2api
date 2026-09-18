package handler

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

const ginKeyGatewayAdaptiveSession = "gateway_adaptive_session"

// SetAdaptivePlanner attaches Adaptive route planner. Leaf groups may span
// platforms; the parent row's platform is only plan identity, not a filter.
func (h *GatewayHandler) SetAdaptivePlanner(planner *service.AdaptiveRoutePlanner) {
	if h != nil {
		h.adaptivePlanner = planner
	}
}

// gatewayAdaptiveSession drives leaf-group order for Anthropic/Gemini Adaptive parents.
// When Billing is set, HOLD replaces v247 text preauth and capture runs on RecordUsage.
type gatewayAdaptiveSession struct {
	ParentGroupID  int64
	Plan           *service.AdaptiveRoutePlan
	Billing        *service.AdaptiveBillingContext
	LeafGroupIDs   []int64
	LeafIndex      int
	CanonicalModel string
}

func getGatewayAdaptiveSession(c *gin.Context) *gatewayAdaptiveSession {
	if c == nil {
		return nil
	}
	v, ok := c.Get(ginKeyGatewayAdaptiveSession)
	if !ok || v == nil {
		return nil
	}
	s, _ := v.(*gatewayAdaptiveSession)
	return s
}

// startGatewayAdaptiveIfParent plans Adaptive leaves when the key's group is an
// Adaptive parent. Leaves may be OpenAI / Anthropic / Gemini / CN. Returns the
// API key bound to the first usable leaf (or original key if not Adaptive).
func (h *GatewayHandler) startGatewayAdaptiveIfParent(
	ctx context.Context,
	c *gin.Context,
	apiKey *service.APIKey,
	requestedModel string,
	protocol service.AdaptiveRouteProtocol,
	body []byte,
	reqLog *zap.Logger,
) *service.APIKey {
	if h == nil || apiKey == nil || apiKey.GroupID == nil || apiKey.User == nil {
		return apiKey
	}
	if h.cfg == nil || !h.cfg.Gateway.AdaptiveRoutingEnabled {
		return apiKey
	}
	if h.adaptivePlanner == nil {
		return apiKey
	}
	platform := ""
	if apiKey.Group != nil && apiKey.Group.Platform != service.PlatformAdaptive {
		platform = apiKey.Group.Platform
	}
	if platform == "" {
		platform = service.PlatformOpenAI
	}

	routeMode := service.AdaptiveRouteModeFromPreference(apiKey.AdaptiveRoutingPreference)
	allowedGroupIDs := map[int64]struct{}{}
	if apiKey.User != nil {
		allowedGroupIDs, _ = h.apiKeyService.GetUserAllowedGroupIDSet(ctx, apiKey.User.ID)
	}
	plan, err := h.adaptivePlanner.Plan(ctx, service.AdaptiveRouteRequest{
		ParentGroupID:       *apiKey.GroupID,
		UserID:              apiKey.User.ID,
		UserIsAdmin:         apiKey.User.IsAdmin(),
		AllowedGroupIDs:     allowedGroupIDs,
		Platform:            platform,
		RequestedModel:      requestedModel,
		Mode:                routeMode,
		MaxRateMultiplier:   apiKey.AdaptiveMaxRateMultiplier,
		AllowedLeafGroupIDs: service.AdaptiveLeafAllowlist(apiKey.AdaptiveLeafGroupIDs),
		Protocol:            protocol,
	})
	if err != nil {
		if errors.Is(err, service.ErrAdaptivePoolNotFound) || errors.Is(err, service.ErrAdaptivePoolDisabled) {
			return apiKey
		}
		if reqLog != nil {
			reqLog.Warn("gateway.adaptive_plan_failed",
				zap.Int64("parent_group_id", *apiKey.GroupID),
				zap.String("platform", platform),
				zap.Error(err),
			)
		}
		return apiKey
	}
	if plan == nil || plan.CandidateCount() == 0 {
		return apiKey
	}

	leafIDs := make([]int64, 0, plan.CandidateCount())
	var firstKey *service.APIKey
	for _, cand := range plan.Candidates() {
		group, gerr := h.apiKeyService.ResolveGroupByID(ctx, cand.LeafGroupID)
		if gerr != nil || group == nil {
			continue
		}
		leafIDs = append(leafIDs, group.ID)
		if firstKey == nil {
			firstKey = cloneAPIKeyWithGroup(apiKey, group)
		}
	}
	if len(leafIDs) == 0 || firstKey == nil {
		return apiKey
	}

	session := &gatewayAdaptiveSession{
		ParentGroupID:  *apiKey.GroupID,
		Plan:           plan,
		LeafGroupIDs:   leafIDs,
		LeafIndex:      0,
		CanonicalModel: plan.CanonicalModel,
	}
	if billing, berr := h.authorizeGatewayAdaptive(ctx, apiKey, plan, requestedModel, body, reqLog); berr != nil {
		if reqLog != nil {
			reqLog.Warn("gateway.adaptive_authorize_failed",
				zap.Int64("parent_group_id", *apiKey.GroupID),
				zap.Error(berr),
			)
		}
		return apiKey
	} else {
		session.Billing = billing
		if billing != nil && len(leafIDs) > 0 {
			h.freezeGatewayAdaptiveLeafUserMultiplier(ctx, session, leafIDs[0])
		}
	}
	if c != nil {
		c.Set(ginKeyGatewayAdaptiveSession, session)
	}
	if reqLog != nil {
		reqLog.Info("gateway.adaptive_plan_ready",
			zap.Int64("parent_group_id", session.ParentGroupID),
			zap.String("platform", platform),
			zap.String("model", session.CanonicalModel),
			zap.String("route_mode", string(plan.Mode)),
			zap.Int64s("leaf_order", leafIDs),
			zap.String("anti_stall_tier", apiKey.AntiStallTier),
		)
	}
	return firstKey
}

// advanceGatewayAdaptiveLeaf switches to the next Adaptive leaf group.
// Returns the new leaf-bound API key, or nil if no more leaves.
func (h *GatewayHandler) advanceGatewayAdaptiveLeaf(
	ctx context.Context,
	c *gin.Context,
	rootKey *service.APIKey,
	reqLog *zap.Logger,
) *service.APIKey {
	session := getGatewayAdaptiveSession(c)
	if session == nil || rootKey == nil || h == nil || h.apiKeyService == nil {
		return nil
	}
	next := session.LeafIndex + 1
	if next >= len(session.LeafGroupIDs) {
		return nil
	}
	// Anti-Stall leaf budget
	if anti := service.AntiStallSessionFromGin(c); anti != nil && anti.Config().Enabled {
		if anti.LeafSwitches() >= anti.Config().MaxLeafSwitches {
			return nil
		}
		anti.RecordLeafSwitch()
		service.EnsureAntiStallDripRunning(c, anti)
	}
	session.LeafIndex = next
	gid := session.LeafGroupIDs[next]
	group, err := h.apiKeyService.ResolveGroupByID(ctx, gid)
	if err != nil || group == nil {
		if reqLog != nil {
			reqLog.Warn("gateway.adaptive_resolve_leaf_failed", zap.Int64("leaf_group_id", gid), zap.Error(err))
		}
		return nil
	}
	if reqLog != nil {
		reqLog.Warn("gateway.adaptive_leaf_switch",
			zap.Int64("parent_group_id", session.ParentGroupID),
			zap.Int64("leaf_group_id", gid),
			zap.Int("leaf_index", next),
		)
	}
	return cloneAPIKeyWithGroup(rootKey, group)
}

func gatewayAdaptiveHoldActive(c *gin.Context) bool {
	session := getGatewayAdaptiveSession(c)
	return session != nil && session.Billing != nil && !session.Billing.Probe
}

func gatewayAdaptiveBilling(c *gin.Context) *service.AdaptiveBillingContext {
	session := getGatewayAdaptiveSession(c)
	if session == nil {
		return nil
	}
	return session.Billing
}

func (h *GatewayHandler) authorizeGatewayAdaptive(
	ctx context.Context,
	apiKey *service.APIKey,
	plan *service.AdaptiveRoutePlan,
	requestedModel string,
	body []byte,
	reqLog *zap.Logger,
) (*service.AdaptiveBillingContext, error) {
	if h == nil || h.adaptiveBilling == nil || apiKey == nil || apiKey.User == nil || plan == nil {
		return nil, nil
	}
	var resolveUserRate func(context.Context, int64, int64, float64) float64
	if h.gatewayService != nil {
		resolveUserRate = h.gatewayService.ResolveUserGroupRateMultiplier
	}
	maxRate := adaptiveHoldRate(ctx, plan.Candidates(), apiKey.User.ID, resolveUserRate)
	estimatedBase := decimal.NewFromFloat(adaptiveV1HoldBaseUSD * maxRate).Round(service.AdaptiveBillingMoneyScale)
	if estimatedBase.IsNegative() || estimatedBase.IsZero() {
		estimatedBase = decimal.NewFromFloat(adaptiveV1HoldBaseUSD)
	}
	if h.gatewayService != nil {
		model := plan.CanonicalModel
		if model == "" {
			model = requestedModel
		}
		pricingAt := service.GatewayTokenRequestPricingAtFromContext(ctx)
		if pricingAt.IsZero() {
			pricingAt = time.Now()
		}
		if priced, err := h.gatewayService.EstimateAdaptiveHoldBase(
			ctx, apiKey, body, model, pricingAt,
			gjson.GetBytes(body, "service_tier").String(), maxRate,
		); err != nil {
			if reqLog != nil {
				reqLog.Warn("gateway.adaptive_hold_estimate_fallback", zap.Error(err))
			}
		} else if priced.IsPositive() {
			estimatedBase = priced
		}
	}
	parentID := plan.ParentGroupID
	logicalID := uuid.NewString()
	ownerID, _ := os.Hostname()
	if ownerID == "" {
		ownerID = "sub2api"
	}
	billing, _, err := h.adaptiveBilling.Authorize(ctx, &service.UsageReservationReserveCommand{
		ReservationID:       uuid.NewString(),
		IdempotencyKey:      "adaptive:" + logicalID,
		LogicalRequestID:    logicalID,
		OwnerID:             ownerID,
		UserID:              apiKey.User.ID,
		APIKeyID:            apiKey.ID,
		ParentGroupID:       &parentID,
		CanonicalModel:      plan.CanonicalModel,
		PricingSnapshotID:   plan.PricingSnapshotID,
		PricingGeneration:   plan.PricingGeneration,
		ConfigGeneration:    plan.ConfigGeneration,
		FundingSource:       service.UsageReservationFundingBalance,
		EstimatedBaseCost:   estimatedBase,
		ManagementFeeBPS:    h.gatewayAdaptiveServiceFeeBPS(ctx),
		ManagementFeeBPSSet: true,
		LeaseTTL:            2 * time.Minute,
	})
	if err != nil {
		return nil, err
	}
	billing.UserID = apiKey.User.ID
	billing.ManagementFeeBPSSet = true
	if reqLog != nil {
		reqLog.Info("gateway.adaptive_authorized",
			zap.String("reservation_id", billing.ReservationID),
			zap.String("held_total", billing.HeldTotal.String()),
			zap.String("estimated_base", estimatedBase.String()),
		)
	}
	return billing, nil
}

func (h *GatewayHandler) freezeGatewayAdaptiveLeafUserMultiplier(ctx context.Context, session *gatewayAdaptiveSession, leafGroupID int64) {
	if h == nil || session == nil || session.Billing == nil || session.Plan == nil || leafGroupID <= 0 {
		return
	}
	leafRate := -1.0
	for _, candidate := range session.Plan.Candidates() {
		if candidate.LeafGroupID == leafGroupID {
			leafRate = candidate.FrozenRateMultiplier
			break
		}
	}
	if leafRate < 0 {
		return
	}
	effective := leafRate
	if h.gatewayService != nil && session.Billing.UserID > 0 {
		effective = h.gatewayService.ResolveUserGroupRateMultiplier(ctx, session.Billing.UserID, leafGroupID, leafRate)
	}
	if effective < 0 {
		return
	}
	session.Billing.UserRateMultiplier = effective
	session.Billing.UserRateMultiplierSet = true
}

func (h *GatewayHandler) gatewayAdaptiveServiceFeeBPS(ctx context.Context) int32 {
	if h == nil || h.settingService == nil {
		return service.DefaultAdaptiveManagementFeeBPS
	}
	return h.settingService.GetAdaptiveServiceFeeBPS(ctx)
}
