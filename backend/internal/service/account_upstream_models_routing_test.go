//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func observedModelTestAccount(id int64, platform string, models []string) Account {
	account := Account{ID: id, Platform: platform, Type: AccountTypeAPIKey,
		Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: int(id)}
	if models != nil {
		account.Extra = map[string]any{UpstreamModelsExtraKey: models, UpstreamModelsSyncStatusExtraKey: "fresh"}
	}
	return account
}

func TestObservedModels_AllGroupAccountGates(t *testing.T) {
	ctx := context.Background()
	for _, platform := range []string{PlatformOpenAI, PlatformAnthropic, PlatformGemini} {
		t.Run(platform, func(t *testing.T) {
			for _, snapshot := range []struct {
				name   string
				models []string
				want   bool
			}{
				{"unknown", nil, true},
				{"supported", []string{"gpt-5.4"}, true},
				{"unsupported", []string{"other"}, false},
			} {
				t.Run(snapshot.name, func(t *testing.T) {
					account := observedModelTestAccount(1, platform, snapshot.models)
					require.Equal(t, snapshot.want, (&GatewayService{}).isModelSupportedByAccountWithContext(ctx, &account, "gpt-5.4"), "ordinary/mixed/fallback gate")
					require.Equal(t, snapshot.want, adaptiveRouteAccountCompatible(ctx, &account, &Group{}, platform, "gpt-5.4", AdaptiveRouteRequest{}), "adaptive leaf gate")
					if platform == PlatformOpenAI {
						require.Equal(t, snapshot.want, isOpenAICompatibleAccountEligibleForRequestBeforeProfit(ctx, &account, platform, "gpt-5.4", false, OpenAIEndpointCapabilityChatCompletions))
					}
					if platform == PlatformGemini {
						require.Equal(t, snapshot.want, (&GeminiMessagesCompatService{}).isAccountUsableForRequest(ctx, &account, "gpt-5.4", platform, false))
					}
				})
			}
		})
	}
}

func TestObservedModels_OpenAIWireMapping(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		t.Run(map[bool]string{false: "mapped", true: "passthrough"}[passthrough], func(t *testing.T) {
			account := observedModelTestAccount(1, PlatformOpenAI, []string{"gpt-5.5"})
			account.Credentials = map[string]any{"model_mapping": map[string]any{"gpt-5.4": "gpt-5.5"}}
			account.Extra["openai_passthrough"] = passthrough
			ctx := context.Background()
			require.Equal(t, !passthrough, isOpenAICompatibleAccountEligibleForRequestBeforeProfit(ctx, &account, PlatformOpenAI, "gpt-5.4", false, OpenAIEndpointCapabilityChatCompletions))
			account.Extra[UpstreamModelsExtraKey] = []string{"gpt-5.4"}
			require.Equal(t, passthrough, isOpenAICompatibleAccountEligibleForRequestBeforeProfit(ctx, &account, PlatformOpenAI, "gpt-5.4", false, OpenAIEndpointCapabilityChatCompletions))
		})
	}
}

func TestObservedModels_OrdinaryAndStickySelection(t *testing.T) {
	for _, groupID := range []int64{14, 85} {
		for _, session := range []string{"", "sticky"} {
			accounts := []Account{observedModelTestAccount(1, PlatformAnthropic, []string{"other"}), observedModelTestAccount(2, PlatformAnthropic, []string{"claude-sonnet-4-6"})}
			repo := &mockAccountRepoForPlatform{accounts: accounts, accountsByID: map[int64]*Account{}}
			for i := range repo.accounts {
				repo.accounts[i].AccountGroups = []AccountGroup{{GroupID: groupID}}
				repo.accountsByID[repo.accounts[i].ID] = &repo.accounts[i]
			}
			svc := &GatewayService{accountRepo: repo, groupRepo: &mockGroupRepoForGateway{groups: map[int64]*Group{groupID: {ID: groupID, Platform: PlatformAnthropic}}}, cache: &mockGatewayCacheForPlatform{sessionBindings: map[string]int64{"sticky": 1}}, cfg: testConfig()}
			selected, err := svc.selectAccountForModelWithPlatform(context.Background(), &groupID, session, "claude-sonnet-4-6", nil, PlatformAnthropic)
			require.NoError(t, err)
			require.Equal(t, int64(2), selected.ID, "group %d session %q must not bypass capability", groupID, session)
		}
	}
}

func TestObservedModels_OpenAIAdvancedAndLegacySelection(t *testing.T) {
	for _, advanced := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy", true: "advanced"}[advanced], func(t *testing.T) {
			resetOpenAIAdvancedSchedulerSettingCacheForTest()
			defer resetOpenAIAdvancedSchedulerSettingCacheForTest()
			accounts := []Account{observedModelTestAccount(1, PlatformOpenAI, []string{"other"}), observedModelTestAccount(2, PlatformOpenAI, []string{"gpt-5.4"})}
			newService := func(accounts []Account) *OpenAIGatewayService {
				svc := &OpenAIGatewayService{accountRepo: schedulerTestOpenAIAccountRepo{accounts: accounts}, cache: &schedulerTestGatewayCache{}, cfg: &config.Config{}, concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{})}
				if advanced {
					svc.rateLimitService = newOpenAIAdvancedSchedulerRateLimitService("true")
				}
				return svc
			}
			groupID := int64(85)
			selected, _, err := newService(accounts).SelectAccountWithScheduler(context.Background(), &groupID, "", "", "gpt-5.4", nil, OpenAIUpstreamTransportAny, false)
			require.NoError(t, err)
			require.NotNil(t, selected)
			require.Equal(t, int64(2), selected.Account.ID)
			if selected.ReleaseFunc != nil {
				selected.ReleaseFunc()
			}
			unknown := observedModelTestAccount(3, PlatformOpenAI, nil)
			selected, _, err = newService([]Account{unknown}).SelectAccountWithScheduler(context.Background(), &groupID, "", "", "gpt-5.4", nil, OpenAIUpstreamTransportAny, false)
			require.NoError(t, err)
			require.Equal(t, unknown.ID, selected.Account.ID)
			if selected.ReleaseFunc != nil {
				selected.ReleaseFunc()
			}
		})
	}
}
