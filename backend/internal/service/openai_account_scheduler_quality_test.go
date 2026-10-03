package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestOpenAIAccountRuntimeStats_PerformanceClassesAreIsolated(t *testing.T) {
	stats := newOpenAIAccountRuntimeStats()
	fast := 3200
	slow := 18_000
	stats.reportForModelClass(1001, "gpt-5.6-sol", "effort=max;context=small", true, &fast)
	stats.reportForModelClass(1001, "gpt-5.6-sol", "effort=xhigh;context=large", true, &slow)

	_, ttft, hasTTFT := stats.snapshotForModelClass(1001, "gpt-5.6-sol", "effort=max;context=small")
	require.True(t, hasTTFT)
	require.InDelta(t, 3200.0, ttft, 1e-9)

	_, ttft, hasTTFT = stats.snapshotForModelClass(1001, "gpt-5.6-sol", "effort=xhigh;context=large")
	require.True(t, hasTTFT)
	require.InDelta(t, 18_000.0, ttft, 1e-9)
}

func TestOpenAIAccountRuntimeStats_PerformanceClassProbeLifecycle(t *testing.T) {
	stats := newOpenAIAccountRuntimeStats()
	class := "effort=medium;tier=default;context=large;chain=standalone"

	require.True(t, stats.tryBeginModelClassProbe(1001, "gpt-5.6-sol", class))
	require.True(t, stats.isModelClassProbePending(1001, "gpt-5.6-sol", class))
	require.False(t, stats.tryBeginModelClassProbe(1001, "gpt-5.6-sol", class))

	ttft := 3200
	stats.reportForModelClass(1001, "gpt-5.6-sol", class, true, &ttft)
	require.False(t, stats.isModelClassProbePending(1001, "gpt-5.6-sol", class))
	require.False(t, stats.needsModelClassProbe(1001, "gpt-5.6-sol", class))
}

func TestOpenAIAccountRuntimeStats_PerformanceClassProbeClaimIsAtomic(t *testing.T) {
	stats := newOpenAIAccountRuntimeStats()
	class := "effort=medium;tier=default;context=large;chain=standalone"
	const contenders = 32
	start := make(chan struct{})
	results := make(chan bool, contenders)
	var wg sync.WaitGroup

	for i := 0; i < contenders; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results <- stats.tryBeginModelClassProbe(1001, "gpt-5.6-sol", class)
		}()
	}
	close(start)
	wg.Wait()
	close(results)

	claimed := 0
	for result := range results {
		if result {
			claimed++
		}
	}
	require.Equal(t, 1, claimed)
}

func TestBuildOpenAIAccountLoadPlanMovesClassTTFTOutlierToOverflow(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.OpenAIWS.LBTopK = 3
	cfg.Gateway.OpenAIWS.SchedulerScoreWeights.TTFT = 8
	stats := newOpenAIAccountRuntimeStats()
	fast := 3000
	stable := 4200
	slow := 20_000
	class := "effort=xhigh;tier=default;context=large;chain=standalone"
	stats.reportForModelClass(1001, "gpt-5.6-sol", class, true, &fast)
	stats.reportForModelClass(1002, "gpt-5.6-sol", class, true, &stable)
	stats.reportForModelClass(1003, "gpt-5.6-sol", class, true, &slow)
	scheduler := &defaultOpenAIAccountScheduler{
		service: &OpenAIGatewayService{cfg: cfg},
		stats:   stats,
	}
	accounts := []*Account{{ID: 1001}, {ID: 1002}, {ID: 1003}}

	plan := scheduler.buildOpenAIAccountLoadPlan(context.Background(), OpenAIAccountScheduleRequest{
		RequestedModel:   "gpt-5.6-sol",
		PerformanceClass: class,
	}, accounts, map[int64]*AccountLoadInfo{})

	require.True(t, plan.includeTTFTOverflowFallback)
	require.False(t, plan.candidates[0].ttftOutlier)
	require.False(t, plan.candidates[1].ttftOutlier)
	require.True(t, plan.candidates[2].ttftOutlier)
	require.Len(t, plan.selectionOrder, 3)
	require.Equal(t, int64(1003), plan.selectionOrder[2].account.ID)
	for _, candidate := range plan.selectionOrder[:2] {
		require.NotEqual(t, int64(1003), candidate.account.ID)
	}
}

func TestBuildOpenAIAccountLoadPlanKeepsSlowCandidatePrimaryWithoutTwoHealthyAlternatives(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.OpenAIWS.LBTopK = 2
	cfg.Gateway.OpenAIWS.SchedulerScoreWeights.TTFT = 8
	stats := newOpenAIAccountRuntimeStats()
	fast := 3000
	slow := 20_000
	class := "effort=high;tier=default;context=large;chain=standalone"
	stats.reportForModelClass(1001, "gpt-5.6-sol", class, true, &fast)
	stats.reportForModelClass(1002, "gpt-5.6-sol", class, true, &slow)
	scheduler := &defaultOpenAIAccountScheduler{
		service: &OpenAIGatewayService{cfg: cfg},
		stats:   stats,
	}
	accounts := []*Account{{ID: 1001}, {ID: 1002}}

	plan := scheduler.buildOpenAIAccountLoadPlan(context.Background(), OpenAIAccountScheduleRequest{
		RequestedModel:   "gpt-5.6-sol",
		PerformanceClass: class,
	}, accounts, map[int64]*AccountLoadInfo{})

	require.False(t, plan.includeTTFTOverflowFallback)
	require.False(t, plan.candidates[0].ttftOutlier)
	require.False(t, plan.candidates[1].ttftOutlier)
	require.Len(t, plan.selectionOrder, 2)
}

func TestBuildOpenAIAccountLoadPlanMovesPendingClassProbeToOverflow(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.OpenAIWS.LBTopK = 3
	stats := newOpenAIAccountRuntimeStats()
	class := "effort=medium;tier=default;context=large;chain=standalone"
	require.True(t, stats.tryBeginModelClassProbe(1003, "gpt-5.6-sol", class))
	scheduler := &defaultOpenAIAccountScheduler{
		service: &OpenAIGatewayService{cfg: cfg},
		stats:   stats,
	}
	accounts := []*Account{{ID: 1001}, {ID: 1002}, {ID: 1003}}

	plan := scheduler.buildOpenAIAccountLoadPlan(context.Background(), OpenAIAccountScheduleRequest{
		RequestedModel:   "gpt-5.6-sol",
		PerformanceClass: class,
	}, accounts, map[int64]*AccountLoadInfo{})

	require.True(t, plan.includeTTFTProbeOverflowFallback)
	require.Len(t, plan.selectionOrder, 3)
	require.Equal(t, int64(1003), plan.selectionOrder[2].account.ID)
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_SessionStickyEscapeDoesNotReselectEscapedAccount(t *testing.T) {
	ctx := context.Background()
	groupID := int64(10121)
	accounts := []Account{
		{
			ID:          22101,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    0,
			GroupIDs:    []int64{groupID},
		},
		{
			ID:          22102,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    9,
			GroupIDs:    []int64{groupID},
		},
	}
	cache := &schedulerTestGatewayCache{sessionBindings: map[string]int64{"openai:session_hash_sticky_no_reselect": 22101}}
	cfg := &config.Config{}
	cfg.Gateway.OpenAIScheduler.StickyEscapeEnabled = true
	cfg.Gateway.OpenAIScheduler.StickyEscapeTTFTMs = 15000
	cfg.Gateway.OpenAIScheduler.StickyEscapeErrorRate = 0.5
	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: accounts},
		cache:              cache,
		cfg:                cfg,
		rateLimitService:   newOpenAIAdvancedSchedulerRateLimitService("true"),
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{acquireResults: map[int64]bool{22102: false}}),
		openaiAccountStats: newOpenAIAccountRuntimeStats(),
	}
	slowTTFT := 20000
	for i := 0; i < 4; i++ {
		svc.openaiAccountStats.report(22101, true, &slowTTFT)
	}

	selection, decision, err := svc.SelectAccountWithScheduler(ctx, &groupID, "", "session_hash_sticky_no_reselect", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, int64(22102), selection.Account.ID, "escaped sticky account must stay excluded from load-balance")
	require.Equal(t, openAIAccountScheduleLayerLoadBalance, decision.Layer)
	require.False(t, decision.StickySessionHit)
	require.Equal(t, int64(22101), cache.sessionBindings["openai:session_hash_sticky_no_reselect"])
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_WeightedStickyPreEscapesBadSessionAccount(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()
	defer resetOpenAIAdvancedSchedulerSettingCacheForTest()

	ctx := context.Background()
	groupID := int64(10122)
	accounts := []Account{
		{
			ID:          22201,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    0,
			GroupIDs:    []int64{groupID},
		},
		{
			ID:          22202,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    9,
			GroupIDs:    []int64{groupID},
		},
	}
	cfg := &config.Config{}
	cfg.Gateway.OpenAIScheduler.StickyEscapeEnabled = true
	cfg.Gateway.OpenAIScheduler.StickyEscapeTTFTMs = 15000
	cfg.Gateway.OpenAIScheduler.StickyEscapeErrorRate = 0.5
	cfg.Gateway.OpenAIWS.LBTopK = 2
	cfg.Gateway.OpenAIWS.SchedulerScoreWeights.Priority = 1
	cfg.Gateway.OpenAIWS.SchedulerScoreWeights.Load = 1
	cfg.Gateway.OpenAIWS.SchedulerScoreWeights.Queue = 0.7
	cfg.Gateway.OpenAIWS.SchedulerScoreWeights.ErrorRate = 0.8
	cfg.Gateway.OpenAIWS.SchedulerScoreWeights.TTFT = 0.5
	cfg.Gateway.OpenAIWS.SchedulerScoreWeights.SessionSticky = 3
	cache := &schedulerTestGatewayCache{sessionBindings: map[string]int64{
		"openai:session_hash_weighted_preescape": 22201,
	}}
	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: accounts},
		cache:              cache,
		cfg:                cfg,
		rateLimitService:   newOpenAIAdvancedSchedulerRateLimitService("true", "true"),
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
		openaiAccountStats: newOpenAIAccountRuntimeStats(),
	}
	slowTTFT := 20000
	for i := 0; i < 4; i++ {
		svc.openaiAccountStats.report(22201, true, &slowTTFT)
	}

	scheduler := newDefaultOpenAIAccountScheduler(svc, svc.openaiAccountStats).(*defaultOpenAIAccountScheduler)
	escaped := scheduler.applyWeightedStickyEscape(OpenAIAccountScheduleRequest{
		StickyAccountID: 22201,
		StickyWeighted:  true,
	})
	require.Contains(t, escaped.StickyEscapeFallbackIDs, int64(22201))
	require.True(t, escaped.PreserveStickyBinding)

	selection, decision, err := svc.SelectAccountWithScheduler(
		ctx,
		&groupID,
		"",
		"session_hash_weighted_preescape",
		"gpt-5.1",
		nil,
		OpenAIUpstreamTransportAny,
		false,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, int64(22202), selection.Account.ID)
	require.Equal(t, openAIAccountScheduleLayerLoadBalance, decision.Layer)
	require.False(t, decision.StickySessionHit)
	require.Equal(t, int64(22201), cache.sessionBindings["openai:session_hash_weighted_preescape"])
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_WeightedStickyFallbackSkipsEscapedAccount(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()
	defer resetOpenAIAdvancedSchedulerSettingCacheForTest()

	ctx := context.Background()
	groupID := int64(10123)
	accounts := []Account{
		{
			ID:          22301,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    0,
			GroupIDs:    []int64{groupID},
		},
		{
			ID:          22302,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    9,
			GroupIDs:    []int64{groupID},
		},
	}
	cfg := &config.Config{}
	cfg.Gateway.OpenAIScheduler.StickyEscapeEnabled = true
	cfg.Gateway.OpenAIScheduler.StickyEscapeTTFTMs = 15000
	cfg.Gateway.OpenAIScheduler.StickyEscapeErrorRate = 0.5
	cfg.Gateway.OpenAIWS.LBTopK = 2
	cfg.Gateway.OpenAIWS.SchedulerScoreWeights.Priority = 1
	cfg.Gateway.OpenAIWS.SchedulerScoreWeights.Load = 1
	cfg.Gateway.OpenAIWS.SchedulerScoreWeights.Queue = 0.7
	cfg.Gateway.OpenAIWS.SchedulerScoreWeights.ErrorRate = 0.8
	cfg.Gateway.OpenAIWS.SchedulerScoreWeights.TTFT = 0.5
	cfg.Gateway.OpenAIWS.SchedulerScoreWeights.SessionSticky = 3
	cache := &schedulerTestGatewayCache{sessionBindings: map[string]int64{
		"openai:session_hash_weighted_fallback_escape": 22301,
	}}
	svc := &OpenAIGatewayService{
		accountRepo:      schedulerTestOpenAIAccountRepo{accounts: accounts},
		cache:            cache,
		cfg:              cfg,
		rateLimitService: newOpenAIAdvancedSchedulerRateLimitService("true", "true"),
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{
			acquireResults: map[int64]bool{22302: false},
		}),
		openaiAccountStats: newOpenAIAccountRuntimeStats(),
	}
	slowTTFT := 20000
	for i := 0; i < 4; i++ {
		svc.openaiAccountStats.report(22301, true, &slowTTFT)
	}

	selection, decision, err := svc.SelectAccountWithScheduler(
		ctx,
		&groupID,
		"",
		"session_hash_weighted_fallback_escape",
		"gpt-5.1",
		nil,
		OpenAIUpstreamTransportAny,
		false,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.NotEqual(t, int64(22301), selection.Account.ID, "escaped sticky account must not be reselected via weighted fallback")
	require.Equal(t, int64(22302), selection.Account.ID)
	require.Equal(t, openAIAccountScheduleLayerLoadBalance, decision.Layer)
	require.False(t, decision.StickySessionHit)
	require.Equal(t, int64(22301), cache.sessionBindings["openai:session_hash_weighted_fallback_escape"])
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func TestOpenAIGatewayService_ReportOpenAIAccountModelScheduleResultWithClass(t *testing.T) {
	svc := &OpenAIGatewayService{openaiAccountStats: newOpenAIAccountRuntimeStats()}
	fast := 2400
	svc.ReportOpenAIAccountModelScheduleResultWithClass(1001, "gpt-5.6-sol", "effort=max;context=small", true, &fast)

	_, ttft, hasTTFT := svc.openaiAccountStats.snapshotForModelClass(1001, "gpt-5.6-sol", "effort=max;context=small")
	require.True(t, hasTTFT)
	require.InDelta(t, 2400.0, ttft, 1e-9)

	_, accountTTFT, hasAccountTTFT := svc.openaiAccountStats.snapshot(1001)
	require.True(t, hasAccountTTFT)
	require.InDelta(t, 2400.0, accountTTFT, 1e-9)
}

func TestOpenAIAccountRuntimeStats_StalePerformanceClassReturnsToNeutral(t *testing.T) {
	stats := newOpenAIAccountRuntimeStats()
	class := "effort=medium;context=large"
	slow := 22_000
	stats.reportForModelClass(1001, "gpt-5.6-sol", class, false, &slow)

	key := openAIAccountModelRuntimeStatKey{accountID: 1001, model: "gpt-5.6-sol", performanceClass: class}
	value, ok := stats.models.Load(key)
	require.True(t, ok)
	stat, ok := value.(*openAIAccountRuntimeStat)
	require.True(t, ok)
	stat.updatedAtUnixNano.Store(time.Now().Add(-openAIAccountRuntimeStatsSampleTTL - time.Second).UnixNano())

	errorRate, ttft, hasTTFT := stats.snapshotForModelClass(1001, "gpt-5.6-sol", class)
	require.Zero(t, errorRate)
	require.Zero(t, ttft)
	require.False(t, hasTTFT)
}
