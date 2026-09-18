package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestBuildOpenAIAccountLoadPlanMovesModelTTFTOutlierToOverflowWithoutPerformanceClass(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.OpenAIWS.LBTopK = 3
	cfg.Gateway.OpenAIWS.SchedulerScoreWeights.TTFT = 8
	stats := newOpenAIAccountRuntimeStats()
	fast := 3000
	stable := 4200
	slow := 20_000
	stats.reportForModelClass(1001, "gpt-5.6-sol", "", true, &fast)
	stats.reportForModelClass(1002, "gpt-5.6-sol", "", true, &stable)
	stats.reportForModelClass(1003, "gpt-5.6-sol", "", true, &slow)
	scheduler := &defaultOpenAIAccountScheduler{
		service: &OpenAIGatewayService{cfg: cfg},
		stats:   stats,
	}
	accounts := []*Account{{ID: 1001}, {ID: 1002}, {ID: 1003}}

	plan := scheduler.buildOpenAIAccountLoadPlan(context.Background(), OpenAIAccountScheduleRequest{
		RequestedModel: "gpt-5.6-sol",
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

func TestReportOpenAIAccountScheduleResult_UsesFailoverFirstTokenMs(t *testing.T) {
	svc := &OpenAIGatewayService{openaiAccountStats: newOpenAIAccountRuntimeStats()}
	err := &UpstreamFailoverError{StatusCode: 504, FirstTokenMs: 60_000}

	svc.ReportOpenAIAccountScheduleResult(&Account{ID: 2175}, "gpt-5.6-sol", false, nil, err)

	_, ttft, hasTTFT := svc.openaiAccountStats.snapshotForModelClass(2175, "gpt-5.6-sol", "")
	require.True(t, hasTTFT)
	require.InDelta(t, 60_000.0, ttft, 1e-9)
	errorRate, _, _ := svc.openaiAccountStats.snapshot(2175)
	require.InDelta(t, 0.2, errorRate, 1e-9)
}

func TestBuildOpenAIAccountLoadPlanMovesHighErrorRateAccountsToOverflow(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.OpenAIWS.LBTopK = 2
	cfg.Gateway.OpenAIWS.SchedulerScoreWeights.ErrorRate = 8
	stats := newOpenAIAccountRuntimeStats()
	good := true
	bad := false
	stats.reportForModelClass(2001, "gpt-5.6-sol", "", good, nil)
	stats.reportForModelClass(2002, "gpt-5.6-sol", "", good, nil)
	for i := 0; i < 4; i++ {
		stats.reportForModelClass(2003, "gpt-5.6-sol", "", bad, nil)
	}
	scheduler := &defaultOpenAIAccountScheduler{
		service: &OpenAIGatewayService{cfg: cfg},
		stats:   stats,
	}
	accounts := []*Account{{ID: 2001}, {ID: 2002}, {ID: 2003}}

	plan := scheduler.buildOpenAIAccountLoadPlan(context.Background(), OpenAIAccountScheduleRequest{
		RequestedModel: "gpt-5.6-sol",
	}, accounts, map[int64]*AccountLoadInfo{})

	require.True(t, plan.includeErrorRateOverflowFallback)
	require.Len(t, plan.selectionOrder, 3)
	require.Equal(t, int64(2003), plan.selectionOrder[2].account.ID)
}
