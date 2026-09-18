package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type openAIFallbackGroupAttempt struct {
	APIKey   *service.APIKey
	Group    *service.Group
	GroupID  *int64
	Index    int
	Fallback bool
	Skipped  bool
}

type adaptiveLeafFailureAction uint8

const (
	adaptiveLeafFailureLegacy adaptiveLeafFailureAction = iota
	adaptiveLeafFailureHold
	adaptiveLeafFailureSwitch
	adaptiveLeafFailureFailHard
)

type adaptiveLeafFailureDecision struct {
	Action          adaptiveLeafFailureAction
	Owner           string
	FailureCount    int
	SwitchThreshold int
	NextHealthScore float64
	Forced          bool
}

// decideAdaptiveLeafFailure assigns one owner to each eligible Adaptive
// failure. Anti-stall owns recovery whenever enabled; otherwise passive mode
// holds the current leaf until its third retryable failure.
func (h *OpenAIGatewayHandler) decideAdaptiveLeafFailure(
	c *gin.Context,
	apiKey *service.APIKey,
	failoverErr *service.UpstreamFailoverError,
	nextGroupID *int64,
) adaptiveLeafFailureDecision {
	decision := adaptiveLeafFailureDecision{Action: adaptiveLeafFailureLegacy, Owner: "legacy"}
	session := getOpenAIAdaptiveSession(c)
	if session == nil || failoverErr == nil || nextGroupID == nil || *nextGroupID <= 0 || !failoverErr.ShouldRetryNextAccount() {
		return decision
	}

	if anti := service.AntiStallSessionFromGin(c); anti != nil && anti.Config().Enabled {
		service.EnsureAntiStallDripRunning(c, anti)
		decision.Owner = "anti_stall"
		decision.FailureCount = anti.UpstreamFails()
		decision.SwitchThreshold = anti.Config().UpstreamMaxRetry
		if anti.ShouldFailHard() {
			decision.Action = adaptiveLeafFailureFailHard
			return decision
		}
		decision.Forced = antiStallForceLeafSwitch(failoverErr) ||
			(anti.ReserveWeight() <= 0 && failoverErr.StatusCode >= http.StatusInternalServerError)
		decision.NextHealthScore = h.getNextLeafHealthScore(c, nextGroupID)
		if decision.Forced || anti.ShouldSwitchLeaf(decision.NextHealthScore) {
			anti.RecordLeafSwitch()
			decision.Action = adaptiveLeafFailureSwitch
		} else {
			decision.Action = adaptiveLeafFailureHold
		}
		return decision
	}

	if apiKey == nil || !apiKey.AdaptivePassiveFailoverEnabled {
		return decision
	}
	session.CurrentLeafUpstreamFailures++
	decision.Owner = "passive"
	decision.FailureCount = session.CurrentLeafUpstreamFailures
	decision.SwitchThreshold = adaptivePassiveFailureThreshold
	if decision.FailureCount >= adaptivePassiveFailureThreshold {
		decision.Action = adaptiveLeafFailureSwitch
	} else {
		decision.Action = adaptiveLeafFailureHold
	}
	return decision
}

func (h *OpenAIGatewayHandler) buildOpenAIFallbackGroupAttempts(ctx context.Context, apiKey *service.APIKey, allowGrok bool, resolveFallbackGroups bool, reqLog *zap.Logger) []openAIFallbackGroupAttempt {
	if apiKey == nil {
		return nil
	}

	primaryAPIKey := apiKey
	primaryGroup := apiKey.Group
	if primaryGroup == nil && apiKey.GroupID != nil && h != nil && h.apiKeyService != nil {
		group, err := h.apiKeyService.ResolveGroupByID(ctx, *apiKey.GroupID)
		if err != nil {
			if reqLog != nil {
				reqLog.Warn("openai.resolve_primary_group_failed",
					zap.Int64("group_id", *apiKey.GroupID),
					zap.Error(err),
				)
			}
		} else {
			primaryGroup = group
			primaryAPIKey = cloneAPIKeyWithGroup(apiKey, group)
		}
	}

	// Legacy fallback chains are removed: a key routes to its primary group only.
	// Group-level failover now comes exclusively from adaptive leaf routing.
	return []openAIFallbackGroupAttempt{{
		APIKey:  primaryAPIKey,
		Group:   primaryGroup,
		GroupID: primaryAPIKey.GroupID,
		Index:   0,
	}}
}

func (h *OpenAIGatewayHandler) resolveOpenAIFallbackGroupAttempt(ctx context.Context, apiKey *service.APIKey, attempt openAIFallbackGroupAttempt, allowGrok bool, reqLog *zap.Logger) (openAIFallbackGroupAttempt, bool) {
	if !attempt.Fallback || attempt.APIKey != nil {
		return attempt, attempt.APIKey != nil
	}
	if attempt.GroupID == nil || *attempt.GroupID <= 0 {
		return attempt, false
	}
	if h == nil || h.apiKeyService == nil {
		if reqLog != nil {
			reqLog.Warn("openai.resolve_fallback_group_failed",
				zap.Int64("fallback_group_id", *attempt.GroupID),
				zap.String("reason", "api_key_service_unavailable"),
			)
		}
		return attempt, false
	}
	group, err := h.apiKeyService.ResolveGroupByID(ctx, *attempt.GroupID)
	if err != nil {
		if reqLog != nil {
			reqLog.Warn("openai.resolve_fallback_group_failed",
				zap.Int64("fallback_group_id", *attempt.GroupID),
				zap.Error(err),
			)
		}
		return attempt, false
	}
	if !openAIFallbackGroupAllowed(group, allowGrok) {
		if reqLog != nil {
			reqLog.Warn("openai.fallback_group_skipped_incompatible_platform",
				zap.Int64("fallback_group_id", group.ID),
				zap.String("fallback_platform", group.Platform),
			)
		}
		return attempt, false
	}
	fallbackAPIKey := cloneAPIKeyWithGroup(apiKey, group)
	attempt.APIKey = fallbackAPIKey
	attempt.Group = group
	attempt.GroupID = fallbackAPIKey.GroupID
	return attempt, true
}

func openAIFallbackGroupAllowed(group *service.Group, allowGrok bool) bool {
	if group == nil || group.Platform == "" || group.Platform == service.PlatformOpenAI {
		return true
	}
	return allowGrok && group.Platform == service.PlatformGrok
}

func hasNextOpenAIFallbackGroup(attempts []openAIFallbackGroupAttempt, index int) bool {
	return nextOpenAIFallbackGroupID(attempts, index) != nil
}

func nextOpenAIFallbackGroupID(attempts []openAIFallbackGroupAttempt, index int) *int64 {
	if index < 0 {
		return nil
	}
	for i := index + 1; i < len(attempts); i++ {
		if attempts[i].Skipped {
			continue
		}
		if attempts[i].GroupID != nil && *attempts[i].GroupID > 0 {
			return attempts[i].GroupID
		}
	}
	return nil
}

func (h *OpenAIGatewayHandler) nextUsableOpenAIFallbackGroupID(ctx context.Context, apiKey *service.APIKey, attempts []openAIFallbackGroupAttempt, index int, allowGrok bool, requireImageGeneration bool, reqLog *zap.Logger) *int64 {
	if index < 0 {
		return nil
	}
	for i := index + 1; i < len(attempts); i++ {
		attempt := attempts[i]
		if attempt.Skipped || !attempt.Fallback {
			continue
		}
		if attempt.APIKey == nil {
			var ok bool
			attempt, ok = h.resolveOpenAIFallbackGroupAttempt(ctx, apiKey, attempt, allowGrok, reqLog)
			if !ok {
				attempt.Skipped = true
				attempts[i] = attempt
				continue
			}
			attempts[i] = attempt
		}
		if requireImageGeneration && !service.GroupAllowsImageGeneration(attempt.Group) {
			if reqLog != nil {
				reqLog.Warn("openai.fallback_group_skipped_image_generation_disabled",
					zap.Any("fallback_group_id", attempt.GroupID),
				)
			}
			attempt.Skipped = true
			attempts[i] = attempt
			continue
		}
		if attempt.GroupID != nil && *attempt.GroupID > 0 {
			return attempt.GroupID
		}
	}
	return nil
}

func openAIFallbackAttemptsAllowImageGeneration(attempts []openAIFallbackGroupAttempt) bool {
	for _, attempt := range attempts {
		if service.GroupAllowsImageGeneration(attempt.Group) {
			return true
		}
	}
	return false
}

func openAIPrimaryGroupAllowsImageGeneration(apiKey *service.APIKey, attempts []openAIFallbackGroupAttempt) bool {
	if apiKey == nil {
		return true
	}
	if apiKey.Group != nil {
		return service.GroupAllowsImageGeneration(apiKey.Group)
	}
	if len(attempts) > 0 && attempts[0].Group != nil {
		return service.GroupAllowsImageGeneration(attempts[0].Group)
	}
	return apiKey.GroupID == nil
}

func shouldFastSwitchOpenAIFallbackGroup(failoverErr *service.UpstreamFailoverError) bool {
	return failoverErr != nil && failoverErr.StatusCode >= 500
}

// openAIShouldGroupFailover decides whether to switch Adaptive/fallback groups
// for this upstream error.
//
// Default (no Anti-Stall): only 5xx fast-switch (legacy).
// With Anti-Stall PRO enabled: intercept any upstream error that the gateway
// already classified as "retry next account" — so the client does not see
// mid-stream 401/402/403/413/429/400-transient/etc. while another leaf is tried.
// True client-faults (context window, non-failover 4xx) never become
// UpstreamFailoverError with retry, so they stay unintercepted.
func openAIShouldGroupFailover(c *gin.Context, failoverErr *service.UpstreamFailoverError) bool {
	if shouldFastSwitchOpenAIFallbackGroup(failoverErr) {
		return true
	}
	if failoverErr == nil || !failoverErr.ShouldRetryNextAccount() {
		return false
	}
	anti := service.AntiStallSessionFromGin(c)
	return anti != nil && anti.Config().Enabled
}

// antiStallForceLeafSwitchOnStatus: errors that are typically leaf/account
// scoped (rate limit, quota, auth, body-size limit, overload). Staying on the
// same leaf is unlikely to help — force Adaptive leaf switch under Anti-Stall.
// Generic 5xx still may hold the leaf for a few same-leaf retries.
func antiStallForceLeafSwitchOnStatus(statusCode int) bool {
	switch statusCode {
	case http.StatusUnauthorized, // 401
		http.StatusPaymentRequired,       // 402
		http.StatusForbidden,             // 403
		http.StatusRequestTimeout,        // 408
		http.StatusConflict,              // 409
		http.StatusRequestEntityTooLarge, // 413
		http.StatusTooManyRequests,       // 429
		529:                              // overloaded (Anthropic-style / some OpenAI paths)
		return true
	default:
		return false
	}
}

// antiStallForceLeafSwitch reports whether this failover should skip same-leaf
// hold and switch immediately.
//
// Uses fine-grained Anthropic / Gemini error tables when the body is
// recognizable; otherwise status heuristics + OpenAI-specific cases.
func antiStallForceLeafSwitch(failoverErr *service.UpstreamFailoverError) bool {
	if failoverErr == nil {
		return false
	}
	// Platform tables (Anthropic error.type / Gemini error.status / auto).
	if service.AntiStallForceLeafFromUpstream(failoverErr.StatusCode, failoverErr.ResponseBody) {
		return true
	}
	if antiStallForceLeafSwitchOnStatus(failoverErr.StatusCode) {
		return true
	}
	// Account-specific body size limit → another leaf may accept it.
	if failoverErr.IsOpenAIRequestBodyTooLarge() {
		return true
	}
	// Empty completion / silent refusal is account/model flaky — switch leaf.
	if service.IsOpenAISilentRefusalErrorBody(failoverErr.ResponseBody) {
		return true
	}
	return false
}

func (h *OpenAIGatewayHandler) openAIFallbackResponseHeaderTimeout() time.Duration {
	if h == nil || h.cfg == nil || h.cfg.Gateway.OpenAIFallbackResponseHeaderTimeout <= 0 {
		return 0
	}
	return time.Duration(h.cfg.Gateway.OpenAIFallbackResponseHeaderTimeout) * time.Second
}

func (h *OpenAIGatewayHandler) withOpenAIFallbackResponseHeaderTimeout(ctx context.Context, apiKey *service.APIKey, attempts []openAIFallbackGroupAttempt, attemptIndex int, allowGrok bool, requireImageGeneration bool, reqLog *zap.Logger) context.Context {
	timeout := h.openAIFallbackResponseHeaderTimeout()
	nextGroupID := h.nextUsableOpenAIFallbackGroupID(ctx, apiKey, attempts, attemptIndex, allowGrok, requireImageGeneration, reqLog)
	if timeout <= 0 || nextGroupID == nil {
		return ctx
	}
	if reqLog != nil {
		reqLog.Debug("openai.fallback_response_header_timeout_enabled",
			zap.Int("timeout_ms", int(timeout.Milliseconds())),
			zap.Any("next_group_id", nextGroupID),
		)
	}
	return service.WithHTTPUpstreamResponseHeaderTimeout(ctx, timeout)
}
