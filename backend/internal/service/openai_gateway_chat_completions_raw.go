package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/util/responseheaders"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
	"go.uber.org/zap"
)

// openaiCCRawAllowedHeaders 是 CC 直转路径专用的客户端 header 透传白名单。
//
// **关键**：不能复用 openaiAllowedHeaders——后者含 Codex 客户端专属 header
// （originator / session_id / x-codex-turn-state / x-codex-turn-metadata / conversation_id），
// 这些在 ChatGPT OAuth 上游是必需的，但透传给 DeepSeek/Kimi/GLM 等第三方
// OpenAI 兼容上游会造成：
//   - 完全忽略（多数友好厂商）——隐性污染上游统计
//   - 400 "unknown parameter"（严格上游）——可见错误
//
// 这里仅放行通用 HTTP header；content-type / authorization / accept 由上下文
// 显式设置，不依赖透传。
//
// 参见决策记录：
// pensieve/short-term/maxims/dont-reuse-shared-headers-whitelist-across-different-upstream-trust-domains
var openaiCCRawAllowedHeaders = map[string]bool{
	"accept-language": true,
	"user-agent":      true,
}

// forwardAsRawChatCompletions 直转客户端的 Chat Completions 请求到上游
// `{base_url}/v1/chat/completions`，**不**做 CC↔Responses 协议转换。
//
// 适用场景：account.platform=openai && account.type=apikey && 上游已被探测确认
// 不支持 /v1/responses 端点（如 DeepSeek/Kimi/GLM/Qwen 等第三方 OpenAI 兼容上游）。
//
// 与 ForwardAsChatCompletions 的关键差异：
//
//   - 不调用 apicompat.ChatCompletionsToResponses，body 仅做模型 ID 改写
//   - 上游 URL 拼到 /v1/chat/completions 而非 /v1/responses
//   - 流式响应 SSE 直接透传给客户端（上游 chunk 已是 CC 格式）
//   - 非流式响应 JSON 直接透传，仅按需提取 usage
//   - 不应用 codex OAuth transform（APIKey 路径无 OAuth）
//   - 不注入 prompt_cache_key（OAuth 专属机制）
//
// 调用入口：openai_gateway_chat_completions.go::ForwardAsChatCompletions
// 在函数顶部按 openai_compat.ShouldUseResponsesAPI 分流。
func (s *OpenAIGatewayService) forwardAsRawChatCompletions(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
	defaultMappedModel string,
) (*OpenAIForwardResult, error) {
	startTime := time.Now()

	// 1. Parse minimal fields needed for routing/billing
	originalModel := gjson.GetBytes(body, "model").String()
	if originalModel == "" {
		writeChatCompletionsError(c, http.StatusBadRequest, "invalid_request_error", "model is required")
		return nil, fmt.Errorf("missing model in request")
	}
	inboundStream := gjson.GetBytes(body, "stream").Bool()
	upstreamStream := inboundStream || account.RequiresOpenAIStreamOnly()

	// 2. Resolve model mapping (same as ForwardAsChatCompletions)
	billingModel := resolveOpenAIForwardModel(account, originalModel, defaultMappedModel)
	upstreamModel := normalizeOpenAIModelForUpstream(account, billingModel)
	SetOpsUpstreamModel(c, upstreamModel)
	grokCacheIdentity := ""
	if account.Platform == PlatformGrok {
		// Resolve before image bridging or other body rewrites so the fallback is
		// anchored to the client's stable conversation prefix.
		grokCacheIdentity = resolveGrokCacheIdentity(c, body, "", upstreamModel)
	}
	reasoningEffort := extractOpenAIReasoningEffortFromBody(body, upstreamModel, billingModel, originalModel)
	// 国产模型默认 effort 补充：需要 mappedModel 判定，推迟到 billingModel 算出之后。
	reasoningEffort = ApplyThinkingEnabledFallback(reasoningEffort, body, billingModel)

	// 3. Rewrite model in body (no protocol conversion)
	upstreamBody := body
	if upstreamModel != originalModel {
		upstreamBody = ReplaceModelInBody(body, upstreamModel)
	}
	if normalizedBody, normalized := NormalizeGLMOpenAIReasoningEffort(upstreamBody, upstreamModel); normalized {
		upstreamBody = normalizedBody
	}

	// 4. Apply OpenAI fast policy on the CC body
	updatedBody, policyErr := s.applyOpenAIFastPolicyToBody(ctx, account, upstreamModel, upstreamBody)
	if policyErr != nil {
		var blocked *OpenAIFastBlockedError
		if errors.As(policyErr, &blocked) {
			MarkOpsClientBusinessLimited(c, OpsClientBusinessLimitedReasonLocalPolicyDenied)
			writeChatCompletionsError(c, http.StatusForbidden, "permission_error", blocked.Message)
		}
		return nil, policyErr
	}
	upstreamBody = updatedBody
	// 计费兜底 tier = 最终出站 body（policy filter/force 后）里的 tier；
	// 最终值由 resolvedOpenAIUpstreamServiceTier 决定（上游回显优先）。
	serviceTier := extractOpenAIServiceTierFromBody(upstreamBody)
	if account.Platform == PlatformGrok {
		strippedBody, stripErr := stripRedundantGrokChatViewImageTool(upstreamBody)
		if stripErr != nil {
			return nil, fmt.Errorf("strip redundant Grok Chat view_image tool: %w", stripErr)
		}
		upstreamBody = strippedBody
	}

	// Grok Composer does not accept image_url parts directly, but Grok Build
	// can describe the images first. Bridge only this exact failure mode.
	token, tokenKind, err := s.getRequestCredential(ctx, c, account)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("account %d missing %s credential", account.ID, tokenKind)
	}

	var bridgeUsage OpenAIUsage
	if account.Platform == PlatformGrok {
		bridgedBody, usage, bridged, bridgeErr := s.bridgeGrokComposerImageInputs(ctx, c, account, upstreamBody, token)
		if bridgeErr != nil {
			var failoverErr *UpstreamFailoverError
			if !errors.As(bridgeErr, &failoverErr) && c != nil && c.Writer != nil && !c.Writer.Written() {
				writeChatCompletionsError(c, http.StatusBadGateway, "upstream_error", bridgeErr.Error())
			}
			return nil, bridgeErr
		}
		if bridged {
			upstreamBody = bridgedBody
			addOpenAIUsage(&bridgeUsage, usage)
		}
	}

	if upstreamStream {
		if !gjson.GetBytes(upstreamBody, "stream").Bool() {
			forcedBody, forceErr := sjson.SetBytes(upstreamBody, "stream", true)
			if forceErr != nil {
				return nil, fmt.Errorf("force upstream stream: %w", forceErr)
			}
			upstreamBody = forcedBody
		}
		var usageErr error
		upstreamBody, usageErr = ensureOpenAIChatStreamUsage(upstreamBody)
		if usageErr != nil {
			return nil, fmt.Errorf("enable stream usage: %w", usageErr)
		}
	}
	if account.Platform == PlatformGrok {
		upstreamBody, err = stripGrokChatPromptCacheKey(upstreamBody)
		if err != nil {
			return nil, fmt.Errorf("remove Responses-only Grok prompt cache key: %w", err)
		}
		upstreamBody, err = normalizeGrokChatReasoningEffort(upstreamBody, upstreamModel)
		if err != nil {
			return nil, fmt.Errorf("normalize Grok chat reasoning effort: %w", err)
		}
	}
	upstreamBody = applyOllamaCloudRawChatCompletionsRequest(account, upstreamBody)

	logger.L().Debug("openai chat_completions raw: forwarding without protocol conversion",
		zap.Int64("account_id", account.ID),
		zap.String("original_model", originalModel),
		zap.String("billing_model", billingModel),
		zap.String("upstream_model", upstreamModel),
		zap.Bool("inbound_stream", inboundStream),
		zap.Bool("upstream_stream", upstreamStream),
		zap.Bool("openai_stream_only", account.RequiresOpenAIStreamOnly()),
	)

	// 5. Build and send upstream request via the shared CC pipeline
	targetURL, err := s.rawChatCompletionsURL(account)
	if err != nil {
		return nil, err
	}
	SetActualOpenAIUpstreamEndpoint(c, grokChatRawEndpoint)
	customUA := account.GetOpenAIUserAgent()
	if customUA == "" && account.IsGrokOAuth() {
		customUA = defaultGrokUpstreamUserAgent()
	}
	resp, err := s.sendCCUpstreamRequest(ctx, c, account, targetURL, upstreamBody, upstreamStream, token, customUA, grokCacheIdentity)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	// 7. Handle error response with failover
	if resp.StatusCode >= 400 {
		respBody, upstreamMsg := s.readOpenAIUpstreamError(resp)
		if account.Platform == PlatformGrok {
			kind := "http_error"
			if s.shouldFailoverGrokUpstreamError(resp.StatusCode, respBody) {
				kind = "failover"
			}
			appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
				Platform:           account.Platform,
				AccountID:          account.ID,
				AccountName:        account.Name,
				UpstreamStatusCode: resp.StatusCode,
				UpstreamRequestID:  firstNonEmpty(resp.Header.Get("x-request-id"), resp.Header.Get("xai-request-id")),
				Kind:               kind,
				Message:            upstreamMsg,
			})
			s.handleGrokAccountUpstreamError(withGrokTeamRateLimitModel(ctx, upstreamModel), account, resp.StatusCode, resp.Header, respBody)
			if s.shouldFailoverGrokUpstreamError(resp.StatusCode, respBody) {
				retryable, retryDelay, retryDeadline, retryMax := grokSameAccountRetryMetadata(account, resp.StatusCode, respBody)
				return nil, &UpstreamFailoverError{
					StatusCode:               resp.StatusCode,
					ResponseBody:             respBody,
					ResponseHeaders:          resp.Header.Clone(),
					RetryableOnSameAccount:   retryable,
					RequestScopedTransient:   retryable && resp.StatusCode == http.StatusTooManyRequests,
					SameAccountRetryDelay:    retryDelay,
					SameAccountRetryDeadline: retryDeadline,
					SameAccountRetryMax:      retryMax,
				}
			}
			return s.handleChatCompletionsErrorResponse(resp, c, account, billingModel)
		}
		if foErr := s.failoverOpenAIUpstreamHTTPError(ctx, c, account, resp, respBody, upstreamMsg, upstreamModel); foErr != nil {
			return nil, foErr
		}
		return s.handleChatCompletionsErrorResponse(resp, c, account, billingModel)
	}

	if account.Platform == PlatformGrok {
		s.updateGrokUsageFromResponse(withGrokTeamRateLimitModel(ctx, upstreamModel), account, resp.Header, resp.StatusCode)
	}

	// 8. Forward response
	var result *OpenAIForwardResult
	var forwardErr error
	if inboundStream {
		result, forwardErr = s.streamRawChatCompletions(c, resp, account, originalModel, billingModel, upstreamModel, reasoningEffort, serviceTier, startTime, len(body))
	} else if upstreamStream {
		result, forwardErr = s.aggregateRawChatCompletions(c, resp, account, originalModel, billingModel, upstreamModel, reasoningEffort, serviceTier, startTime)
	} else {
		result, forwardErr = s.bufferRawChatCompletions(c, resp, account, originalModel, billingModel, upstreamModel, reasoningEffort, serviceTier, startTime)
	}
	if result != nil {
		addOpenAIUsage(&result.Usage, bridgeUsage)
		result.UpstreamEndpoint = grokChatRawEndpoint
	}
	return result, forwardErr
}

func (s *OpenAIGatewayService) rawChatCompletionsURL(account *Account) (string, error) {
	if account.Platform == PlatformGrok {
		targetURL, err := buildGrokChatCompletionsURL(account, s.cfg, s.settingService)
		if err != nil {
			return "", fmt.Errorf("invalid grok base_url: %w", err)
		}
		return targetURL, nil
	}

	return s.openAIChatCompletionsTargetURL(account)
}

// streamRawChatCompletions 透传上游 CC SSE 流到客户端，并提取 usage（包括
// 末尾 [DONE] 之前的 chunk 中的 usage 字段，按 OpenAI CC 协议）。
//
// usage 字段仅在客户端请求 stream_options.include_usage=true 时出现于上游响应中。
// 网关会对上游强制打开 include_usage 以保证计费完整，并原样向下游透传 usage，
// 让级联代理或下游计费系统也能拿到完整用量。
func (s *OpenAIGatewayService) streamRawChatCompletions(
	c *gin.Context,
	resp *http.Response,
	account *Account,
	originalModel string,
	billingModel string,
	upstreamModel string,
	reasoningEffort *string,
	serviceTier *string,
	startTime time.Time,
	requestBodyLen int,
) (*OpenAIForwardResult, error) {
	observer := upstreamResponseModelObserverFromContext(c)
	if observer == nil {
		observer = beginUpstreamResponseModelObservation(c)
	}
	requestID := resp.Header.Get("x-request-id")
	writeStreamHeaders := s.newStreamHeaderWriter(c, resp.Header)
	scanner := s.newUpstreamSSEScanner(resp.Body)

	var usage OpenAIUsage
	var firstTokenMs *int
	clientDisconnected := false
	clientOutputStarted := false
	pendingLines := make([]string, 0, 8)
	refusalDetector := newOpenAIChatSilentRefusalDetector(requestBodyLen)
	var terminal openAIRawStreamTerminalState

	writeLine := func(line string) {
		if clientDisconnected {
			return
		}
		if !clientOutputStarted && !refusalDetector.ShouldReleaseClientOutput() {
			pendingLines = append(pendingLines, line)
			return
		}
		if !clientOutputStarted {
			writeStreamHeaders()
			for _, pending := range pendingLines {
				if _, werr := c.Writer.WriteString(pending + "\n"); werr != nil {
					clientDisconnected = true
					logger.L().Debug("openai chat_completions raw: client disconnected, continuing to drain upstream for billing",
						zap.Error(werr),
						zap.String("request_id", requestID),
					)
					return
				}
			}
			pendingLines = pendingLines[:0]
			clientOutputStarted = true
		}
		if _, werr := c.Writer.WriteString(line + "\n"); werr != nil {
			clientDisconnected = true
			logger.L().Debug("openai chat_completions raw: client disconnected, continuing to drain upstream for billing",
				zap.Error(werr),
				zap.String("request_id", requestID),
			)
		}
	}

	for scanner.Scan() {
		line := scanner.Text()
		refusalDetector.ObserveSSELine(line)
		if payload, ok := extractOpenAISSEDataLine(line); ok {
			trimmedPayload := strings.TrimSpace(payload)
			terminal.ObserveDataLine(trimmedPayload)
			if trimmedPayload != "[DONE]" {
				observer.ObserveOpenAI([]byte(payload), strings.TrimSpace(gjson.Get(payload, "type").String()))
				usageOnlyChunk := isOpenAIChatUsageOnlyStreamChunk(payload)
				if u := extractCCStreamUsage(payload); u != nil {
					usage = *u
				}
				if firstTokenMs == nil && !usageOnlyChunk {
					elapsed := int(time.Since(startTime).Milliseconds())
					firstTokenMs = &elapsed
				}
			}
		}
		line = applyOllamaCloudRawChatCompletionsSSELine(account, line)
		line = stripEmptyChatToolCallIdentityFromSSELine(line)

		writeLine(line)
		if line == "" {
			if !clientDisconnected && clientOutputStarted {
				c.Writer.Flush()
			}
			if account != nil && account.IsGrok() && terminal.sawDone {
				break
			}
			continue
		}
		if !clientDisconnected && clientOutputStarted {
			c.Writer.Flush()
		}
	}

	scanErr := scanner.Err()
	if scanErr != nil && !errors.Is(scanErr, context.Canceled) && !errors.Is(scanErr, context.DeadlineExceeded) {
		logger.L().Warn("openai chat_completions raw: stream read error",
			zap.Error(scanErr),
			zap.String("request_id", requestID),
		)
	}
	requestContextCanceled := false
	if c != nil && c.Request != nil {
		requestContextCanceled = c.Request.Context().Err() != nil
	}
	// A canceled client request must retain the existing partial-usage billing
	// semantics. A transport timeout that did not cancel the request context is
	// an upstream failure and must not be silently treated as a client abort.
	clientAborted := clientDisconnected ||
		(errors.Is(scanErr, context.Canceled) && requestContextCanceled) ||
		(errors.Is(scanErr, context.DeadlineExceeded) && requestContextCanceled)
	if !clientAborted && terminal.IsTruncated(clientOutputStarted) {
		cause := scanErr
		if cause == nil {
			cause = ErrOpenAIUpstreamStreamTruncated
		}
		logger.L().Warn("openai chat_completions raw: upstream stream truncated before terminal chunk",
			zap.Error(cause),
			zap.String("request_id", requestID),
			zap.Int64("account_id", account.ID),
			zap.String("upstream_model", upstreamModel),
			zap.Bool("saw_sse_data", terminal.sawDataLine),
			zap.Bool("client_output_started", clientOutputStarted),
		)
		if !clientOutputStarted {
			return nil, newOpenAIRawStreamTruncatedFailoverError(c, account, requestID, cause)
		}
		recordOpenAIRawStreamTruncation(c, account, requestID, cause, "http_error")
		return &OpenAIForwardResult{
			RequestID:                     requestID,
			Usage:                         usage,
			Model:                         originalModel,
			BillingModel:                  billingModel,
			UpstreamModel:                 upstreamModel,
			UpstreamResponseModel:         observedUpstreamResponseModel(c),
			UpstreamResponseModelConflict: observedUpstreamResponseModelConflict(c),
			UpstreamResponseServiceTier:   observedUpstreamResponseServiceTier(c),
			ReasoningEffort:               reasoningEffort,
			ServiceTier:                   resolvedOpenAIUpstreamServiceTier(c, serviceTier),
			Stream:                        true,
			Duration:                      time.Since(startTime),
			FirstTokenMs:                  firstTokenMs,
		}, newOpenAIUpstreamStreamReadError(cause)
	}
	if scanErr == nil && !clientDisconnected && !clientOutputStarted {
		if refusalDetector.IsSilentRefusal() {
			return nil, newOpenAISilentRefusalFailoverError(c, account, requestID)
		}
		if len(pendingLines) > 0 {
			writeStreamHeaders()
			for _, pending := range pendingLines {
				if _, werr := c.Writer.WriteString(pending + "\n"); werr != nil {
					clientDisconnected = true
					logger.L().Debug("openai chat_completions raw: client disconnected during final flush",
						zap.Error(werr),
						zap.String("request_id", requestID),
					)
					break
				}
			}
			if !clientDisconnected {
				c.Writer.Flush()
				clientOutputStarted = true
			}
		}
	}

	return &OpenAIForwardResult{
		RequestID:                     requestID,
		Usage:                         usage,
		Model:                         originalModel,
		BillingModel:                  billingModel,
		UpstreamModel:                 upstreamModel,
		UpstreamResponseModel:         observedUpstreamResponseModel(c),
		UpstreamResponseModelConflict: observedUpstreamResponseModelConflict(c),
		UpstreamResponseServiceTier:   observedUpstreamResponseServiceTier(c),
		ReasoningEffort:               reasoningEffort,
		ServiceTier:                   resolvedOpenAIUpstreamServiceTier(c, serviceTier),
		Stream:                        true,
		Duration:                      time.Since(startTime),
		FirstTokenMs:                  firstTokenMs,
	}, nil
}

// ensureOpenAIChatStreamUsage 确保 raw Chat Completions 流式请求会让上游返回 usage。
// usage 也会继续向下游透传，支持级联代理和下游计费系统。
func ensureOpenAIChatStreamUsage(body []byte) ([]byte, error) {
	updated, err := sjson.SetBytes(body, "stream_options.include_usage", true)
	if err != nil {
		return body, err
	}
	return updated, nil
}

func isOpenAIChatUsageOnlyStreamChunk(payload string) bool {
	if strings.TrimSpace(payload) == "" {
		return false
	}
	if !gjson.Get(payload, "usage").Exists() {
		return false
	}
	choices := gjson.Get(payload, "choices")
	return choices.Exists() && choices.IsArray() && len(choices.Array()) == 0
}

// extractCCStreamUsage 从单个 CC 流式 chunk 的 payload 中提取 usage 字段。
// CC 协议中 usage 仅出现在末尾 chunk（且仅当 include_usage 生效时），
// 但上游可能在多个 chunk 中重复——总是用最新值。
func extractCCStreamUsage(payload string) *OpenAIUsage {
	usageResult := gjson.Get(payload, "usage")
	if !usageResult.Exists() || !usageResult.IsObject() {
		return nil
	}
	u, ok := openAIUsageFromGJSON(usageResult)
	if !ok {
		return nil
	}
	return &u
}

func (s *OpenAIGatewayService) aggregateRawChatCompletions(
	c *gin.Context,
	resp *http.Response,
	account *Account,
	originalModel string,
	billingModel string,
	upstreamModel string,
	reasoningEffort *string,
	serviceTier *string,
	startTime time.Time,
) (*OpenAIForwardResult, error) {
	requestID := resp.Header.Get("x-request-id")
	acc, err := s.collectRawChatCompletionSSE(c, resp, originalModel, requestID)
	if err != nil {
		return nil, newOpenAIRawStreamTruncatedFailoverError(c, account, requestID, err)
	}

	chatResp := acc.Response()
	if requiresBillableGrokChatUsage(account, billingModel, upstreamModel, chatResp.Model) && !hasBillableGrokChatUsage(acc.Usage) {
		upstreamRequestID := firstNonEmpty(requestID, resp.Header.Get("xai-request-id"))
		return nil, newGrokMissingUsageFailoverError(c, account, upstreamRequestID)
	}

	if s.responseHeaderFilter != nil {
		responseheaders.WriteFilteredHeaders(c.Writer.Header(), resp.Header, s.responseHeaderFilter)
	}
	c.Writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	c.JSON(http.StatusOK, chatResp)

	return &OpenAIForwardResult{
		RequestID:                     requestID,
		Usage:                         acc.Usage,
		Model:                         originalModel,
		BillingModel:                  billingModel,
		UpstreamModel:                 upstreamModel,
		UpstreamResponseModel:         observedUpstreamResponseModel(c),
		UpstreamResponseModelConflict: observedUpstreamResponseModelConflict(c),
		UpstreamResponseServiceTier:   observedUpstreamResponseServiceTier(c),
		ReasoningEffort:               reasoningEffort,
		ServiceTier:                   resolvedOpenAIUpstreamServiceTier(c, serviceTier),
		Stream:                        false,
		Duration:                      time.Since(startTime),
	}, nil
}

func (s *OpenAIGatewayService) collectRawChatCompletionSSE(
	c *gin.Context,
	resp *http.Response,
	fallbackModel string,
	requestID string,
) (*rawChatCompletionAggregator, error) {
	observer := upstreamResponseModelObserverFromContext(c)
	if observer == nil {
		observer = beginUpstreamResponseModelObservation(c)
	}
	acc := newRawChatCompletionAggregator(fallbackModel)
	var terminal openAIRawStreamTerminalState
	scanner := s.newUpstreamSSEScanner(resp.Body)
	for scanner.Scan() {
		payload, ok := extractOpenAISSEDataLine(scanner.Text())
		if !ok {
			continue
		}
		trimmed := strings.TrimSpace(payload)
		terminal.ObserveDataLine(trimmed)
		if trimmed == "" || trimmed == "[DONE]" {
			continue
		}
		observer.ObserveOpenAI([]byte(payload), strings.TrimSpace(gjson.Get(payload, "type").String()))
		acc.Observe(trimmed)
	}
	scanErr := scanner.Err()
	if scanErr != nil && !errors.Is(scanErr, context.Canceled) && !errors.Is(scanErr, context.DeadlineExceeded) {
		logger.L().Warn("openai chat_completions raw: aggregate stream read error",
			zap.Error(scanErr),
			zap.String("request_id", requestID),
		)
	}
	if scanErr != nil || terminal.IsTruncated(false) {
		if scanErr != nil {
			return nil, scanErr
		}
		return nil, ErrOpenAIUpstreamStreamTruncated
	}
	return acc, nil
}

type rawChatCompletionAggregator struct {
	id                string
	created           int64
	model             string
	systemFingerprint string
	serviceTier       string
	Usage             OpenAIUsage
	choices           map[int]*rawChatAggregatedChoice
}

type rawChatAggregatedChoice struct {
	index         int
	role          string
	content       strings.Builder
	reasoning     strings.Builder
	finishReason  string
	toolCalls     map[int]*apicompat.ChatToolCall
	toolCallOrder []int
}

func newRawChatCompletionAggregator(fallbackModel string) *rawChatCompletionAggregator {
	return &rawChatCompletionAggregator{
		model:   fallbackModel,
		choices: make(map[int]*rawChatAggregatedChoice),
	}
}

func (a *rawChatCompletionAggregator) Observe(payload string) {
	if a == nil || strings.TrimSpace(payload) == "" {
		return
	}
	if id := gjson.Get(payload, "id").String(); id != "" {
		a.id = id
	}
	if created := gjson.Get(payload, "created").Int(); created != 0 {
		a.created = created
	}
	if model := gjson.Get(payload, "model").String(); model != "" {
		a.model = model
	}
	if fp := gjson.Get(payload, "system_fingerprint").String(); fp != "" {
		a.systemFingerprint = fp
	}
	if tier := gjson.Get(payload, "service_tier").String(); tier != "" {
		a.serviceTier = tier
	}
	if u := extractCCStreamUsage(payload); u != nil {
		a.Usage = *u
	}
	for _, choice := range gjson.Get(payload, "choices").Array() {
		index := int(choice.Get("index").Int())
		agg := a.choice(index)
		delta := choice.Get("delta")
		if role := delta.Get("role").String(); role != "" {
			agg.role = role
		}
		if content := delta.Get("content"); content.Exists() && content.Type == gjson.String {
			agg.content.WriteString(content.String())
		}
		if reasoning := delta.Get("reasoning_content"); reasoning.Exists() && reasoning.Type == gjson.String {
			agg.reasoning.WriteString(reasoning.String())
		} else if reasoning := delta.Get("reasoning"); reasoning.Exists() && reasoning.Type == gjson.String {
			agg.reasoning.WriteString(reasoning.String())
		}
		for _, tool := range delta.Get("tool_calls").Array() {
			toolIndex := int(tool.Get("index").Int())
			if !tool.Get("index").Exists() {
				toolIndex = len(agg.toolCallOrder)
			}
			call := agg.toolCall(toolIndex)
			if id := tool.Get("id").String(); id != "" {
				call.ID = id
			}
			if typ := tool.Get("type").String(); typ != "" {
				call.Type = typ
			}
			if name := tool.Get("function.name").String(); name != "" {
				call.Function.Name = name
			}
			if args := tool.Get("function.arguments"); args.Exists() && args.Type == gjson.String {
				call.Function.Arguments += args.String()
			}
		}
		if finish := strings.TrimSpace(choice.Get("finish_reason").String()); finish != "" {
			agg.finishReason = finish
		}
	}
}

func (a *rawChatCompletionAggregator) choice(index int) *rawChatAggregatedChoice {
	if existing, ok := a.choices[index]; ok {
		return existing
	}
	choice := &rawChatAggregatedChoice{
		index:     index,
		role:      "assistant",
		toolCalls: make(map[int]*apicompat.ChatToolCall),
	}
	a.choices[index] = choice
	return choice
}

func (c *rawChatAggregatedChoice) toolCall(index int) *apicompat.ChatToolCall {
	if existing, ok := c.toolCalls[index]; ok {
		return existing
	}
	call := &apicompat.ChatToolCall{Type: "function"}
	c.toolCalls[index] = call
	c.toolCallOrder = append(c.toolCallOrder, index)
	return call
}

func (a *rawChatCompletionAggregator) Response() apicompat.ChatCompletionsResponse {
	indexes := make([]int, 0, len(a.choices))
	for index := range a.choices {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	out := apicompat.ChatCompletionsResponse{
		ID:                a.id,
		Object:            "chat.completion",
		Created:           a.created,
		Model:             a.model,
		SystemFingerprint: a.systemFingerprint,
		ServiceTier:       a.serviceTier,
		Choices:           make([]apicompat.ChatChoice, 0, len(indexes)),
	}
	if a.Usage.InputTokens != 0 || a.Usage.OutputTokens != 0 || a.Usage.CacheReadInputTokens != 0 || a.Usage.CacheCreationInputTokens != 0 {
		out.Usage = openAIUsageToChatUsage(a.Usage)
	}
	for _, index := range indexes {
		choice := a.choices[index]
		message := apicompat.ChatMessage{Role: choice.role}
		if content := choice.content.String(); content != "" {
			raw, _ := json.Marshal(content)
			message.Content = raw
		}
		if reasoning := choice.reasoning.String(); reasoning != "" {
			message.ReasoningContent = reasoning
		}
		if len(choice.toolCallOrder) > 0 {
			message.ToolCalls = make([]apicompat.ChatToolCall, 0, len(choice.toolCallOrder))
			for _, toolIndex := range choice.toolCallOrder {
				call := *choice.toolCalls[toolIndex]
				call.Index = nil
				message.ToolCalls = append(message.ToolCalls, call)
			}
		}
		out.Choices = append(out.Choices, apicompat.ChatChoice{
			Index:        index,
			Message:      message,
			FinishReason: choice.finishReason,
		})
	}
	return out
}

func openAIUsageToChatUsage(usage OpenAIUsage) *apicompat.ChatUsage {
	out := &apicompat.ChatUsage{
		PromptTokens:     usage.InputTokens,
		CompletionTokens: usage.OutputTokens,
		TotalTokens:      usage.InputTokens + usage.OutputTokens,
	}
	if usage.CacheReadInputTokens != 0 || usage.CacheCreationInputTokens != 0 {
		out.PromptTokensDetails = &apicompat.ChatTokenDetails{
			CachedTokens:        usage.CacheReadInputTokens,
			CacheCreationTokens: usage.CacheCreationInputTokens,
			CacheWriteTokens:    usage.CacheCreationInputTokens,
		}
	}
	return out
}

// bufferRawChatCompletions 透传上游 CC 非流式 JSON 响应。
func (s *OpenAIGatewayService) bufferRawChatCompletions(
	c *gin.Context,
	resp *http.Response,
	account *Account,
	originalModel string,
	billingModel string,
	upstreamModel string,
	reasoningEffort *string,
	serviceTier *string,
	startTime time.Time,
) (*OpenAIForwardResult, error) {
	requestID := resp.Header.Get("x-request-id")

	respBody, err := ReadUpstreamResponseBody(resp.Body, s.cfg, c, openAITooLargeError)
	if err != nil {
		if !errors.Is(err, ErrUpstreamResponseBodyTooLarge) {
			writeChatCompletionsError(c, http.StatusBadGateway, "api_error", "Failed to read upstream response")
		}
		return nil, fmt.Errorf("read upstream body: %w", err)
	}
	observer := upstreamResponseModelObserverFromContext(c)
	if observer == nil {
		observer = beginUpstreamResponseModelObservation(c)
	}
	observer.ObserveOpenAI(respBody, strings.TrimSpace(gjson.GetBytes(respBody, "type").String()))

	var usage OpenAIUsage
	if parsedUsage, ok := extractOpenAIUsageFromJSONBytes(respBody); ok {
		usage = parsedUsage
	}
	responseModel := gjson.GetBytes(respBody, "model").String()
	if requiresBillableGrokChatUsage(account, billingModel, upstreamModel, responseModel) && !hasBillableGrokChatUsage(usage) {
		upstreamRequestID := firstNonEmpty(requestID, resp.Header.Get("xai-request-id"))
		return nil, newGrokMissingUsageFailoverError(c, account, upstreamRequestID)
	}
	respBody = applyOllamaCloudRawChatCompletionsResponse(account, respBody)

	if s.responseHeaderFilter != nil {
		responseheaders.WriteFilteredHeaders(c.Writer.Header(), resp.Header, s.responseHeaderFilter)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		c.Writer.Header().Set("Content-Type", ct)
	} else {
		c.Writer.Header().Set("Content-Type", "application/json")
	}
	c.Writer.WriteHeader(http.StatusOK)
	_, _ = c.Writer.Write(respBody)

	return &OpenAIForwardResult{
		RequestID:                     requestID,
		Usage:                         usage,
		Model:                         originalModel,
		BillingModel:                  billingModel,
		UpstreamModel:                 upstreamModel,
		UpstreamResponseModel:         observedUpstreamResponseModel(c),
		UpstreamResponseModelConflict: observedUpstreamResponseModelConflict(c),
		UpstreamResponseServiceTier:   observedUpstreamResponseServiceTier(c),
		ReasoningEffort:               reasoningEffort,
		ServiceTier:                   resolvedOpenAIUpstreamServiceTier(c, serviceTier),
		Stream:                        false,
		Duration:                      time.Since(startTime),
	}, nil
}

// buildOpenAIChatCompletionsURL 拼接上游 Chat Completions 端点 URL。
//
//   - base 已是 /chat/completions：原样返回
//   - base 以 /v1 结尾：追加 /chat/completions
//   - base 以其他版本段结尾（如 /v4）：追加 /chat/completions
//   - 其他情况：追加 /v1/chat/completions
//
// 与 buildOpenAIResponsesURL 是姐妹函数。
func buildOpenAIChatCompletionsURL(base string) string {
	return buildOpenAIEndpointURL(base, "/v1/chat/completions")
}
