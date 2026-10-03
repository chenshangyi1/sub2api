package handler

import (
	"context"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func adaptiveLeafPlatform(apiKey *service.APIKey) string {
	if apiKey != nil && apiKey.Group != nil {
		return apiKey.Group.Platform
	}
	return ""
}

func adaptiveLeafSelectPlatform(apiKey *service.APIKey) string {
	platform := adaptiveLeafPlatform(apiKey)
	if service.IsOpenAICompatibleLeafPlatform(platform) {
		return service.NormalizeOpenAICompatiblePlatform(platform)
	}
	return platform
}

// openAIWSAdaptiveLeafExecutable reports whether a planned Adaptive leaf can
// run on Responses WebSocket. OpenAI-compat leaves (openai/grok/kimi/zhipu/
// deepseek) use the v247 OpenAI scheduler + forwarder. Anthropic/Gemini have
// no WS execution path and must be skipped by the caller. An unresolved group
// (empty platform) stays on the v247 OpenAI scheduler, matching pre-Adaptive WS.
func openAIWSAdaptiveLeafExecutable(apiKey *service.APIKey) bool {
	platform := adaptiveLeafPlatform(apiKey)
	if platform == "" {
		return true
	}
	return service.IsOpenAICompatibleLeafPlatform(platform)
}

// openAIWSAdaptiveImageAllowed mirrors HTTP Responses: Adaptive sessions allow
// image intent when any planned leaf permits it; non-Adaptive uses the key's
// primary group.
func openAIWSAdaptiveImageAllowed(apiKey *service.APIKey, attempts []openAIFallbackGroupAttempt, sess *openAIAdaptiveSession) bool {
	if sess != nil {
		return openAIFallbackAttemptsAllowImageGeneration(attempts)
	}
	if apiKey == nil {
		return true
	}
	return service.GroupAllowsImageGeneration(apiKey.Group)
}

// tryAdaptiveNativeLeafChatCompletions executes one Anthropic/Gemini Adaptive
// leaf from an OpenAI Chat Completions inbound request. switchLeaf means the
// caller should try the next planned leaf; done means a response was written.
func (h *OpenAIGatewayHandler) tryAdaptiveNativeLeafChatCompletions(
	c *gin.Context,
	attemptAPIKey *service.APIKey,
	subscription *service.UserSubscription,
	reqModel string,
	body []byte,
	sessionHash string,
	channelMapping service.ChannelMappingResult,
	pricingAt time.Time,
	reqLog *zap.Logger,
) (switchLeaf bool, done bool) {
	if h == nil || attemptAPIKey == nil || h.anthropicGateway == nil {
		return true, false
	}
	leafPlatform := adaptiveLeafPlatform(attemptAPIKey)
	selection, err := h.anthropicGateway.SelectAccountWithLoadAwareness(
		c.Request.Context(),
		attemptAPIKey.GroupID,
		sessionHash,
		reqModel,
		nil,
		"",
		0,
	)
	if err != nil || selection == nil || selection.Account == nil {
		if reqLog != nil {
			reqLog.Info("openai.adaptive_native_leaf_select_failed",
				zap.String("leaf_platform", leafPlatform),
				zap.Error(err),
			)
		}
		h.markAdaptiveLeafFailed(c.Request.Context(), c, "precommit_upstream", reqLog)
		return true, false
	}
	account := selection.Account
	if selection.ReleaseFunc != nil {
		defer selection.ReleaseFunc()
	}

	forwardBody := body
	if channelMapping.Mapped {
		forwardBody = h.gatewayService.ReplaceModelInBody(body, channelMapping.MappedModel)
	}

	var result *service.ForwardResult
	if account.Platform == service.PlatformGemini && h.geminiCompat != nil {
		result, err = h.geminiCompat.ForwardAsChatCompletions(c.Request.Context(), c, account, forwardBody)
	} else {
		result, err = h.anthropicGateway.ForwardAsChatCompletions(c.Request.Context(), c, account, forwardBody, nil)
	}
	if err != nil || result == nil {
		if reqLog != nil {
			reqLog.Info("openai.adaptive_native_leaf_forward_failed",
				zap.String("leaf_platform", leafPlatform),
				zap.Int64("account_id", account.ID),
				zap.Error(err),
			)
		}
		h.markAdaptiveLeafFailed(c.Request.Context(), c, "precommit_upstream", reqLog)
		return true, false
	}

	adaptiveBillingCtx, adaptiveSession := prepareAdaptiveSessionSettlement(c)
	userAgent := c.GetHeader("User-Agent")
	clientIP := ip.GetClientIP(c)
	inboundEndpoint := GetInboundEndpoint(c)
	upstreamEndpoint := GetUpstreamEndpoint(c, account.Platform)
	quotaPlatform := service.QuotaPlatform(c.Request.Context(), attemptAPIKey)
	sessionID := service.ExtractClientSessionID(c)
	h.submitOpenAIUsageRecordTask(c.Request.Context(), nil, func(ctx context.Context) {
		if recErr := h.anthropicGateway.RecordUsage(ctx, &service.RecordUsageInput{
			Result:             result,
			APIKey:             attemptAPIKey,
			User:               attemptAPIKey.User,
			Account:            account,
			Subscription:       subscription,
			InboundEndpoint:    inboundEndpoint,
			UpstreamEndpoint:   upstreamEndpoint,
			UserAgent:          userAgent,
			IPAddress:          clientIP,
			APIKeyService:      h.apiKeyService,
			QuotaPlatform:      quotaPlatform,
			SessionID:          sessionID,
			ChannelUsageFields: clientRequestedUsageFields(c, channelMapping, reqModel, result.UpstreamModel),
			PricingAt:          pricingAt,
			AdaptiveBilling:    adaptiveBillingCtx,
		}); recErr != nil && reqLog != nil {
			reqLog.Error("openai.adaptive_native_leaf_record_usage_failed",
				zap.Int64("account_id", account.ID),
				zap.Error(recErr),
			)
			return
		}
		if adaptiveSession != nil {
			adaptiveSession.markAdaptiveBillingCaptured()
		}
	})
	return false, true
}

// tryAdaptiveNativeLeafMessages executes one Anthropic/Gemini Adaptive leaf
// from an OpenAI-compatible /v1/messages inbound request.
func (h *OpenAIGatewayHandler) tryAdaptiveNativeLeafMessages(
	c *gin.Context,
	attemptAPIKey *service.APIKey,
	subscription *service.UserSubscription,
	reqModel string,
	body []byte,
	sessionHash string,
	channelMapping service.ChannelMappingResult,
	pricingAt time.Time,
	reqLog *zap.Logger,
) (switchLeaf bool, done bool) {
	if h == nil || attemptAPIKey == nil || h.anthropicGateway == nil {
		return true, false
	}
	leafPlatform := adaptiveLeafPlatform(attemptAPIKey)
	selection, err := h.anthropicGateway.SelectAccountWithLoadAwareness(
		c.Request.Context(),
		attemptAPIKey.GroupID,
		sessionHash,
		reqModel,
		nil,
		"",
		0,
	)
	if err != nil || selection == nil || selection.Account == nil {
		if reqLog != nil {
			reqLog.Info("openai.adaptive_native_leaf_messages_select_failed",
				zap.String("leaf_platform", leafPlatform),
				zap.Error(err),
			)
		}
		h.markAdaptiveLeafFailed(c.Request.Context(), c, "precommit_upstream", reqLog)
		return true, false
	}
	account := selection.Account
	if selection.ReleaseFunc != nil {
		defer selection.ReleaseFunc()
	}

	forwardBody := body
	if channelMapping.Mapped {
		forwardBody = h.gatewayService.ReplaceModelInBody(body, channelMapping.MappedModel)
	}

	var result *service.ForwardResult
	if account.Platform == service.PlatformGemini && h.geminiCompat != nil {
		result, err = h.geminiCompat.Forward(c.Request.Context(), c, account, forwardBody)
	} else {
		bodyRef := service.NewRequestBodyRef(forwardBody)
		parsed, parseErr := service.ParseGatewayRequest(bodyRef, "messages")
		if parseErr != nil || parsed == nil {
			h.markAdaptiveLeafFailed(c.Request.Context(), c, "precommit_upstream", reqLog)
			return true, false
		}
		result, err = h.anthropicGateway.Forward(c.Request.Context(), c, account, parsed)
	}
	if err != nil || result == nil {
		if reqLog != nil {
			reqLog.Info("openai.adaptive_native_leaf_messages_forward_failed",
				zap.String("leaf_platform", leafPlatform),
				zap.Int64("account_id", account.ID),
				zap.Error(err),
			)
		}
		h.markAdaptiveLeafFailed(c.Request.Context(), c, "precommit_upstream", reqLog)
		return true, false
	}

	adaptiveBillingCtx, adaptiveSession := prepareAdaptiveSessionSettlement(c)
	userAgent := c.GetHeader("User-Agent")
	clientIP := ip.GetClientIP(c)
	inboundEndpoint := GetInboundEndpoint(c)
	upstreamEndpoint := GetUpstreamEndpoint(c, account.Platform)
	quotaPlatform := service.QuotaPlatform(c.Request.Context(), attemptAPIKey)
	sessionID := service.ExtractClientSessionID(c)
	h.submitOpenAIUsageRecordTask(c.Request.Context(), nil, func(ctx context.Context) {
		if recErr := h.anthropicGateway.RecordUsage(ctx, &service.RecordUsageInput{
			Result:             result,
			APIKey:             attemptAPIKey,
			User:               attemptAPIKey.User,
			Account:            account,
			Subscription:       subscription,
			InboundEndpoint:    inboundEndpoint,
			UpstreamEndpoint:   upstreamEndpoint,
			UserAgent:          userAgent,
			IPAddress:          clientIP,
			APIKeyService:      h.apiKeyService,
			QuotaPlatform:      quotaPlatform,
			SessionID:          sessionID,
			ChannelUsageFields: clientRequestedUsageFields(c, channelMapping, reqModel, result.UpstreamModel),
			PricingAt:          pricingAt,
			AdaptiveBilling:    adaptiveBillingCtx,
		}); recErr != nil && reqLog != nil {
			reqLog.Error("openai.adaptive_native_leaf_messages_record_usage_failed",
				zap.Int64("account_id", account.ID),
				zap.Error(recErr),
			)
			return
		}
		if adaptiveSession != nil {
			adaptiveSession.markAdaptiveBillingCaptured()
		}
	})
	return false, true
}

// tryAdaptiveNativeLeafResponses executes one Anthropic/Gemini Adaptive leaf
// from an OpenAI /v1/responses inbound request.
func (h *OpenAIGatewayHandler) tryAdaptiveNativeLeafResponses(
	c *gin.Context,
	attemptAPIKey *service.APIKey,
	subscription *service.UserSubscription,
	reqModel string,
	body []byte,
	sessionHash string,
	channelMapping service.ChannelMappingResult,
	pricingAt time.Time,
	reqLog *zap.Logger,
) (switchLeaf bool, done bool) {
	if h == nil || attemptAPIKey == nil || h.anthropicGateway == nil {
		return true, false
	}
	leafPlatform := adaptiveLeafPlatform(attemptAPIKey)
	selection, err := h.anthropicGateway.SelectAccountWithLoadAwareness(
		c.Request.Context(),
		attemptAPIKey.GroupID,
		sessionHash,
		reqModel,
		nil,
		"",
		0,
	)
	if err != nil || selection == nil || selection.Account == nil {
		if reqLog != nil {
			reqLog.Info("openai.adaptive_native_leaf_responses_select_failed",
				zap.String("leaf_platform", leafPlatform),
				zap.Error(err),
			)
		}
		h.markAdaptiveLeafFailed(c.Request.Context(), c, "precommit_upstream", reqLog)
		return true, false
	}
	account := selection.Account
	if selection.ReleaseFunc != nil {
		defer selection.ReleaseFunc()
	}

	forwardBody := body
	if channelMapping.Mapped {
		forwardBody = h.gatewayService.ReplaceModelInBody(body, channelMapping.MappedModel)
	}

	var result *service.ForwardResult
	if account.Platform == service.PlatformGemini && h.geminiCompat != nil {
		result, err = h.geminiCompat.ForwardAsResponses(c.Request.Context(), c, account, forwardBody)
	} else {
		result, err = h.anthropicGateway.ForwardAsResponses(c.Request.Context(), c, account, forwardBody, nil)
	}
	if err != nil || result == nil {
		if reqLog != nil {
			reqLog.Info("openai.adaptive_native_leaf_responses_forward_failed",
				zap.String("leaf_platform", leafPlatform),
				zap.Int64("account_id", account.ID),
				zap.Error(err),
			)
		}
		h.markAdaptiveLeafFailed(c.Request.Context(), c, "precommit_upstream", reqLog)
		return true, false
	}

	adaptiveBillingCtx, adaptiveSession := prepareAdaptiveSessionSettlement(c)
	userAgent := c.GetHeader("User-Agent")
	clientIP := ip.GetClientIP(c)
	inboundEndpoint := GetInboundEndpoint(c)
	upstreamEndpoint := GetUpstreamEndpoint(c, account.Platform)
	quotaPlatform := service.QuotaPlatform(c.Request.Context(), attemptAPIKey)
	sessionID := service.ExtractClientSessionID(c)
	h.submitOpenAIUsageRecordTask(c.Request.Context(), nil, func(ctx context.Context) {
		if recErr := h.anthropicGateway.RecordUsage(ctx, &service.RecordUsageInput{
			Result:             result,
			APIKey:             attemptAPIKey,
			User:               attemptAPIKey.User,
			Account:            account,
			Subscription:       subscription,
			InboundEndpoint:    inboundEndpoint,
			UpstreamEndpoint:   upstreamEndpoint,
			UserAgent:          userAgent,
			IPAddress:          clientIP,
			APIKeyService:      h.apiKeyService,
			QuotaPlatform:      quotaPlatform,
			SessionID:          sessionID,
			ChannelUsageFields: clientRequestedUsageFields(c, channelMapping, reqModel, result.UpstreamModel),
			PricingAt:          pricingAt,
			AdaptiveBilling:    adaptiveBillingCtx,
		}); recErr != nil && reqLog != nil {
			reqLog.Error("openai.adaptive_native_leaf_responses_record_usage_failed",
				zap.Int64("account_id", account.ID),
				zap.Error(recErr),
			)
			return
		}
		if adaptiveSession != nil {
			adaptiveSession.markAdaptiveBillingCaptured()
		}
	})
	return false, true
}
