package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
)

// AdaptiveRouteMaxCandidates is the max number of leaf groups executed for one
// logical request (ordered plan). Product target is 4–5 physical leaves under
// one Adaptive parent; keep this in sync with AdaptiveMaxLeafAttempts and DB checks.
const AdaptiveRouteMaxCandidates = 5

// AdaptiveMaxLeafAttempts is the inclusive upper bound for attempt_no on
// reservations, usage evidence, and MarkInFlight. Must equal MaxCandidates.
const AdaptiveMaxLeafAttempts = AdaptiveRouteMaxCandidates

var (
	ErrAdaptiveRouteInvalid         = errors.New("adaptive route request is invalid")
	ErrAdaptiveRouteUnavailable     = errors.New("adaptive route planner is unavailable")
	ErrAdaptiveModelUnavailable     = errors.New("adaptive model is unavailable")
	ErrAdaptivePoolDisabled         = errors.New("adaptive pool is disabled")
	ErrAdaptivePoolPlatformMismatch = errors.New("adaptive pool platform mismatch")
)

type AdaptiveRouteMode string

const (
	AdaptiveRouteModeIntelligence AdaptiveRouteMode = "intelligence"
	AdaptiveRouteModePrice        AdaptiveRouteMode = "price"
)

type AdaptiveRouteProtocol string

const (
	AdaptiveRouteProtocolAnthropicMessages AdaptiveRouteProtocol = "anthropic_messages"
	AdaptiveRouteProtocolOpenAIResponses   AdaptiveRouteProtocol = "openai_responses"
	AdaptiveRouteProtocolOpenAIChat        AdaptiveRouteProtocol = "openai_chat_completions"
	AdaptiveRouteProtocolOpenAIMessages    AdaptiveRouteProtocol = "openai_messages"
	AdaptiveRouteProtocolOpenAIWebSocket   AdaptiveRouteProtocol = "openai_websocket"
	AdaptiveRouteProtocolGeminiGenerate    AdaptiveRouteProtocol = "gemini_generate_content"
)

// AdaptiveRouteRequest contains only request-stable inputs. Plan evaluates it
// once; fallback execution must consume the returned candidates in order and
// must not call Plan again for the same logical request.
type AdaptiveRouteRequest struct {
	ParentGroupID   int64
	UserID          int64
	UserIsAdmin     bool
	AllowedGroupIDs map[int64]struct{}
	Platform        string
	RequestedModel  string
	Mode            AdaptiveRouteMode
	// UseManualIntelligenceOrder ranks intelligence mode by membership sort_order
	// (admin calibration) instead of rate_multiplier descending.
	UseManualIntelligenceOrder bool
	// MaxRateMultiplier when set excludes leaves with FrozenRateMultiplier above
	// this ceiling (per-key Adaptive budget). Nil = no budget filter.
	MaxRateMultiplier *float64
	// AllowedLeafGroupIDs when non-empty restricts planning to these leaf IDs.
	// Nil/empty means every enabled pool member is eligible.
	AllowedLeafGroupIDs      map[int64]struct{}
	Protocol                 AdaptiveRouteProtocol
	QoSClass                 string
	RequiredOpenAICapability OpenAIEndpointCapability
	RequireCompact           bool
}

func (r *AdaptiveRouteRequest) normalize() {
	if r == nil {
		return
	}
	r.Platform = strings.ToLower(strings.TrimSpace(r.Platform))
	r.RequestedModel = strings.TrimSpace(r.RequestedModel)
	r.QoSClass = strings.TrimSpace(r.QoSClass)
	if r.Mode == "" {
		r.Mode = AdaptiveRouteModeIntelligence
	}
}

func (r AdaptiveRouteRequest) validate() error {
	if r.ParentGroupID <= 0 || r.Platform == "" || r.RequestedModel == "" {
		return ErrAdaptiveRouteInvalid
	}
	if r.Mode != AdaptiveRouteModeIntelligence && r.Mode != AdaptiveRouteModePrice {
		return ErrAdaptiveRouteInvalid
	}
	return nil
}

// AdaptiveRouteAccountSource deliberately uses the group-scoped repository
// query. Adaptive pools keep their leaf boundary even when the installation is
// otherwise configured in simple run mode.
type AdaptiveRouteAccountSource interface {
	ListSchedulableByGroupIDAndPlatforms(ctx context.Context, groupID int64, platforms []string) ([]Account, error)
}

type AdaptiveRouteGroupSource interface {
	GetByID(ctx context.Context, groupID int64) (*Group, error)
}

type AdaptiveRouteChannelSource interface {
	ResolveChannelMapping(ctx context.Context, groupID int64, model string) ChannelMappingResult
	IsModelRestricted(ctx context.Context, groupID int64, model string) bool
}

// AdaptiveRouteSignalSource supplies one immutable health/QoS observation for
// all leaves in a planning call. A missing source or a read failure enters the
// auditable cold-start ordering instead of making routing depend on call order.
type AdaptiveRouteSignalSource interface {
	GetAdaptiveRouteSignalSnapshot(ctx context.Context, req AdaptiveRouteSignalRequest) (*AdaptiveRouteSignalSnapshot, error)
}

type AdaptiveRouteSignalRequest struct {
	ParentGroupID    int64
	CanonicalModel   string
	Platform         string
	QoSClass         string
	ConfigGeneration int64
	LeafGroupIDs     []int64
}

type AdaptiveRouteSignalSnapshot struct {
	Generation int64
	SnapshotID string
	Leaves     map[int64]AdaptiveRouteLeafSignal
}

type AdaptiveRouteLeafSignal struct {
	Known               bool
	Healthy             bool
	HealthScore         float64
	QoSScore            float64
	ErrorRate           float64
	FirstTokenLatencyMS float64
	TotalLatencyMS      float64
	SampleCount         int64
}

// AdaptiveRouteCandidate is a value snapshot. CompatibleAccountIDs are audit
// evidence only; the leaf's existing account scheduler remains responsible for
// selecting and acquiring one account when the attempt starts.
type AdaptiveRouteCandidate struct {
	LeafGroupID                  int64
	Platform                     string
	RoutedModel                  string
	FrozenRateMultiplier         float64
	MinimumAccountRateMultiplier float64
	MaximumAccountRateMultiplier float64
	CompatibleAccountCount       int
	PriceSafeAccountCount        int
	MappedAccountCount           int
	CompatibleAccountIDs         []int64
	MemberSortOrder              int
	HealthKnown                  bool
	Healthy                      bool
	HealthScore                  float64
	QoSScore                     float64
	ErrorRate                    float64
	FirstTokenLatencyMS          float64
	TotalLatencyMS               float64
	SampleCount                  int64
	ColdStart                    bool
	RankReason                   string
}

func (c AdaptiveRouteCandidate) clone() AdaptiveRouteCandidate {
	c.CompatibleAccountIDs = append([]int64(nil), c.CompatibleAccountIDs...)
	return c
}

// AdaptiveRoutePlan hides its candidate slice and returns defensive copies so
// a request cannot reorder the frozen fallback plan by mutating shared memory.
type AdaptiveRoutePlan struct {
	ParentGroupID     int64
	Platform          string
	RequestedModel    string
	CanonicalModel    string
	Mode              AdaptiveRouteMode
	Protocol          AdaptiveRouteProtocol
	QoSClass          string
	ConfigGeneration  int64
	PricingGeneration int64
	PricingSnapshotID string
	SignalGeneration  int64
	SignalSnapshotID  string
	ColdStartReason   string
	candidates        []AdaptiveRouteCandidate
}

func (p *AdaptiveRoutePlan) Candidates() []AdaptiveRouteCandidate {
	if p == nil || len(p.candidates) == 0 {
		return nil
	}
	out := make([]AdaptiveRouteCandidate, len(p.candidates))
	for i := range p.candidates {
		out[i] = p.candidates[i].clone()
	}
	return out
}

// NewAdaptiveRoutePlan constructs a frozen plan from candidates. Tests use this
// because the candidate slice is unexported so request handlers cannot mutate it.
func NewAdaptiveRoutePlan(candidates []AdaptiveRouteCandidate) *AdaptiveRoutePlan {
	plan := &AdaptiveRoutePlan{candidates: make([]AdaptiveRouteCandidate, len(candidates))}
	for i := range candidates {
		plan.candidates[i] = candidates[i].clone()
	}
	return plan
}

func (p *AdaptiveRoutePlan) CandidateCount() int {
	if p == nil {
		return 0
	}
	return len(p.candidates)
}

// AdaptiveModelUnavailableError carries enough frozen identity to produce an
// explicit model-unavailable response without silently substituting a model.
type AdaptiveModelUnavailableError struct {
	ParentGroupID    int64
	Platform         string
	CanonicalModel   string
	ConfigGeneration int64
	Reason           string
}

func (e *AdaptiveModelUnavailableError) Error() string {
	if e == nil {
		return ErrAdaptiveModelUnavailable.Error()
	}
	return fmt.Sprintf("%s: parent_group_id=%d platform=%s model=%s generation=%d reason=%s",
		ErrAdaptiveModelUnavailable, e.ParentGroupID, e.Platform, e.CanonicalModel, e.ConfigGeneration, e.Reason)
}

func (e *AdaptiveModelUnavailableError) Unwrap() error { return ErrAdaptiveModelUnavailable }

type AdaptiveRoutePlanner struct {
	pool     AdaptivePoolSnapshotRepository
	accounts AdaptiveRouteAccountSource
	groups   AdaptiveRouteGroupSource
	channels AdaptiveRouteChannelSource
	signals  AdaptiveRouteSignalSource
}

func NewAdaptiveRoutePlanner(
	pool AdaptivePoolSnapshotRepository,
	accounts AdaptiveRouteAccountSource,
	groups AdaptiveRouteGroupSource,
	channels AdaptiveRouteChannelSource,
	signals AdaptiveRouteSignalSource,
) *AdaptiveRoutePlanner {
	return &AdaptiveRoutePlanner{
		pool: pool, accounts: accounts, groups: groups, channels: channels, signals: signals,
	}
}

// AdaptiveLeafIdentity is a visible-parent catalog entry: leaf group plus its
// own platform. Parent platform is not a filter.
type AdaptiveLeafIdentity struct {
	GroupID  int64
	Platform string
}

// EnabledLeaves returns enabled Adaptive leaf groups for model listing.
// ErrAdaptivePoolNotFound / ErrAdaptivePoolDisabled mean the group is not an
// active Adaptive parent and callers should fall back to the parent catalog.
func (p *AdaptiveRoutePlanner) EnabledLeaves(ctx context.Context, parentGroupID int64, allowedLeafIDs []int64) ([]AdaptiveLeafIdentity, error) {
	if p == nil || p.pool == nil {
		return nil, ErrAdaptiveRouteUnavailable
	}
	if parentGroupID <= 0 {
		return nil, ErrAdaptiveRouteInvalid
	}
	pool, err := p.pool.GetAdaptivePoolSnapshot(ctx, parentGroupID)
	if err != nil {
		return nil, err
	}
	if pool == nil || pool.ParentGroupID != parentGroupID {
		return nil, ErrAdaptivePoolNotFound
	}
	if !pool.Enabled {
		return nil, ErrAdaptivePoolDisabled
	}
	allowlist := AdaptiveLeafAllowlist(allowedLeafIDs)
	out := make([]AdaptiveLeafIdentity, 0, len(pool.Members))
	seen := make(map[int64]struct{}, len(pool.Members))
	for _, member := range pool.Members {
		if !member.Enabled || member.LeafGroupID <= 0 || member.LeafGroupID == pool.ParentGroupID {
			continue
		}
		if len(allowlist) > 0 {
			if _, ok := allowlist[member.LeafGroupID]; !ok {
				continue
			}
		}
		if _, dup := seen[member.LeafGroupID]; dup {
			continue
		}
		seen[member.LeafGroupID] = struct{}{}
		platform := ""
		if p.groups != nil {
			group, groupErr := p.groups.GetByID(ctx, member.LeafGroupID)
			if groupErr != nil || group == nil || group.ID != member.LeafGroupID || !group.IsActive() {
				continue
			}
			platform = strings.ToLower(strings.TrimSpace(group.Platform))
		}
		out = append(out, AdaptiveLeafIdentity{GroupID: member.LeafGroupID, Platform: platform})
	}
	return out, nil
}

// leafInput is the input shape consumed by planFromLeaves: adaptive pool
// members plus their resolved group, channel mapping, and schedulable accounts.
type leafInput struct {
	ref      AdaptiveLeafRef
	group    *Group
	mapping  ChannelMappingResult
	accounts []Account
}

func (p *AdaptiveRoutePlanner) Plan(ctx context.Context, req AdaptiveRouteRequest) (*AdaptiveRoutePlan, error) {
	req.normalize()
	if err := req.validate(); err != nil {
		return nil, err
	}
	if p == nil || p.pool == nil || p.accounts == nil || p.groups == nil {
		return nil, ErrAdaptiveRouteUnavailable
	}

	canonicalModel := CanonicalizeAdaptiveModel(req.Platform, req.RequestedModel)
	if canonicalModel == "" {
		return nil, ErrAdaptiveRouteInvalid
	}
	pool, err := p.pool.GetAdaptivePoolSnapshot(ctx, req.ParentGroupID)
	if err != nil {
		return nil, fmt.Errorf("load adaptive pool: %w", err)
	}
	if pool == nil || pool.ParentGroupID != req.ParentGroupID {
		return nil, ErrAdaptiveRouteUnavailable
	}
	if !pool.Enabled {
		return nil, ErrAdaptivePoolDisabled
	}
	// Pool-level manual intelligence order overrides request flag when set.
	if pool.UseManualIntelligenceOrder {
		req.UseManualIntelligenceOrder = true
	}
	poolPlatform := strings.ToLower(strings.TrimSpace(pool.Platform))
	if poolPlatform == "" {
		return nil, ErrAdaptiveRouteUnavailable
	}

	leaves := make([]leafInput, 0, len(pool.Members))
	leafIDs := make([]int64, 0, len(pool.Members))
	seenLeaf := make(map[int64]struct{}, len(pool.Members))
	for _, member := range pool.Members {
		if !member.Enabled || member.LeafGroupID <= 0 || member.LeafGroupID == pool.ParentGroupID {
			continue
		}
		if len(req.AllowedLeafGroupIDs) > 0 {
			if _, ok := req.AllowedLeafGroupIDs[member.LeafGroupID]; !ok {
				continue
			}
		}
		if _, duplicate := seenLeaf[member.LeafGroupID]; duplicate {
			continue
		}
		seenLeaf[member.LeafGroupID] = struct{}{}
		group, groupErr := p.groups.GetByID(ctx, member.LeafGroupID)
		if groupErr != nil {
			return nil, fmt.Errorf("load adaptive leaf group %d: %w", member.LeafGroupID, groupErr)
		}
		if group == nil || group.ID != member.LeafGroupID || !group.IsActive() ||
			group.RateMultiplier < 0 || math.IsNaN(group.RateMultiplier) || math.IsInf(group.RateMultiplier, 0) {
			continue
		}
		// Hidden/exclusive leaf groups are Adaptive infrastructure. Users bind
		// the visible parent; do not filter leaves by user_visible or exclusive
		// here or Adaptive routing would empty the pool.
		// Leaf groups may span protocols (247). Canonicalize and list accounts
		// with the LEAF platform, never the parent row's platform.
		leafPlatform := strings.ToLower(strings.TrimSpace(group.Platform))
		if leafPlatform == "" {
			continue
		}
		leafModel := CanonicalizeAdaptiveModel(leafPlatform, req.RequestedModel)
		if leafModel == "" {
			leafModel = canonicalModel
		}
		mapping := ChannelMappingResult{MappedModel: leafModel}
		if p.channels != nil {
			mapping = p.channels.ResolveChannelMapping(ctx, group.ID, leafModel)
			if strings.TrimSpace(mapping.MappedModel) == "" {
				mapping.MappedModel = leafModel
			}
			billingModel := billingModelForRestriction(mapping.BillingModelSource, leafModel, mapping.MappedModel)
			if billingModel != "" && p.channels.IsModelRestricted(ctx, group.ID, billingModel) {
				continue
			}
		}
		platforms := adaptiveRouteAccountPlatforms(leafPlatform)
		leafAccounts, accountErr := p.accounts.ListSchedulableByGroupIDAndPlatforms(ctx, group.ID, platforms)
		if accountErr != nil {
			return nil, fmt.Errorf("load adaptive leaf accounts %d: %w", group.ID, accountErr)
		}
		leaves = append(leaves, leafInput{ref: member, group: group, mapping: mapping, accounts: leafAccounts})
		leafIDs = append(leafIDs, member.LeafGroupID)
	}

	return p.planFromLeaves(ctx, req, canonicalModel, poolPlatform, pool.ConfigGeneration, leaves, leafIDs)
}

// planFromLeaves is the adaptive candidate builder: load signals, apply price
// protection + per-key budget, sort, and freeze the ordered plan.
func (p *AdaptiveRoutePlanner) planFromLeaves(
	ctx context.Context,
	req AdaptiveRouteRequest,
	canonicalModel string,
	poolPlatform string,
	configGeneration int64,
	leaves []leafInput,
	leafIDs []int64,
) (*AdaptiveRoutePlan, error) {
	signalSnapshot, signalReason := p.loadSignals(ctx, req, canonicalModel, configGeneration, leafIDs)
	healthyCandidates := make([]AdaptiveRouteCandidate, 0, len(leaves))
	degradedCandidates := make([]AdaptiveRouteCandidate, 0, len(leaves))
	pricingGeneration := configGeneration
	for _, leaf := range leaves {
		candidate := AdaptiveRouteCandidate{
			LeafGroupID:          leaf.group.ID,
			Platform:             strings.ToLower(strings.TrimSpace(leaf.group.Platform)),
			RoutedModel:          leaf.mapping.MappedModel,
			FrozenRateMultiplier: leaf.group.RateMultiplier,
			MemberSortOrder:      leaf.ref.SortOrder,
		}
		if updated := leaf.group.UpdatedAt.UnixNano(); updated > pricingGeneration {
			pricingGeneration = updated
		}
		priceSafeIDs := make([]int64, 0, len(leaf.accounts))
		lossMakingIDs := make([]int64, 0, len(leaf.accounts))
		rateSeen := false
		var minRate, maxRate float64
		compatibleAccounts := make([]Account, 0, len(leaf.accounts))
		for i := range leaf.accounts {
			account := &leaf.accounts[i]
			if !adaptiveRouteAccountCompatible(ctx, account, leaf.group, candidate.Platform, candidate.RoutedModel, req) {
				continue
			}
			compatibleAccounts = append(compatibleAccounts, *account)
		}
		compatibleAccounts = accountsSupportingRequestedModel(compatibleAccounts, candidate.RoutedModel)
		for i := range compatibleAccounts {
			account := &compatibleAccounts[i]
			if p.channels != nil && leaf.mapping.BillingModelSource == BillingModelSourceUpstream {
				upstreamModel := adaptiveRouteUpstreamModel(account, candidate.RoutedModel, req.RequireCompact)
				if upstreamModel != "" && p.channels.IsModelRestricted(ctx, leaf.group.ID, upstreamModel) {
					continue
				}
			}
			if len(account.GetModelMapping()) > 0 {
				candidate.MappedAccountCount++
			}
			rate := account.BillingRateMultiplier()
			if !rateSeen || rate < minRate {
				minRate = rate
			}
			if !rateSeen || rate > maxRate {
				maxRate = rate
			}
			rateSeen = true
			if adaptiveAccountRateWithinLeaf(rate, leaf.group.RateMultiplier) {
				priceSafeIDs = append(priceSafeIDs, account.ID)
			} else {
				lossMakingIDs = append(lossMakingIDs, account.ID)
			}
		}
		eligibleIDs := priceSafeIDs
		if len(eligibleIDs) == 0 {
			eligibleIDs = lossMakingIDs
		}
		candidate.CompatibleAccountIDs = append(candidate.CompatibleAccountIDs, eligibleIDs...)
		candidate.CompatibleAccountCount = len(eligibleIDs)
		candidate.PriceSafeAccountCount = len(priceSafeIDs)
		if rateSeen {
			candidate.MinimumAccountRateMultiplier = minRate
			candidate.MaximumAccountRateMultiplier = maxRate
		}
		if candidate.CompatibleAccountCount == 0 {
			continue
		}
		if req.MaxRateMultiplier != nil && candidate.FrozenRateMultiplier > *req.MaxRateMultiplier {
			continue
		}
		sort.Slice(candidate.CompatibleAccountIDs, func(i, j int) bool {
			return candidate.CompatibleAccountIDs[i] < candidate.CompatibleAccountIDs[j]
		})
		if signalSnapshot != nil {
			applyAdaptiveRouteSignal(&candidate, signalSnapshot.Leaves[candidate.LeafGroupID])
		}
		candidate.ColdStart = !candidate.HealthKnown
		candidate.RankReason = adaptiveRouteRankReason(req.Mode, candidate.HealthKnown)
		if candidate.HealthKnown && !candidate.Healthy {
			degradedCandidates = append(degradedCandidates, candidate)
			continue
		}
		healthyCandidates = append(healthyCandidates, candidate)
	}

	if len(healthyCandidates) == 0 && len(degradedCandidates) == 0 {
		return nil, &AdaptiveModelUnavailableError{
			ParentGroupID: req.ParentGroupID, Platform: poolPlatform, CanonicalModel: canonicalModel,
			ConfigGeneration: configGeneration, Reason: "no_healthy_schedulable_compatible_leaf",
		}
	}
	sortAdaptiveRouteCandidates(healthyCandidates, req.Mode, req.UseManualIntelligenceOrder)
	sortAdaptiveRouteCandidates(degradedCandidates, req.Mode, req.UseManualIntelligenceOrder)
	candidates := make([]AdaptiveRouteCandidate, 0, len(healthyCandidates)+len(degradedCandidates))
	candidates = append(candidates, healthyCandidates...)
	candidates = append(candidates, degradedCandidates...)
	if len(candidates) > AdaptiveRouteMaxCandidates {
		primaryN := len(healthyCandidates)
		if primaryN > AdaptiveRouteMaxCandidates {
			primaryN = AdaptiveRouteMaxCandidates
		}
		if primaryN == 0 {
			candidates = degradedCandidates[:AdaptiveRouteMaxCandidates]
		} else {
			need := AdaptiveRouteMaxCandidates - primaryN
			if need > len(degradedCandidates) {
				need = len(degradedCandidates)
			}
			candidates = append(append([]AdaptiveRouteCandidate{}, healthyCandidates[:primaryN]...), degradedCandidates[:need]...)
		}
	}
	pricingSnapshotID := adaptiveRoutePricingSnapshotID(req.ParentGroupID, canonicalModel, configGeneration, pricingGeneration, candidates)

	plan := &AdaptiveRoutePlan{
		ParentGroupID: req.ParentGroupID, Platform: poolPlatform, RequestedModel: req.RequestedModel,
		CanonicalModel: canonicalModel, Mode: req.Mode, Protocol: req.Protocol, QoSClass: req.QoSClass,
		ConfigGeneration: configGeneration, PricingGeneration: pricingGeneration,
		PricingSnapshotID: pricingSnapshotID, ColdStartReason: signalReason,
		candidates: make([]AdaptiveRouteCandidate, len(candidates)),
	}
	if signalSnapshot != nil {
		plan.SignalGeneration = signalSnapshot.Generation
		plan.SignalSnapshotID = strings.TrimSpace(signalSnapshot.SnapshotID)
	}
	for i := range candidates {
		plan.candidates[i] = candidates[i].clone()
	}
	return plan, nil
}

func (p *AdaptiveRoutePlanner) loadSignals(
	ctx context.Context,
	req AdaptiveRouteRequest,
	canonicalModel string,
	configGeneration int64,
	leafIDs []int64,
) (*AdaptiveRouteSignalSnapshot, string) {
	if p.signals == nil {
		return nil, "signal_source_unconfigured"
	}
	snapshot, err := p.signals.GetAdaptiveRouteSignalSnapshot(ctx, AdaptiveRouteSignalRequest{
		ParentGroupID: req.ParentGroupID, CanonicalModel: canonicalModel, Platform: req.Platform,
		QoSClass: req.QoSClass, ConfigGeneration: configGeneration, LeafGroupIDs: append([]int64(nil), leafIDs...),
	})
	if err != nil {
		return nil, "signal_snapshot_error"
	}
	if snapshot == nil || snapshot.Generation < 0 {
		return nil, "signal_snapshot_missing"
	}
	return snapshot, ""
}

func CanonicalizeAdaptiveModel(platform, requestedModel string) string {
	model := strings.ToLower(strings.TrimSpace(requestedModel))
	if model == "" {
		return ""
	}
	model = strings.TrimLeft(model, "/")
	if strings.Contains(model, "/") {
		model = model[strings.LastIndexByte(model, '/')+1:]
	}
	switch strings.ToLower(strings.TrimSpace(platform)) {
	case PlatformOpenAI, PlatformGrok:
		if normalized := canonicalizeOpenAIModelAliasSpelling(model); normalized != "" {
			model = normalized
		}
		if normalized := normalizeKnownOpenAICodexModel(model); normalized != "" {
			model = normalized
		}
		return model
	case PlatformAnthropic:
		return claude.NormalizeModelID(model)
	default:
		return model
	}
}

func adaptiveRouteAccountPlatforms(platform string) []string {
	if platform == PlatformAnthropic {
		return []string{PlatformAnthropic, PlatformAntigravity}
	}
	return []string{platform}
}

func adaptiveRouteAccountCompatible(
	ctx context.Context,
	account *Account,
	group *Group,
	platform, routedModel string,
	req AdaptiveRouteRequest,
) bool {
	if account == nil || group == nil || !account.IsSchedulableForModelWithContext(ctx, routedModel) {
		return false
	}
	if !supportsUpstreamModelForRequest(ctx, account, routedModel, req.RequireCompact) {
		return false
	}
	if group.RequireOAuthOnly && account.Type == AccountTypeAPIKey {
		return false
	}
	if group.RequirePrivacySet && !account.IsPrivacySet() {
		return false
	}
	if platform == PlatformOpenAI || platform == PlatformGrok {
		return isOpenAICompatibleAccountEligibleForRequest(
			ctx, account, platform, routedModel, req.RequireCompact, req.RequiredOpenAICapability,
		)
	}
	if platform == PlatformAnthropic {
		if account.Platform == PlatformAntigravity && !account.IsMixedSchedulingEnabled() {
			return false
		}
		if account.Platform != PlatformAnthropic && account.Platform != PlatformAntigravity {
			return false
		}
		return adaptiveRouteAnthropicModelSupported(ctx, account, routedModel)
	}
	return account.MatchesRequestedPlatform(platform) && account.IsModelSupported(routedModel)
}

// adaptiveAccountRateWithinLeaf reports whether an account's upstream billing
// rate is at or below a leaf group's downstream rate, so the account can serve
// that leaf without losing margin. It shares the profit-control epsilon so the
// same decimal rounding tolerance applies everywhere.
func adaptiveAccountRateWithinLeaf(accountRate, leafRate float64) bool {
	return !profitControlOverThreshold(accountRate, leafRate)
}

func adaptiveRouteAnthropicModelSupported(ctx context.Context, account *Account, requestedModel string) bool {
	if account == nil {
		return false
	}
	if account.Platform == PlatformAntigravity {
		if strings.TrimSpace(requestedModel) == "" {
			return true
		}
		mapped := mapAntigravityModel(account, requestedModel)
		if mapped == "" {
			return false
		}
		if enabled, ok := ThinkingEnabledFromContext(ctx); ok {
			finalModel := applyThinkingModelSuffix(mapped, enabled)
			return finalModel == mapped || account.IsModelSupported(finalModel)
		}
		return true
	}
	if account.IsBedrock() {
		_, ok := ResolveBedrockModelID(account, requestedModel)
		return ok
	}
	if account.Platform == PlatformAnthropic && account.Type != AccountTypeAPIKey {
		if account.Type == AccountTypeServiceAccount {
			requestedModel = normalizeVertexAnthropicModelID(claude.NormalizeModelID(requestedModel))
		} else {
			requestedModel = claude.NormalizeModelID(requestedModel)
		}
	}
	return account.IsModelSupported(requestedModel)
}

func adaptiveRouteUpstreamModel(account *Account, requestedModel string, requireCompact bool) string {
	if account == nil {
		return ""
	}
	if account.Platform == PlatformOpenAI || account.Platform == PlatformGrok {
		return resolveOpenAIAccountUpstreamModelForRequest(account, requestedModel, requireCompact)
	}
	return resolveAccountUpstreamModel(account, requestedModel)
}

func applyAdaptiveRouteSignal(candidate *AdaptiveRouteCandidate, signal AdaptiveRouteLeafSignal) {
	if candidate == nil || !signal.Known || !adaptiveRouteSignalValid(signal) {
		return
	}
	candidate.HealthKnown = true
	candidate.Healthy = signal.Healthy
	candidate.HealthScore = signal.HealthScore
	candidate.QoSScore = signal.QoSScore
	candidate.ErrorRate = signal.ErrorRate
	candidate.FirstTokenLatencyMS = signal.FirstTokenLatencyMS
	candidate.TotalLatencyMS = signal.TotalLatencyMS
	candidate.SampleCount = signal.SampleCount
}

func adaptiveRouteSignalValid(signal AdaptiveRouteLeafSignal) bool {
	values := []float64{signal.HealthScore, signal.QoSScore, signal.ErrorRate, signal.FirstTokenLatencyMS, signal.TotalLatencyMS}
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
			return false
		}
	}
	return signal.SampleCount >= 0
}

func adaptiveRouteRankReason(mode AdaptiveRouteMode, healthKnown bool) string {
	if healthKnown {
		if mode == AdaptiveRouteModePrice {
			return "health_then_member_order_then_price"
		}
		return "health_then_member_order_then_intelligence_tiebreak"
	}
	return "cold_start_member_sort_order"
}

// RecordLeafOutcome feeds passive traffic outcomes into the signal store when
// the planner was wired with an AdaptiveRouteSignalStore (or compatible type).
func (p *AdaptiveRoutePlanner) RecordLeafOutcome(outcome AdaptiveLeafOutcome) {
	if p == nil || p.signals == nil {
		return
	}
	if store, ok := p.signals.(*AdaptiveRouteSignalStore); ok {
		store.RecordLeafOutcome(outcome)
	}
}

// adaptiveLeafIsKnownUnhealthy reports hard-bad leaves kept only as last resort.
func adaptiveLeafIsKnownUnhealthy(c AdaptiveRouteCandidate) bool {
	return c.HealthKnown && !c.Healthy
}

// adaptiveLeafHealthSoftRank: higher is preferred among non-unhealthy leaves.
// 2 = known healthy, 1 = cold-start (unknown). Does not rank unhealthy.
func adaptiveLeafHealthSoftRank(c AdaptiveRouteCandidate) int {
	if c.HealthKnown && c.Healthy {
		return 2
	}
	return 1
}

func sortAdaptiveRouteCandidates(candidates []AdaptiveRouteCandidate, mode AdaptiveRouteMode, manualIntelligence bool) {
	sort.SliceStable(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		// 1) Known-unhealthy always last (last resort). Do NOT promote a known-
		// healthy expensive leaf above a cold-start cheap leaf — that made
		// price mode still hit 兜底 after intelligence traffic warmed the
		// expensive leaf.
		aBad, bBad := adaptiveLeafIsKnownUnhealthy(a), adaptiveLeafIsKnownUnhealthy(b)
		if aBad != bBad {
			return !aBad
		}
		// Explicit mapping beats empty-mapping "allow family" leaves. An empty
		// OpenAI leaf otherwise occupies one of the 5 Adaptive slots and 404s
		// before a later mapped GPT/Gemini/CN leaf is ever attempted.
		aMapped, bMapped := a.MappedAccountCount > 0, b.MappedAccountCount > 0
		if aMapped != bMapped {
			return aMapped
		}
		// 2) Mode primary ranking (price / intelligence / manual).
		if mode == AdaptiveRouteModePrice {
			// 价格优先：倍率从低到高
			if a.FrozenRateMultiplier != b.FrozenRateMultiplier {
				return a.FrozenRateMultiplier < b.FrozenRateMultiplier
			}
			if a.MemberSortOrder != b.MemberSortOrder {
				return a.MemberSortOrder < b.MemberSortOrder
			}
		} else if manualIntelligence {
			// 智力优先 + 手动标定：membership sort_order（小=更优智力）
			if a.MemberSortOrder != b.MemberSortOrder {
				return a.MemberSortOrder < b.MemberSortOrder
			}
			if a.FrozenRateMultiplier != b.FrozenRateMultiplier {
				return a.FrozenRateMultiplier > b.FrozenRateMultiplier
			}
		} else {
			// 智力优先默认：价格越高智力越高
			if a.FrozenRateMultiplier != b.FrozenRateMultiplier {
				return a.FrozenRateMultiplier > b.FrozenRateMultiplier
			}
			// 同价时用 sort_order 做微调标定
			if a.MemberSortOrder != b.MemberSortOrder {
				return a.MemberSortOrder < b.MemberSortOrder
			}
		}
		// 3) Soft health tie-break among same mode rank: healthy > cold-start.
		if ra, rb := adaptiveLeafHealthSoftRank(a), adaptiveLeafHealthSoftRank(b); ra != rb {
			return ra > rb
		}
		// 4) Passive health / QoS scores when both known healthy.
		if a.HealthKnown && b.HealthKnown && a.Healthy && b.Healthy {
			if a.HealthScore != b.HealthScore {
				return a.HealthScore > b.HealthScore
			}
			if a.QoSScore != b.QoSScore {
				return a.QoSScore > b.QoSScore
			}
		}
		return a.LeafGroupID < b.LeafGroupID
	})
}

func adaptiveRoutePricingSnapshotID(
	parentGroupID int64,
	canonicalModel string,
	configGeneration, pricingGeneration int64,
	candidates []AdaptiveRouteCandidate,
) string {
	h := sha256.New()
	write := func(value string) {
		_, _ = h.Write([]byte(value))
		_, _ = h.Write([]byte{0})
	}
	write("adaptive-route-pricing-v1")
	write(strconv.FormatInt(parentGroupID, 10))
	write(canonicalModel)
	write(strconv.FormatInt(configGeneration, 10))
	write(strconv.FormatInt(pricingGeneration, 10))
	for _, candidate := range candidates {
		write(strconv.FormatInt(candidate.LeafGroupID, 10))
		write(candidate.RoutedModel)
		write(strconv.FormatFloat(candidate.FrozenRateMultiplier, 'g', -1, 64))
		write(strconv.FormatFloat(candidate.MinimumAccountRateMultiplier, 'g', -1, 64))
		write(strconv.FormatFloat(candidate.MaximumAccountRateMultiplier, 'g', -1, 64))
		write(strconv.Itoa(candidate.PriceSafeAccountCount))
	}
	return hex.EncodeToString(h.Sum(nil))
}
