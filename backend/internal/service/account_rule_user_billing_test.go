//go:build unit

package service

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAccountRuleUserBilling_GatewayCosts(t *testing.T) {
	ctx := context.Background()
	rules := []AccountStatsPricingRule{
		{AccountIDs: []int64{11}, ApplyToUserBilling: true, Pricing: []ChannelModelPricing{{
			Platform: "anthropic", Models: []string{"supplier-v4"}, BillingMode: BillingModeToken,
			InputPrice: testPtrFloat64(4.5e-6), OutputPrice: testPtrFloat64(13.5e-6),
		}}},
		{AccountIDs: []int64{12}, ApplyToUserBilling: true, Pricing: []ChannelModelPricing{{
			Platform: "anthropic", Models: []string{"supplier-v4"}, BillingMode: BillingModeToken,
			InputPrice: testPtrFloat64(9e-6), OutputPrice: testPtrFloat64(27e-6),
		}}},
	}
	r := newResolverWithChannelAndAccountRules(t, []ChannelModelPricing{{
		Platform: "anthropic", Models: []string{"public-v4"}, BillingMode: BillingModeToken,
		InputPrice: testPtrFloat64(1e-6), OutputPrice: testPtrFloat64(2e-6),
	}}, rules)
	key := &APIKey{GroupID: groupIDPtr(), Group: &Group{ID: 100, Platform: "anthropic"}}
	gateway := &GatewayService{resolver: r, billingService: r.billingService}
	openAI := &OpenAIGatewayService{resolver: r, billingService: r.billingService}
	for _, tc := range []struct {
		id   int64
		base float64
	}{{11, 0.1745775}, {12, 0.349155}, {13, 0.037675}} {
		t.Run(strconv.FormatInt(tc.id, 10), func(t *testing.T) {
			pc := pricingContextForAccount(&Account{ID: tc.id}, "supplier-v4")
			for _, multiplier := range []float64{0.25, 0} {
				cost := gateway.calculateRecordUsageCost(ctx, &ForwardResult{
					Usage: ClaudeUsage{InputTokens: 35435, OutputTokens: 1120},
				}, key, "public-v4", multiplier, multiplier, time.Time{}, &recordUsageOpts{}, pc)
				require.InDelta(t, tc.base, cost.TotalCost, 1e-12)
				require.InDelta(t, tc.base*multiplier, cost.ActualCost, 1e-12)
				other, err := openAI.calculateOpenAIRecordUsageCost(ctx, &OpenAIForwardResult{}, key,
					[]string{"public-v4"}, multiplier, multiplier, multiplier, multiplier,
					UsageTokens{InputTokens: 35435, OutputTokens: 1120}, "", nil, time.Time{}, pc)
				require.NoError(t, err)
				require.InDelta(t, cost.TotalCost, other.TotalCost, 1e-12)
				require.InDelta(t, cost.ActualCost, other.ActualCost, 1e-12)
			}
		})
	}
}

func TestAccountRuleUserBilling_NonBillableFreeFast(t *testing.T) {
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	userRepo := &openAIRecordUsageUserRepoStub{}
	svc := newOpenAIRecordUsageServiceForTest(usageRepo, userRepo, &openAIRecordUsageSubRepoStub{}, nil)
	r := newResolverWithChannelAndAccountRules(t, nil, []AccountStatsPricingRule{{
		AccountIDs: []int64{11}, ApplyToUserBilling: true,
		Pricing: []ChannelModelPricing{{Platform: "anthropic", Models: []string{"supplier-v4"},
			InputPrice: testPtrFloat64(4.5e-6), OutputPrice: testPtrFloat64(13.5e-6)}},
	}})
	svc.resolver = r
	svc.billingService = r.billingService
	tier := "priority"
	err := svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
		Result: &OpenAIForwardResult{
			RequestID: "nonbillable-account-rule-fast", Model: "public-v4", UpstreamModel: "supplier-v4",
			ServiceTier: &tier, NonBillableUpstreamError: true,
			Usage: OpenAIUsage{InputTokens: 35435}, Duration: time.Second,
		},
		APIKey: &APIKey{ID: 1, GroupID: groupIDPtr(), Group: &Group{ID: 100,
			Platform: PlatformOpenAI, Hydrated: true, RateMultiplier: 0.25, FreeOpenAIFast: true}},
		User: &User{ID: 2}, Account: &Account{ID: 11, Platform: PlatformOpenAI, Type: AccountTypeOAuth},
	})
	require.NoError(t, err)
	require.NotNil(t, usageRepo.lastLog)
	require.Zero(t, usageRepo.lastLog.TotalCost)
	require.Zero(t, usageRepo.lastLog.ActualCost)
	require.Zero(t, userRepo.deductCalls)
}

func TestAccountRuleUserBilling_MediaOverridesGroupPrice(t *testing.T) {
	r := newResolverWithChannelAndAccountRules(t, nil, []AccountStatsPricingRule{{
		AccountIDs: []int64{11}, ApplyToUserBilling: true,
		Pricing: []ChannelModelPricing{
			{Platform: "anthropic", Models: []string{"supplier-image"}, BillingMode: BillingModeImage, PerRequestPrice: testPtrFloat64(0.08)},
			{Platform: "anthropic", Models: []string{"supplier-video"}, BillingMode: BillingModeVideo, PerRequestPrice: testPtrFloat64(0.2)},
		},
	}})
	key := &APIKey{GroupID: groupIDPtr(), Group: &Group{ID: 100, Platform: "anthropic", ImagePrice2K: testPtrFloat64(9), VideoPrice720P: testPtrFloat64(9)}}
	gateway := &GatewayService{resolver: r, billingService: r.billingService}
	openAI := &OpenAIGatewayService{resolver: r, billingService: r.billingService}
	pc := pricingContextForAccount(&Account{ID: 11}, "supplier-image")
	cost := gateway.calculateRecordUsageCost(context.Background(), &ForwardResult{ImageCount: 2, ImageSize: "2K"}, key, "public-image", 0.25, 0.25, time.Time{}, &recordUsageOpts{}, pc)
	require.InDelta(t, 0.16, cost.TotalCost, 1e-12)
	require.InDelta(t, 0.04, cost.ActualCost, 1e-12)
	cost = openAI.calculateOpenAIImageCost(context.Background(), "public-image", key, &OpenAIForwardResult{ImageCount: 2, ImageSize: "2K"}, 0.25, pc)
	require.InDelta(t, 0.16, cost.TotalCost, 1e-12)
	pc.upstreamModel = "supplier-video"
	cost = openAI.calculateOpenAIVideoCost(context.Background(), "public-video", key, &OpenAIForwardResult{VideoCount: 2, VideoResolution: "720p", VideoDurationSeconds: 5}, 0.25, pc)
	require.InDelta(t, 2, cost.TotalCost, 1e-12)
	require.InDelta(t, 0.5, cost.ActualCost, 1e-12)
}

func TestAccountRuleUserBilling_UpstreamBeforeAlias(t *testing.T) {
	r := newResolverWithChannelAndAccountRules(t, nil, []AccountStatsPricingRule{
		{GroupIDs: []int64{100}, ApplyToUserBilling: true, Pricing: []ChannelModelPricing{{Platform: "anthropic", Models: []string{"public-v4"}, InputPrice: testPtrFloat64(1e-6)}}},
		{GroupIDs: []int64{100}, ApplyToUserBilling: true, Pricing: []ChannelModelPricing{{Platform: "anthropic", Models: []string{"supplier-*"}, InputPrice: testPtrFloat64(4.5e-6)}}},
	})
	id := int64(11)
	resolved := r.Resolve(context.Background(), PricingInput{Model: "public-v4", UpstreamModel: "supplier-v4", GroupID: groupIDPtr(), AccountID: &id})
	require.Equal(t, PricingSourceAccountRule, resolved.Source)
	require.InDelta(t, 4.5e-6, resolved.BasePricing.InputPricePerToken, 1e-12)
	resolved = r.Resolve(context.Background(), PricingInput{Model: "public-v4", GroupID: groupIDPtr()})
	require.NotEqual(t, PricingSourceAccountRule, resolved.Source)
}
