// 数据面：用量落库与计费。
package gateway

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/bejix/upstream-ops/backend/storage"
	"github.com/gin-gonic/gin"
)

func (rt *Runtime) recordUsage(
	key *storage.GatewayKey,
	group *storage.GatewayGroup,
	route *storage.GatewayRoute,
	target *upstreamTarget,
	reqID, requestedModel, upstreamModel, chain string,
	tokens UsageTokens,
	rate, billingRate float64,
	stream bool,
	status int,
	success bool,
	errInfo usageErrorInfo,
	durationMS int64,
	firstTokenMS *int64,
	c *gin.Context,
	meta usageRecordMeta,
) {
	priceModel := upstreamModel
	if priceModel == "" {
		priceModel = requestedModel
	}
	// OpenAI total input includes cache details; Anthropic input_tokens is
	// already exclusive and must remain unchanged.
	tokens = NormalizeUsageBucketsForProtocol(tokens, meta.UpstreamProtocol)
	pricing := rt.Pricing.Resolve(priceModel)
	cost := CalculateCost(pricing, tokens, rate, billingRate)
	reqType := storage.GatewayRequestTypeSync
	if stream {
		reqType = storage.GatewayRequestTypeStream
	}
	billingMode := "token"
	if tokens.ImageOutputTokens > 0 {
		billingMode = "image"
	}
	sourceGroupName := strings.TrimSpace(route.SourceGroupName)
	if sourceGroupName == "" || rt.isSourceGroupIDPlaceholder(sourceGroupName) {
		// 运行时再解析一次：旧路由可能只有 id、name 为空
		if route.SourceGroupID != nil && *route.SourceGroupID > 0 && route.SourceChannelID > 0 {
			if gs := rt.loadGroupsByChannel(context.Background(), []storage.GatewayRoute{*route}); len(gs) > 0 {
				tmp := *route
				rt.enrichRouteSourceGroupName(&tmp, gs[route.SourceChannelID])
				if n := strings.TrimSpace(tmp.SourceGroupName); n != "" && !rt.isSourceGroupIDPlaceholder(n) {
					sourceGroupName = n
				}
			}
		}
	}
	if sourceGroupName == "" && route.SourceGroupID != nil {
		sourceGroupName = fmt.Sprintf("id:%d", *route.SourceGroupID)
	}
	var channelID uint
	var providerID uint
	providerName := ""
	sourceKeyName := strings.TrimSpace(route.SourceAPIKeyName)
	sourceKeyID := route.SourceAPIKeyID
	if target != nil {
		if target.Channel != nil {
			channelID = target.Channel.ID
		}
		if target.Provider != nil {
			providerID = target.Provider.ID
			providerName = target.Provider.Name
			if sourceKeyName == "" {
				sourceKeyName = target.Provider.APIKeyHint
			}
		}
	} else if route != nil {
		channelID = route.SourceChannelID
		providerID = route.GatewayProviderID
	}
	attempt := meta.Attempt
	if attempt <= 0 {
		attempt = 1
	}
	attemptKind := strings.TrimSpace(meta.AttemptKind)
	if attemptKind == "" {
		attemptKind = attemptKindPrimary
	}
	item := &storage.GatewayUsageLog{
		GatewayGroupID:    group.ID,
		GatewayKeyID:      key.ID,
		RouteID:           route.ID,
		ChannelID:         channelID,
		GatewayProviderID: providerID,
		ProviderName:      providerName,
		// 路由快照：保存路由换 id 后历史记录仍可展示
		SourceAPIKeyID:        sourceKeyID,
		SourceAPIKeyName:      sourceKeyName,
		SourceGroupID:         route.SourceGroupID,
		SourceGroupName:       sourceGroupName,
		RequestID:             reqID,
		Attempt:               attempt,
		AttemptKind:           attemptKind,
		CooldownUntil:         meta.CooldownUntil,
		RequestedModel:        requestedModel,
		UpstreamModel:         upstreamModel,
		ModelMappingChain:     chain,
		InboundEndpoint:       meta.InboundEndpoint,
		UpstreamEndpoint:      meta.UpstreamEndpoint,
		InboundProtocol:       meta.InboundProtocol,
		UpstreamProtocol:      meta.UpstreamProtocol,
		ProtocolConverted:     meta.ProtocolConverted,
		RequestType:           reqType,
		ServiceTier:           meta.ServiceTier,
		ReasoningEffort:       meta.ReasoningEffort,
		BillingMode:           billingMode,
		InputTokens:           tokens.InputTokens,
		OutputTokens:          tokens.OutputTokens,
		CacheCreationTokens:   tokens.CacheCreationTokens,
		CacheReadTokens:       tokens.CacheReadTokens,
		CacheCreation5mTokens: tokens.CacheCreation5mTokens,
		CacheCreation1hTokens: tokens.CacheCreation1hTokens,
		ImageOutputTokens:     tokens.ImageOutputTokens,
		ReasoningTokens:       tokens.ReasoningTokens,
		InputCost:             cost.InputCost,
		OutputCost:            cost.OutputCost,
		CacheCreationCost:     cost.CacheCreationCost,
		CacheReadCost:         cost.CacheReadCost,
		ImageOutputCost:       cost.ImageOutputCost,
		TotalCost:             cost.TotalCost,
		ActualCost:            cost.ActualCost,
		RateMultiplier:        rate,
		BillingRateMultiplier: billingRate,
		// 与上游同步账号计费倍率一致：账户侧统计用换算后的有效倍率，而非独立默认 1
		AccountRateMultiplier: rate,
		Stream:                stream,
		StatusCode:            status,
		Success:               success,
		ErrorMessage:          rt.truncateRunes(errInfo.Summary, rt.gatewayRuntime().UsageErrorMsgRunes),
		ErrorType:             errInfo.Type,
		ErrorDetail:           errInfo.Detail,
		UpstreamURL:           meta.UpstreamURL,
		UpstreamErrorBody:     errInfo.UpstreamBody,
		UpstreamErrorHeaders:  errInfo.UpstreamHeaders,
		DurationMS:            durationMS,
		FirstTokenMS:          firstTokenMS,
		IPAddress:             c.ClientIP(),
		UserAgent:             c.Request.UserAgent(),
		CreatedAt:             time.Now(),
	}
	if err := rt.Usage.Create(item); err != nil && rt.Log != nil {
		rt.Log.Error("write usage log failed", "err", err)
	}
	if success && cost.ActualCost > 0 {
		_ = rt.Keys.AddQuotaUsed(key.ID, cost.ActualCost)
	}
}

// buildUpstreamErrorInfo 从转发失败结果拼装可落库的详细错误（截断上限用默认值，便于单测）。
