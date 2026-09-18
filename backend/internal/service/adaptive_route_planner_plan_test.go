package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type stubAdaptivePoolRepo struct {
	snapshot *AdaptivePoolSnapshot
}

func (s stubAdaptivePoolRepo) GetAdaptivePoolSnapshot(context.Context, int64) (*AdaptivePoolSnapshot, error) {
	return s.snapshot, nil
}

func (s stubAdaptivePoolRepo) ListAdaptivePoolSnapshots(context.Context) ([]AdaptivePoolSnapshot, error) {
	if s.snapshot == nil {
		return nil, nil
	}
	return []AdaptivePoolSnapshot{*s.snapshot}, nil
}

type stubAdaptiveGroupRepo struct {
	groups map[int64]*Group
}

func (s stubAdaptiveGroupRepo) GetByID(_ context.Context, groupID int64) (*Group, error) {
	return s.groups[groupID], nil
}

type stubAdaptiveAccountRepo struct {
	byGroup map[int64][]Account
}

func (s stubAdaptiveAccountRepo) ListSchedulableByGroupIDAndPlatforms(_ context.Context, groupID int64, platforms []string) ([]Account, error) {
	accounts := s.byGroup[groupID]
	if len(platforms) == 0 {
		return append([]Account(nil), accounts...), nil
	}
	allowed := make(map[string]struct{}, len(platforms))
	for _, platform := range platforms {
		allowed[platform] = struct{}{}
	}
	out := make([]Account, 0, len(accounts))
	for _, account := range accounts {
		if _, ok := allowed[account.Platform]; ok {
			out = append(out, account)
		}
	}
	return out, nil
}

type stubAdaptiveChannelRepo struct{}

func (stubAdaptiveChannelRepo) ResolveChannelMapping(_ context.Context, _ int64, model string) ChannelMappingResult {
	return ChannelMappingResult{MappedModel: model}
}

func (stubAdaptiveChannelRepo) IsModelRestricted(context.Context, int64, string) bool {
	return false
}

func testAdaptiveLeafGroup(id int64, platform string, visible bool, exclusive bool) *Group {
	return &Group{
		ID:             id,
		Platform:       platform,
		Status:         StatusActive,
		RateMultiplier: 1,
		UserVisible:    visible,
		UserVisibleSet: true,
		IsExclusive:    exclusive,
	}
}

func testAdaptiveLeafAccount(id int64, platform string) Account {
	account := Account{
		ID:          id,
		Platform:    platform,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
	}
	switch platform {
	case PlatformOpenAI:
		account.Credentials = map[string]any{"model_mapping": map[string]any{"gpt-4o": "gpt-4o"}}
	case PlatformAnthropic:
		account.Credentials = map[string]any{"model_mapping": map[string]any{"gpt-4o": "claude-sonnet-4", "claude-sonnet-4": "claude-sonnet-4"}}
	case PlatformGemini:
		account.Credentials = map[string]any{"model_mapping": map[string]any{"gpt-4o": "gemini-2.5-flash", "gemini-3.8-flash": "gemini-3.8-flash"}}
	}
	return account
}

func TestAdaptivePlanIncludesMixedPlatformLeaves(t *testing.T) {
	t.Parallel()

	planner := NewAdaptiveRoutePlanner(
		stubAdaptivePoolRepo{snapshot: &AdaptivePoolSnapshot{
			ParentGroupID:    10,
			Platform:         PlatformOpenAI,
			Enabled:          true,
			ConfigGeneration: 1,
			Members: []AdaptiveLeafRef{
				{LeafGroupID: 21, Enabled: true, SortOrder: 1},
				{LeafGroupID: 22, Enabled: true, SortOrder: 2},
				{LeafGroupID: 23, Enabled: true, SortOrder: 3},
			},
		}},
		stubAdaptiveAccountRepo{byGroup: map[int64][]Account{
			21: {testAdaptiveLeafAccount(201, PlatformOpenAI)},
			22: {testAdaptiveLeafAccount(202, PlatformAnthropic)},
			23: {testAdaptiveLeafAccount(203, PlatformGemini)},
		}},
		stubAdaptiveGroupRepo{groups: map[int64]*Group{
			21: testAdaptiveLeafGroup(21, PlatformOpenAI, true, false),
			22: testAdaptiveLeafGroup(22, PlatformAnthropic, true, false),
			23: testAdaptiveLeafGroup(23, PlatformGemini, true, false),
		}},
		stubAdaptiveChannelRepo{},
		nil,
	)

	plan, err := planner.Plan(context.Background(), AdaptiveRouteRequest{
		ParentGroupID:  10,
		Platform:       PlatformOpenAI,
		RequestedModel: "gpt-4o",
		Mode:           AdaptiveRouteModePrice,
		Protocol:       AdaptiveRouteProtocolOpenAIChat,
	})
	require.NoError(t, err)
	require.NotNil(t, plan)

	got := map[int64]string{}
	for _, candidate := range plan.Candidates() {
		got[candidate.LeafGroupID] = candidate.Platform
	}
	require.Equal(t, "openai", got[21])
	require.Equal(t, "anthropic", got[22])
	require.Equal(t, "gemini", got[23])
	require.Len(t, got, 3)
}

func TestAdaptivePlanRespectsLeafAllowlist(t *testing.T) {
	t.Parallel()

	planner := NewAdaptiveRoutePlanner(
		stubAdaptivePoolRepo{snapshot: &AdaptivePoolSnapshot{
			ParentGroupID:    10,
			Platform:         PlatformOpenAI,
			Enabled:          true,
			ConfigGeneration: 1,
			Members: []AdaptiveLeafRef{
				{LeafGroupID: 21, Enabled: true, SortOrder: 1},
				{LeafGroupID: 22, Enabled: true, SortOrder: 2},
				{LeafGroupID: 23, Enabled: true, SortOrder: 3},
			},
		}},
		stubAdaptiveAccountRepo{byGroup: map[int64][]Account{
			21: {testAdaptiveLeafAccount(201, PlatformOpenAI)},
			22: {testAdaptiveLeafAccount(202, PlatformAnthropic)},
			23: {testAdaptiveLeafAccount(203, PlatformGemini)},
		}},
		stubAdaptiveGroupRepo{groups: map[int64]*Group{
			21: testAdaptiveLeafGroup(21, PlatformOpenAI, true, false),
			22: testAdaptiveLeafGroup(22, PlatformAnthropic, true, false),
			23: testAdaptiveLeafGroup(23, PlatformGemini, true, false),
		}},
		stubAdaptiveChannelRepo{},
		nil,
	)

	plan, err := planner.Plan(context.Background(), AdaptiveRouteRequest{
		ParentGroupID:       10,
		Platform:            PlatformOpenAI,
		RequestedModel:      "gpt-4o",
		Mode:                AdaptiveRouteModePrice,
		Protocol:            AdaptiveRouteProtocolOpenAIChat,
		AllowedLeafGroupIDs: AdaptiveLeafAllowlist([]int64{22, 23}),
	})
	require.NoError(t, err)
	got := map[int64]string{}
	for _, candidate := range plan.Candidates() {
		got[candidate.LeafGroupID] = candidate.Platform
	}
	require.Equal(t, "anthropic", got[22])
	require.Equal(t, "gemini", got[23])
	require.Len(t, got, 2)
}

func TestAdaptivePlanIncludesHiddenAndExclusiveLeaves(t *testing.T) {
	t.Parallel()

	planner := NewAdaptiveRoutePlanner(
		stubAdaptivePoolRepo{snapshot: &AdaptivePoolSnapshot{
			ParentGroupID:    10,
			Platform:         PlatformOpenAI,
			Enabled:          true,
			ConfigGeneration: 1,
			Members: []AdaptiveLeafRef{
				{LeafGroupID: 21, Enabled: true, SortOrder: 1},
				{LeafGroupID: 22, Enabled: true, SortOrder: 2},
				{LeafGroupID: 23, Enabled: true, SortOrder: 3},
			},
		}},
		stubAdaptiveAccountRepo{byGroup: map[int64][]Account{
			21: {testAdaptiveLeafAccount(201, PlatformOpenAI)},
			22: {testAdaptiveLeafAccount(202, PlatformAnthropic)},
			23: {testAdaptiveLeafAccount(203, PlatformGemini)},
		}},
		stubAdaptiveGroupRepo{groups: map[int64]*Group{
			21: testAdaptiveLeafGroup(21, PlatformOpenAI, true, false),
			22: testAdaptiveLeafGroup(22, PlatformAnthropic, false, false),
			23: testAdaptiveLeafGroup(23, PlatformGemini, true, true),
		}},
		stubAdaptiveChannelRepo{},
		nil,
	)

	plan, err := planner.Plan(context.Background(), AdaptiveRouteRequest{
		ParentGroupID:   10,
		UserID:          7,
		UserIsAdmin:     false,
		AllowedGroupIDs: map[int64]struct{}{},
		Platform:        PlatformOpenAI,
		RequestedModel:  "gpt-4o",
		Mode:            AdaptiveRouteModePrice,
	})
	require.NoError(t, err)
	got := map[int64]string{}
	for _, candidate := range plan.Candidates() {
		got[candidate.LeafGroupID] = candidate.Platform
	}
	require.Equal(t, "openai", got[21])
	require.Equal(t, "anthropic", got[22])
	require.Equal(t, "gemini", got[23])
	require.Len(t, got, 3)
}

func TestAdaptiveRouteAccountCompatibleUsesLeafPlatform(t *testing.T) {
	t.Parallel()

	openaiGroup := testAdaptiveLeafGroup(21, PlatformOpenAI, true, false)
	anthropicGroup := testAdaptiveLeafGroup(22, PlatformAnthropic, true, false)
	openaiAccount := testAdaptiveLeafAccount(201, PlatformOpenAI)
	anthropicAccount := testAdaptiveLeafAccount(202, PlatformAnthropic)
	req := AdaptiveRouteRequest{Protocol: AdaptiveRouteProtocolOpenAIChat}

	require.True(t, adaptiveRouteAccountCompatible(context.Background(), &openaiAccount, openaiGroup, PlatformOpenAI, "gpt-4o", req))
	require.False(t, adaptiveRouteAccountCompatible(context.Background(), &openaiAccount, anthropicGroup, PlatformAnthropic, "gpt-4o", req))
	require.True(t, adaptiveRouteAccountCompatible(context.Background(), &anthropicAccount, anthropicGroup, PlatformAnthropic, "claude-sonnet-4", req))
	require.False(t, adaptiveRouteAccountCompatible(context.Background(), &anthropicAccount, openaiGroup, PlatformOpenAI, "claude-sonnet-4", req))
}

func TestAdaptivePlanSkipsEmptyMappingOpenAILeafForGeminiModel(t *testing.T) {
	t.Parallel()

	emptyOpenAI := testAdaptiveLeafAccount(201, PlatformOpenAI)
	emptyOpenAI.Credentials = map[string]any{}
	geminiLeaf := testAdaptiveLeafAccount(203, PlatformGemini)

	planner := NewAdaptiveRoutePlanner(
		stubAdaptivePoolRepo{snapshot: &AdaptivePoolSnapshot{
			ParentGroupID:    108,
			Platform:         PlatformAdaptive,
			Enabled:          true,
			ConfigGeneration: 1,
			Members: []AdaptiveLeafRef{
				{LeafGroupID: 17, Enabled: true, SortOrder: 1},
				{LeafGroupID: 114, Enabled: true, SortOrder: 2},
			},
		}},
		stubAdaptiveAccountRepo{byGroup: map[int64][]Account{
			17:  {emptyOpenAI},
			114: {geminiLeaf},
		}},
		stubAdaptiveGroupRepo{groups: map[int64]*Group{
			17:  testAdaptiveLeafGroup(17, PlatformOpenAI, true, false),
			114: testAdaptiveLeafGroup(114, PlatformGemini, true, false),
		}},
		stubAdaptiveChannelRepo{},
		nil,
	)

	plan, err := planner.Plan(context.Background(), AdaptiveRouteRequest{
		ParentGroupID:  108,
		Platform:       PlatformOpenAI,
		RequestedModel: "gemini-3.8-flash",
		Mode:           AdaptiveRouteModePrice,
		Protocol:       AdaptiveRouteProtocolOpenAIChat,
	})
	require.NoError(t, err)
	require.NotNil(t, plan)
	got := map[int64]string{}
	for _, candidate := range plan.Candidates() {
		got[candidate.LeafGroupID] = candidate.Platform
	}
	require.Equal(t, "gemini", got[114])
	require.NotContains(t, got, int64(17))
}

func TestAdaptivePlanPrefersMappedLeafOverEmptyMappingOpenAILeaf(t *testing.T) {
	t.Parallel()

	emptyOpenAI := testAdaptiveLeafAccount(201, PlatformOpenAI)
	emptyOpenAI.Credentials = map[string]any{}
	mappedOpenAI := testAdaptiveLeafAccount(202, PlatformOpenAI)
	mappedOpenAI.Credentials = map[string]any{
		"model_mapping": map[string]any{
			"gpt-6-astra":   "gpt-6-astra",
			"gpt-5.6-terra": "gpt-5.6-terra",
		},
	}

	cheapEmpty := testAdaptiveLeafGroup(17, PlatformOpenAI, true, false)
	cheapEmpty.RateMultiplier = 0.05
	mappedLeaf := testAdaptiveLeafGroup(85, PlatformOpenAI, true, false)
	mappedLeaf.RateMultiplier = 0.08

	planner := NewAdaptiveRoutePlanner(
		stubAdaptivePoolRepo{snapshot: &AdaptivePoolSnapshot{
			ParentGroupID:    108,
			Platform:         PlatformAdaptive,
			Enabled:          true,
			ConfigGeneration: 1,
			Members: []AdaptiveLeafRef{
				{LeafGroupID: 17, Enabled: true, SortOrder: 1},
				{LeafGroupID: 85, Enabled: true, SortOrder: 2},
			},
		}},
		stubAdaptiveAccountRepo{byGroup: map[int64][]Account{
			17: {emptyOpenAI},
			85: {mappedOpenAI},
		}},
		stubAdaptiveGroupRepo{groups: map[int64]*Group{
			17: cheapEmpty,
			85: mappedLeaf,
		}},
		stubAdaptiveChannelRepo{},
		nil,
	)

	plan, err := planner.Plan(context.Background(), AdaptiveRouteRequest{
		ParentGroupID:  108,
		Platform:       PlatformOpenAI,
		RequestedModel: "gpt-6-astra",
		Mode:           AdaptiveRouteModePrice,
		Protocol:       AdaptiveRouteProtocolOpenAIChat,
	})
	require.NoError(t, err)
	require.NotNil(t, plan)
	cands := plan.Candidates()
	require.GreaterOrEqual(t, len(cands), 1)
	require.Equal(t, int64(85), cands[0].LeafGroupID)
	require.Equal(t, 1, cands[0].MappedAccountCount)
}

func TestIsOpenAICompatibleLeafPlatformDoesNotCollapseAnthropic(t *testing.T) {
	t.Parallel()
	require.True(t, IsOpenAICompatibleLeafPlatform(PlatformOpenAI))
	require.True(t, IsOpenAICompatibleLeafPlatform(PlatformCN))
	require.True(t, IsOpenAICompatibleLeafPlatform(PlatformVideo))
	require.True(t, IsOpenAICompatibleLeafPlatform(PlatformKimi))
	require.False(t, IsOpenAICompatibleLeafPlatform(PlatformAnthropic))
	require.False(t, IsOpenAICompatibleLeafPlatform(PlatformGemini))
	require.Equal(t, PlatformOpenAI, NormalizeOpenAICompatiblePlatform(PlatformAnthropic))
}
