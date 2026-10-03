package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/googleapi"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

func (s *GeminiMessagesCompatService) forwardGeminiNativeViaOpenAICompat(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	originalModel string,
	action string,
	stream bool,
	body []byte,
) (*ForwardResult, error) {
	start := time.Now()
	if s == nil || s.httpUpstream == nil {
		return nil, s.writeGoogleError(c, http.StatusBadGateway, "OpenAI-compatible Gemini upstream is not configured")
	}
	apiKey := strings.TrimSpace(account.GetCredential("api_key"))
	if apiKey == "" {
		return nil, s.writeGoogleError(c, http.StatusBadGateway, "gemini api_key not configured")
	}
	protocol := APIProtocolChatCompletions
	if account.usesNativeResponsesUpstream() {
		protocol = APIProtocolResponses
	}
	baseURL := strings.TrimSpace(account.GetCNProtocolBaseURL(protocol))
	if baseURL == "" {
		baseURL = strings.TrimSpace(account.GetCredential("base_url"))
	}
	if baseURL == "" {
		return nil, s.writeGoogleError(c, http.StatusBadGateway, "OpenAI-compatible Gemini base_url is not configured")
	}
	mappedModel := account.GetMappedModel(originalModel)
	if action == "countTokens" {
		return s.writeGeminiCountTokensFromBody(c, mappedModel, body)
	}

	chatBody, err := geminiNativeRequestToChatCompletions(mappedModel, body, stream)
	if err != nil {
		return nil, s.writeGoogleError(c, http.StatusBadRequest, err.Error())
	}
	endpoint := "/v1/chat/completions"
	if protocol == APIProtocolResponses {
		var chatRequest apicompat.ChatCompletionsRequest
		if err := json.Unmarshal(chatBody, &chatRequest); err != nil {
			return nil, s.writeGoogleError(c, http.StatusBadRequest, "Invalid Gemini request")
		}
		request, err := apicompat.ChatCompletionsToResponses(&chatRequest)
		if err != nil {
			return nil, s.writeGoogleError(c, http.StatusBadRequest, "Invalid Gemini request")
		}
		// The shared converter defaults to Codex streaming and a minimum budget.
		// Gemini relays support the requested mode and exact generation limit.
		request.Stream = stream
		request.MaxOutputTokens = chatRequest.MaxTokens
		request.Include = nil
		chatBody, err = json.Marshal(request)
		if err != nil {
			return nil, err
		}
		endpoint = "/v1/responses"
	}
	SetActualOpenAIUpstreamEndpoint(c, endpoint)
	fullURL := buildOpenAIEndpointURL(baseURL, endpoint)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fullURL, bytes.NewReader(chatBody))
	if err != nil {
		return nil, s.writeGoogleError(c, http.StatusBadGateway, err.Error())
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	if stream {
		req.Header.Set("Accept", "text/event-stream")
	}

	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	resp, err := s.httpUpstream.Do(WithAccountTrafficRequest(req, account), proxyURL, account.ID, account.Concurrency)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, s.writeGoogleError(c, http.StatusBadGateway, "Failed to contact Gemini upstream")
	}
	closeBody := sync.OnceFunc(func() { _ = resp.Body.Close() })
	defer closeBody()
	if resp.StatusCode >= 400 {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		if failoverErr := s.geminiOpenAICompatFailoverError(c, account, originalModel, resp.StatusCode, errBody, resp.Header.Get("x-request-id")); failoverErr != nil {
			return nil, failoverErr
		}
		return nil, s.writeGoogleError(c, resp.StatusCode, sanitizeUpstreamErrorMessage(extractGeminiBridgeErrorMessage(errBody)))
	}

	if stream {
		// Close a blocked read on cancellation, including custom upstream transports.
		stopClosing := context.AfterFunc(ctx, closeBody)
		defer stopClosing()
		result, err := s.pipeOpenAIStreamAsGemini(ctx, c, resp.Body, mappedModel, protocol, start)
		result.RequestID = resp.Header.Get("x-request-id")
		result.UpstreamHeaders = resp.Header
		result.Duration = time.Since(start)
		return result, err
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (8<<20)+1))
	if err != nil || len(raw) > 8<<20 || !json.Valid(raw) {
		return nil, s.writeGoogleError(c, http.StatusBadGateway, "Invalid Gemini upstream response")
	}
	usage, _ := extractOpenAIUsageFromJSONBytes(raw)
	result := &ForwardResult{
		RequestID: resp.Header.Get("x-request-id"), UpstreamHeaders: resp.Header,
		Model: mappedModel, UpstreamModel: mappedModel, Usage: geminiBridgeClaudeUsage(usage),
		Duration: time.Since(start),
	}
	if status := geminiBridgeFailureStatus(raw, ""); status != 0 {
		result.NonBillableUpstreamError = geminiBridgeNonBillableFailure(status, raw) && !geminiBridgeHasOutput(raw, result.Usage)
		return result, s.writeGoogleError(c, status, sanitizeUpstreamErrorMessage(extractGeminiBridgeErrorMessage(raw)))
	}
	usageMetadata := geminiBridgeUsageMetadata(usage, geminiBridgeUsageObject(raw))
	if protocol == APIProtocolResponses {
		var response apicompat.ResponsesResponse
		if err := json.Unmarshal(raw, &response); err != nil || (response.Status != "completed" && response.Status != "incomplete") {
			return nil, s.writeGoogleError(c, http.StatusBadGateway, "Invalid Gemini upstream response")
		}
		raw, err = json.Marshal(apicompat.ResponsesToChatCompletions(&response, mappedModel))
		if err != nil {
			return nil, err
		}
	}
	if !gjson.GetBytes(raw, "choices").IsArray() || len(gjson.GetBytes(raw, "choices").Array()) == 0 {
		s.markGeminiEmptyResponse(c, account, false, resp.Header.Get("x-request-id"))
		return nil, geminiEmptyResponseFailoverError(false, raw)
	}
	geminiBody := geminiBridgePayload(gjson.GetBytes(raw, "choices.0.message.content").String(), mappedModel,
		geminiBridgeFinishReason(gjson.GetBytes(raw, "choices.0.finish_reason").String()), usageMetadata)
	if isGeminiEmptyResponseBody(geminiBody) {
		s.markGeminiEmptyResponse(c, account, false, resp.Header.Get("x-request-id"))
		return nil, geminiEmptyResponseFailoverError(false, geminiBody)
	}
	c.Data(http.StatusOK, "application/json", geminiBody)
	return result, nil
}

func (s *GeminiMessagesCompatService) generateGeminiViaOpenAICompat(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	originalModel string,
	stream bool,
	body []byte,
	writeErr func(*gin.Context, int, string) error,
) ([]byte, ClaudeUsage, error) {
	if writeErr == nil {
		writeErr = s.writeGoogleError
	}
	if s == nil || s.httpUpstream == nil {
		return nil, ClaudeUsage{}, writeErr(c, http.StatusBadGateway, "OpenAI-compatible Gemini upstream is not configured")
	}
	mappedModel := account.GetMappedModel(originalModel)
	chatBody, err := geminiNativeRequestToChatCompletions(mappedModel, body, stream)
	if err != nil {
		return nil, ClaudeUsage{}, writeErr(c, http.StatusBadRequest, err.Error())
	}
	resp, err := s.doGeminiOpenAICompatChat(ctx, account, chatBody, stream)
	if err != nil {
		return nil, ClaudeUsage{}, writeErr(c, http.StatusBadGateway, err.Error())
	}
	closeBody := sync.OnceFunc(func() { _ = resp.Body.Close() })
	defer closeBody()
	if resp.StatusCode >= 400 {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		if failoverErr := s.geminiOpenAICompatFailoverError(c, account, originalModel, resp.StatusCode, errBody, resp.Header.Get("x-request-id")); failoverErr != nil {
			return nil, ClaudeUsage{}, failoverErr
		}
		return nil, ClaudeUsage{}, writeErr(c, resp.StatusCode, strings.TrimSpace(string(errBody)))
	}
	if stream {
		geminiBody, usage, collectErr := collectOpenAIChatStreamAsGemini(resp.Body, mappedModel)
		if collectErr != nil {
			return nil, ClaudeUsage{}, writeErr(c, http.StatusBadGateway, collectErr.Error())
		}
		if isGeminiEmptyResponseBody(geminiBody) {
			s.markGeminiEmptyResponse(c, account, true, resp.Header.Get("x-request-id"))
			return nil, ClaudeUsage{}, geminiEmptyResponseFailoverError(true, geminiBody)
		}
		return geminiBody, usage, nil
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if isGeminiEmptyOpenAIChatBody(raw) {
		s.markGeminiEmptyResponse(c, account, false, resp.Header.Get("x-request-id"))
		return nil, ClaudeUsage{}, geminiEmptyResponseFailoverError(false, raw)
	}
	geminiBody, usage := chatCompletionsToGeminiNative(raw, mappedModel)
	if isGeminiEmptyResponseBody(geminiBody) {
		s.markGeminiEmptyResponse(c, account, false, resp.Header.Get("x-request-id"))
		return nil, ClaudeUsage{}, geminiEmptyResponseFailoverError(false, geminiBody)
	}
	return geminiBody, usage, nil
}

func (s *GeminiMessagesCompatService) writeGeminiCountTokensFromBody(c *gin.Context, model string, body []byte) (*ForwardResult, error) {
	text := strings.TrimSpace(gjson.GetBytes(body, "contents").Raw)
	if text == "" {
		text = string(body)
	}
	count := len([]rune(text)) / 4
	if count < 1 {
		count = 1
	}
	payload, _ := json.Marshal(map[string]any{
		"totalTokens": count,
		"model":       model,
	})
	c.Data(http.StatusOK, "application/json", payload)
	return &ForwardResult{Model: model, UpstreamModel: model}, nil
}

func geminiNativeRequestToChatCompletions(model string, body []byte, stream bool) ([]byte, error) {
	contents := gjson.GetBytes(body, "contents")
	if !contents.IsArray() {
		return nil, fmt.Errorf("missing contents")
	}
	msgs := make([]map[string]any, 0)
	for _, content := range contents.Array() {
		role := strings.ToLower(strings.TrimSpace(content.Get("role").String()))
		switch role {
		case "model":
			role = "assistant"
		case "user", "system":
		default:
			role = "user"
		}
		var parts []string
		for _, part := range content.Get("parts").Array() {
			if text := strings.TrimSpace(part.Get("text").String()); text != "" {
				parts = append(parts, text)
			}
		}
		if len(parts) == 0 {
			continue
		}
		msgs = append(msgs, map[string]any{"role": role, "content": strings.Join(parts, "\n")})
	}
	if sys := strings.TrimSpace(gjson.GetBytes(body, "systemInstruction.parts.0.text").String()); sys != "" {
		msgs = append([]map[string]any{{"role": "system", "content": sys}}, msgs...)
	}
	if len(msgs) == 0 {
		return nil, fmt.Errorf("empty Gemini contents")
	}
	out := map[string]any{
		"model":    model,
		"messages": msgs,
		"stream":   stream,
	}
	if stream {
		out["stream_options"] = map[string]bool{"include_usage": true}
	}
	if temp := gjson.GetBytes(body, "generationConfig.temperature"); temp.Exists() {
		out["temperature"] = temp.Value()
	}
	if maxTok := gjson.GetBytes(body, "generationConfig.maxOutputTokens"); maxTok.Exists() {
		out["max_tokens"] = maxTok.Value()
	}
	return json.Marshal(out)
}

func isGeminiEmptyOpenAIChatBody(raw []byte) bool {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return true
	}
	if !gjson.ValidBytes(raw) {
		return false
	}
	parsed := gjson.ParseBytes(raw)
	if parsed.Get("error").Exists() {
		return false
	}
	choices := parsed.Get("choices")
	if !choices.Exists() {
		return isGeminiEmptyResponseBody(raw)
	}
	if !choices.IsArray() || len(choices.Array()) == 0 {
		return true
	}
	first := choices.Array()[0]
	content := strings.TrimSpace(first.Get("message.content").String())
	if content == "" {
		content = strings.TrimSpace(first.Get("delta.content").String())
	}
	if content != "" {
		return false
	}
	if first.Get("message.tool_calls").Exists() || first.Get("delta.tool_calls").Exists() {
		return false
	}
	return true
}

func shouldFallbackGeminiNativeToOpenAI(account *Account, statusCode int, respBody []byte, c *gin.Context) bool {
	if account == nil || !account.AllowsGeminiNativeToOpenAIFallback() {
		return false
	}
	if c != nil && IsResponseCommitted(c) {
		return false
	}
	return isGeminiNativePathMismatch(statusCode, respBody)
}

// isGeminiNativePathMismatch 判断 /v1beta 404 是「中转没有这条 Google 路径」
// 而不是 Google JSON 的模型不存在。前者同号改走 OpenAI 协议，后者换号。
func isGeminiNativePathMismatch(statusCode int, body []byte) bool {
	if statusCode != http.StatusNotFound && statusCode != http.StatusMethodNotAllowed {
		return false
	}
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 || !gjson.ValidBytes(trimmed) {
		return true
	}
	if gjson.GetBytes(trimmed, "error.code").Int() == 404 {
		return false
	}
	if strings.EqualFold(gjson.GetBytes(trimmed, "error.status").String(), "NOT_FOUND") {
		return false
	}
	return true
}

func (s *GeminiMessagesCompatService) geminiOpenAICompatFailoverError(c *gin.Context, account *Account, requestedModel string, statusCode int, respBody []byte, upstreamRequestID string) *UpstreamFailoverError {
	if account == nil || !s.shouldFailoverGeminiUpstreamError(statusCode) {
		return nil
	}
	if c != nil && c.Request != nil && s.rateLimitService != nil {
		s.handleGeminiUpstreamErrorForModel(c.Request.Context(), account, requestedModel, statusCode, nil, respBody)
	}
	return s.skippedErrorPolicyFailoverError(c, account, statusCode, respBody, upstreamRequestID)
}

func (s *GeminiMessagesCompatService) openaiCompatChatCompletionsURL(account *Account) (string, string, error) {
	apiKey := strings.TrimSpace(account.GetOpenAIProtocolAPIKey())
	if apiKey == "" {
		apiKey = strings.TrimSpace(account.GetCredential("api_key"))
	}
	if apiKey == "" {
		return "", "", fmt.Errorf("openai-compatible api_key not configured")
	}
	baseURL := strings.TrimSpace(account.GetOpenAIBaseURL())
	if baseURL == "" {
		baseURL = strings.TrimSpace(account.GetCNProtocolBaseURL(APIProtocolChatCompletions))
	}
	if baseURL == "" {
		baseURL = strings.TrimSpace(account.GetCredential("base_url"))
	}
	if baseURL == "" {
		return "", "", fmt.Errorf("OpenAI-compatible base_url is not configured")
	}
	normalized, err := s.validateUpstreamBaseURL(baseURL)
	if err != nil {
		return "", "", err
	}
	return buildOpenAIChatCompletionsURL(normalized), apiKey, nil
}

func (s *GeminiMessagesCompatService) doGeminiOpenAICompatChat(ctx context.Context, account *Account, chatBody []byte, stream bool) (*http.Response, error) {
	if s == nil || s.httpUpstream == nil {
		return nil, fmt.Errorf("OpenAI-compatible Gemini upstream is not configured")
	}
	fullURL, apiKey, err := s.openaiCompatChatCompletionsURL(account)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fullURL, bytes.NewReader(chatBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	if stream {
		req.Header.Set("Accept", "text/event-stream")
	}
	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	return s.httpUpstream.Do(WithAccountTrafficRequest(req, account), proxyURL, account.ID, account.Concurrency)
}

func collectOpenAIChatStreamAsGemini(body io.Reader, model string) ([]byte, ClaudeUsage, error) {
	raw, err := io.ReadAll(io.LimitReader(body, 8<<20))
	if err != nil {
		return nil, ClaudeUsage{}, err
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		empty, _ := json.Marshal(map[string]any{"candidates": []any{}})
		return empty, ClaudeUsage{}, nil
	}
	if !bytes.Contains(trimmed, []byte("data:")) {
		if isGeminiEmptyOpenAIChatBody(trimmed) {
			empty, _ := json.Marshal(map[string]any{"candidates": []any{}})
			return empty, ClaudeUsage{}, nil
		}
		geminiBody, usage := chatCompletionsToGeminiNative(trimmed, model)
		return geminiBody, usage, nil
	}
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var text strings.Builder
	usage := ClaudeUsage{}
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}
		delta := gjson.Get(data, "choices.0.delta.content").String()
		if delta == "" {
			delta = gjson.Get(data, "choices.0.message.content").String()
		}
		if delta != "" {
			text.WriteString(delta)
		}
		if gjson.Get(data, "usage").Exists() {
			usage.InputTokens = int(gjson.Get(data, "usage.prompt_tokens").Int())
			usage.OutputTokens = int(gjson.Get(data, "usage.completion_tokens").Int())
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, usage, err
	}
	synthetic, _ := json.Marshal(map[string]any{
		"choices": []map[string]any{
			{"message": map[string]any{"content": text.String()}},
		},
		"usage": map[string]any{
			"prompt_tokens":     usage.InputTokens,
			"completion_tokens": usage.OutputTokens,
		},
	})
	if isGeminiEmptyOpenAIChatBody(synthetic) {
		empty, _ := json.Marshal(map[string]any{"candidates": []any{}})
		return empty, usage, nil
	}
	geminiBody, converted := chatCompletionsToGeminiNative(synthetic, model)
	if converted.InputTokens > 0 || converted.OutputTokens > 0 {
		usage = converted
	}
	return geminiBody, usage, nil
}

func (s *GeminiMessagesCompatService) forwardClaudeViaOpenAICompat(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	originalModel string,
	stream bool,
	geminiReq []byte,
) (*ForwardResult, error) {
	writeErr := func(gc *gin.Context, status int, message string) error {
		errType := "upstream_error"
		if status == http.StatusBadRequest {
			errType = "invalid_request_error"
		}
		return s.writeClaudeError(gc, status, errType, message)
	}
	geminiBody, usage, err := s.generateGeminiViaOpenAICompat(ctx, c, account, originalModel, stream, geminiReq, writeErr)
	if err != nil {
		return nil, err
	}
	mappedModel := account.GetMappedModel(originalModel)
	claudeResp, converted := convertGeminiJSONToClaudeMessage(geminiBody, originalModel, false)
	if converted != nil && (converted.InputTokens > 0 || converted.OutputTokens > 0) {
		usage = *converted
	}
	if stream {
		writeClaudeStreamFromCollectedGemini(c, claudeResp, originalModel, usage)
	} else {
		c.JSON(http.StatusOK, claudeResp)
	}
	return &ForwardResult{Model: originalModel, UpstreamModel: mappedModel, Usage: usage, Stream: stream}, nil
}

func (s *GeminiMessagesCompatService) forwardChatCompletionsViaOpenAICompat(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	originalModel string,
	clientStream bool,
	geminiReq []byte,
	clientFormat geminiCompatClientFormat,
	startTime time.Time,
) (*ForwardResult, error) {
	writeErr := func(gc *gin.Context, status int, message string) error {
		errType := "upstream_error"
		if status == http.StatusBadRequest {
			errType = "invalid_request_error"
		}
		return s.writeGeminiCompatClientError(gc, clientFormat, status, errType, message)
	}
	geminiBody, usage, err := s.generateGeminiViaOpenAICompat(ctx, c, account, originalModel, clientStream, geminiReq, writeErr)
	if err != nil {
		return nil, err
	}
	mappedModel := account.GetMappedModel(originalModel)
	var geminiResp map[string]any
	if err := json.Unmarshal(geminiBody, &geminiResp); err != nil {
		return nil, s.writeGeminiCompatClientError(c, clientFormat, http.StatusBadGateway, "upstream_error", "Failed to parse upstream response")
	}
	if clientFormat == geminiCompatFormatResponses {
		responsesResp, converted, convErr := geminiResponseToResponses(geminiResp, originalModel, geminiBody, nil)
		if convErr != nil {
			return nil, s.writeGeminiCompatClientError(c, clientFormat, http.StatusBadGateway, "upstream_error", "Failed to parse upstream response")
		}
		if converted != nil && (converted.InputTokens > 0 || converted.OutputTokens > 0) {
			usage = *converted
		}
		if clientStream {
			writeResponsesStreamFromCollected(c, responsesResp)
		} else {
			c.JSON(http.StatusOK, responsesResp)
		}
		return &ForwardResult{Model: originalModel, UpstreamModel: mappedModel, Usage: usage, Stream: clientStream, Duration: time.Since(startTime)}, nil
	}
	claudeResp, converted := convertGeminiJSONToClaudeMessage(geminiBody, originalModel, true)
	if converted != nil && (converted.InputTokens > 0 || converted.OutputTokens > 0) {
		usage = *converted
	}
	chatResp, convErr := claudeMessageMapToChatCompletions(claudeResp, originalModel)
	if convErr != nil {
		return nil, s.writeGeminiCompatClientError(c, clientFormat, http.StatusBadGateway, "upstream_error", "Failed to parse upstream response")
	}
	if clientStream {
		writeChatCompletionsStreamFromCollected(c, chatResp)
	} else {
		c.JSON(http.StatusOK, chatResp)
	}
	return &ForwardResult{Model: originalModel, UpstreamModel: mappedModel, Usage: usage, Stream: clientStream, Duration: time.Since(startTime)}, nil
}

func convertGeminiJSONToClaudeMessage(geminiBody []byte, originalModel string, includeInlineData bool) (map[string]any, *ClaudeUsage) {
	var geminiResp map[string]any
	if err := json.Unmarshal(geminiBody, &geminiResp); err != nil {
		return map[string]any{"type": "message", "role": "assistant", "content": []any{}}, &ClaudeUsage{}
	}
	return convertGeminiToClaudeMessage(geminiResp, originalModel, geminiBody, includeInlineData)
}

func writeClaudeStreamFromCollectedGemini(c *gin.Context, claudeResp map[string]any, originalModel string, usage ClaudeUsage) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)
	flusher, _ := c.Writer.(http.Flusher)
	messageID := generateAnthropicMsgID()
	writeSSE(c.Writer, "message_start", map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id":            messageID,
			"type":          "message",
			"role":          "assistant",
			"model":         originalModel,
			"content":       []any{},
			"stop_reason":   nil,
			"stop_sequence": nil,
			"usage":         map[string]any{"input_tokens": 0, "output_tokens": 0},
		},
	})
	text := collectedClaudeText(claudeResp)
	if text != "" {
		writeSSE(c.Writer, "content_block_start", map[string]any{
			"type":  "content_block_start",
			"index": 0,
			"content_block": map[string]any{
				"type": "text",
				"text": "",
			},
		})
		writeSSE(c.Writer, "content_block_delta", map[string]any{
			"type":  "content_block_delta",
			"index": 0,
			"delta": map[string]any{"type": "text_delta", "text": text},
		})
		writeSSE(c.Writer, "content_block_stop", map[string]any{
			"type":  "content_block_stop",
			"index": 0,
		})
	}
	stopReason := strings.TrimSpace(fmt.Sprint(claudeResp["stop_reason"]))
	if stopReason == "" || stopReason == "<nil>" {
		stopReason = "end_turn"
	}
	writeSSE(c.Writer, "message_delta", map[string]any{
		"type": "message_delta",
		"delta": map[string]any{
			"stop_reason":   stopReason,
			"stop_sequence": nil,
		},
		"usage": map[string]any{
			"input_tokens":  usage.InputTokens,
			"output_tokens": usage.OutputTokens,
		},
	})
	writeSSE(c.Writer, "message_stop", map[string]any{"type": "message_stop"})
	if flusher != nil {
		flusher.Flush()
	}
}

func collectedClaudeText(claudeResp map[string]any) string {
	var text strings.Builder
	content, _ := claudeResp["content"].([]any)
	for _, item := range content {
		block, _ := item.(map[string]any)
		if block == nil {
			continue
		}
		if strings.TrimSpace(fmt.Sprint(block["type"])) != "text" {
			continue
		}
		text.WriteString(fmt.Sprint(block["text"]))
	}
	return text.String()
}

func writeChatCompletionsStreamFromCollected(c *gin.Context, chatResp map[string]any) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)
	flusher, _ := c.Writer.(http.Flusher)
	text := ""
	finish := "stop"
	if choices, ok := chatResp["choices"].([]map[string]any); ok && len(choices) > 0 {
		if msg, ok := choices[0]["message"].(map[string]any); ok {
			text = fmt.Sprint(msg["content"])
		}
		if fr, ok := choices[0]["finish_reason"].(string); ok && fr != "" {
			finish = fr
		}
	}
	chunk, _ := json.Marshal(map[string]any{
		"id":      fmt.Sprint(chatResp["id"]),
		"object":  "chat.completion.chunk",
		"created": time.Now().Unix(),
		"model":   chatResp["model"],
		"choices": []map[string]any{
			{
				"index": 0,
				"delta": map[string]any{
					"role":    "assistant",
					"content": text,
				},
				"finish_reason": finish,
			},
		},
	})
	_, _ = fmt.Fprintf(c.Writer, "data: %s\n\n", chunk)
	_, _ = io.WriteString(c.Writer, "data: [DONE]\n\n")
	if flusher != nil {
		flusher.Flush()
	}
}

func writeResponsesStreamFromCollected(c *gin.Context, responsesResp any) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)
	flusher, _ := c.Writer.(http.Flusher)
	payload, _ := json.Marshal(responsesResp)
	_, _ = fmt.Fprintf(c.Writer, "event: response.completed\ndata: %s\n\n", payload)
	if flusher != nil {
		flusher.Flush()
	}
}

func claudeMessageMapToChatCompletions(claudeResp map[string]any, model string) (map[string]any, error) {
	text := ""
	if content, ok := claudeResp["content"].([]any); ok {
		for _, item := range content {
			block, _ := item.(map[string]any)
			if block == nil {
				continue
			}
			if strings.TrimSpace(fmt.Sprint(block["type"])) != "text" {
				continue
			}
			text += fmt.Sprint(block["text"])
		}
	}
	stopReason := strings.TrimSpace(fmt.Sprint(claudeResp["stop_reason"]))
	finish := "stop"
	if stopReason == "max_tokens" {
		finish = "length"
	}
	usageIn, usageOut := 0, 0
	if usage, ok := claudeResp["usage"].(map[string]any); ok {
		usageIn, _ = usage["input_tokens"].(int)
		usageOut, _ = usage["output_tokens"].(int)
		if usageIn == 0 {
			if n, ok := usage["input_tokens"].(float64); ok {
				usageIn = int(n)
			}
		}
		if usageOut == 0 {
			if n, ok := usage["output_tokens"].(float64); ok {
				usageOut = int(n)
			}
		}
	}
	return map[string]any{
		"id":      fmt.Sprint(claudeResp["id"]),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []map[string]any{
			{
				"index": 0,
				"message": map[string]any{
					"role":    "assistant",
					"content": text,
				},
				"finish_reason": finish,
			},
		},
		"usage": map[string]any{
			"prompt_tokens":     usageIn,
			"completion_tokens": usageOut,
			"total_tokens":      usageIn + usageOut,
		},
	}, nil
}

func chatCompletionsToGeminiNative(raw []byte, model string) ([]byte, ClaudeUsage) {
	text := gjson.GetBytes(raw, "choices.0.message.content").String()
	finish := gjson.GetBytes(raw, "choices.0.finish_reason").String()
	usage, _ := extractOpenAIUsageFromJSONBytes(raw)
	return geminiBridgePayload(text, model, geminiBridgeFinishReason(finish), geminiBridgeUsageMetadata(usage, geminiBridgeUsageObject(raw))), geminiBridgeClaudeUsage(usage)
}

func geminiBridgeClaudeUsage(usage OpenAIUsage) ClaudeUsage {
	return ClaudeUsage{
		InputTokens:  max(usage.InputTokens-usage.CacheReadInputTokens-usage.CacheCreationInputTokens, 0),
		OutputTokens: usage.OutputTokens, CacheReadInputTokens: usage.CacheReadInputTokens,
		CacheCreationInputTokens: usage.CacheCreationInputTokens, ImageOutputTokens: usage.ImageOutputTokens,
	}
}

func geminiBridgeUsageObject(raw []byte) gjson.Result {
	if usage := gjson.GetBytes(raw, "response.usage"); usage.IsObject() {
		return usage
	}
	return gjson.GetBytes(raw, "usage")
}

func geminiBridgeUsageMetadata(usage OpenAIUsage, raw gjson.Result) map[string]any {
	thoughts := max(int(firstPositiveGJSONInt(raw.Get("completion_tokens_details.reasoning_tokens"), raw.Get("output_tokens_details.reasoning_tokens"))), 0)
	metadata := map[string]any{
		"promptTokenCount":        usage.InputTokens,
		"candidatesTokenCount":    max(usage.OutputTokens-thoughts, 0),
		"totalTokenCount":         usage.InputTokens + usage.OutputTokens,
		"cachedContentTokenCount": usage.CacheReadInputTokens,
	}
	if thoughts > 0 {
		metadata["thoughtsTokenCount"] = thoughts
	}
	return metadata
}

func geminiBridgeFinishReason(finish string) string {
	switch finish {
	case "length":
		return "MAX_TOKENS"
	case "content_filter":
		return "SAFETY"
	default:
		return "STOP"
	}
}

func geminiBridgePayload(text, model, finish string, metadata map[string]any) []byte {
	candidate := map[string]any{
		"content": map[string]any{"role": "model", "parts": []map[string]string{{"text": text}}},
	}
	if finish != "" {
		candidate["finishReason"] = finish
	}
	payload := map[string]any{
		"candidates": []map[string]any{
			candidate,
		},
		"modelVersion": model,
	}
	if metadata != nil {
		payload["usageMetadata"] = metadata
	}
	data, _ := json.Marshal(payload)
	return data
}

func (s *GeminiMessagesCompatService) pipeOpenAIStreamAsGemini(ctx context.Context, c *gin.Context, body io.Reader, model, protocol string, start time.Time) (*ForwardResult, error) {
	result := &ForwardResult{Model: model, UpstreamModel: model, Stream: true}
	streamBalanceGuard, _ := BalancePreauthorizationGuardFromContext(ctx)
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Status(http.StatusOK)
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var parser openAICompatSSEFrameParser
	state := apicompat.NewResponsesEventToChatState()
	state.Model = model
	var metadata map[string]any
	var streamErr error
	streamStatus := http.StatusBadGateway
	streamMessage := "Gemini upstream stream did not complete"
	finish := ""
	terminal := false
	sawOutput := false
	write := func(data []byte) error {
		if result.ClientDisconnect {
			return io.ErrClosedPipe
		}
		if err := ctx.Err(); err != nil {
			result.ClientDisconnect = true
			return err
		}
		MarkResponseCommitted(c)
		if _, err := fmt.Fprintf(c.Writer, "data: %s\n\n", data); err != nil {
			result.ClientDisconnect = true
			return err
		}
		c.Writer.Flush()
		return nil
	}
	consume := func(frame openAICompatSSEFrame, ok bool) {
		if !ok || terminal || result.ClientDisconnect || ctx.Err() != nil {
			return
		}
		parsed := parseOpenAISSEDataFrame([]byte(frame.Data), frame.EventType)
		if parsed.isDone() {
			if protocol == APIProtocolChatCompletions && finish != "" {
				terminal = true
			}
			return
		}
		if !parsed.validJSON {
			streamErr = errors.New("invalid Gemini upstream stream event")
			terminal = true
			return
		}
		if usage, ok := openAIUsageFromGJSON(geminiBridgeUsageObject(parsed.data)); ok {
			result.Usage = geminiBridgeClaudeUsage(usage)
			metadata = geminiBridgeUsageMetadata(usage, geminiBridgeUsageObject(parsed.data))
		}
		sawOutput = sawOutput || geminiBridgeHasOutput(parsed.data, result.Usage) || openAIStreamFrameStartsVisibleOutput(parsed)
		if sawOutput {
			result.NonBillableUpstreamError = false
		}
		if status := geminiBridgeFailureStatus(parsed.data, parsed.eventType); status != 0 {
			if !sawOutput && geminiBridgeNonBillableFailure(status, parsed.data) {
				result.NonBillableUpstreamError = true
			}
			streamStatus = status
			streamErr = fmt.Errorf("gemini upstream stream failed: %s", parsed.eventType)
			// A Responses error prelude can be followed by a terminal usage snapshot.
			terminal = protocol != APIProtocolResponses || (parsed.eventType != "error" && parsed.eventType != "")
			return
		}
		if streamErr != nil {
			terminal = isOpenAICompatResponsesTerminalEvent(parsed.eventType)
			return
		}
		var chunks []apicompat.ChatCompletionsChunk
		if protocol == APIProtocolResponses {
			var event apicompat.ResponsesStreamEvent
			if err := json.Unmarshal(parsed.data, &event); err != nil {
				streamErr = err
				terminal = true
				return
			}
			event.Type = parsed.eventType
			chunks = apicompat.ResponsesEventToChatChunks(&event, state)
			terminal = isOpenAICompatResponsesTerminalEvent(event.Type)
		} else {
			var chunk apicompat.ChatCompletionsChunk
			if err := json.Unmarshal(parsed.data, &chunk); err != nil {
				streamErr = err
				terminal = true
				return
			}
			chunks = []apicompat.ChatCompletionsChunk{chunk}
		}
		for _, chunk := range chunks {
			for _, choice := range chunk.Choices {
				if choice.Index != 0 {
					continue
				}
				if choice.Delta.Content != nil && *choice.Delta.Content != "" {
					if err := ctx.Err(); err != nil {
						result.ClientDisconnect = true
						streamErr = err
						terminal = true
						return
					}
					if topUpErr := streamBalanceGuard.ObserveStreamingOutput(ctx, len(*choice.Delta.Content)); topUpErr != nil {
						streamErr = wrapStreamOutputHoldTopUpFailure(topUpErr)
						streamStatus = http.StatusServiceUnavailable
						if errors.Is(topUpErr, ErrBalanceWithholdingFailed) {
							streamStatus = http.StatusForbidden
						}
						streamMessage = "Streaming output balance reservation failed"
						terminal = true
						return
					}
					if result.FirstTokenMs == nil {
						ms := int(time.Since(start).Milliseconds())
						result.FirstTokenMs = &ms
					}
					if err := write(geminiBridgePayload(*choice.Delta.Content, model, "", nil)); err != nil {
						streamErr = err
						terminal = true
						return
					}
				}
				if choice.FinishReason != nil {
					finish = geminiBridgeFinishReason(*choice.FinishReason)
				}
			}
		}
	}
	for !terminal && !result.ClientDisconnect && ctx.Err() == nil && scanner.Scan() {
		consume(parser.AddLine(scanner.Text()))
	}
	consume(parser.Finish())
	if streamErr == nil {
		streamErr = scanner.Err()
	}
	if ctx.Err() != nil {
		result.ClientDisconnect = true
		streamErr = ctx.Err()
	}
	if streamErr == nil && !terminal {
		streamErr = io.ErrUnexpectedEOF
	}
	if streamErr == nil && metadata == nil {
		streamErr = errors.New("gemini upstream stream omitted usage")
	}
	if streamErr != nil {
		payload, _ := json.Marshal(map[string]any{"error": map[string]any{
			"code": streamStatus, "status": googleapi.HTTPStatusToGoogleStatus(streamStatus), "message": streamMessage,
		}, "usageMetadata": metadata})
		_ = write(payload)
		return result, streamErr
	}
	return result, write(geminiBridgePayload("", model, finish, metadata))
}

func extractGeminiBridgeErrorMessage(raw []byte) string {
	for _, path := range []string{"response.error.message", "error.message", "message"} {
		if message := gjson.GetBytes(raw, path).String(); message != "" {
			return message
		}
	}
	return "Gemini upstream request failed"
}

func geminiBridgeFailureStatus(raw []byte, eventType string) int {
	status := gjson.GetBytes(raw, "response.status").String()
	if status == "" {
		status = gjson.GetBytes(raw, "status").String()
	}
	if eventType == "error" || eventType == "response.failed" || eventType == "response.cancelled" || eventType == "response.canceled" ||
		status == "failed" || status == "cancelled" || status == "canceled" || gjson.GetBytes(raw, "error").IsObject() || gjson.GetBytes(raw, "response.error").IsObject() {
		code := firstNonEmpty(openAIStreamFailedEventErrorCode(raw), gjson.GetBytes(raw, "code").String())
		if strings.EqualFold(code, "input_too_small") || isOpenAINonBillableRequestError("", raw) {
			return http.StatusBadRequest
		}
		if isOpenAIDeterministicClientErrorMessage("", raw) {
			return openAIDeterministicClientHTTPStatus("", raw)
		}
		return openAIStreamFailedEventSemanticStatus(raw, extractGeminiBridgeErrorMessage(raw))
	}
	return 0
}

func geminiBridgeNonBillableFailure(status int, raw []byte) bool {
	if status != http.StatusBadRequest {
		return false
	}
	policy, _, _ := detectOpenAICyberPolicy(raw)
	return !policy
}

func geminiBridgeHasOutput(raw []byte, usage ClaudeUsage) bool {
	if usage.OutputTokens > 0 || usage.ImageOutputTokens > 0 {
		return true
	}
	root := gjson.ParseBytes(raw)
	for _, path := range []string{"output", "response.output"} {
		for _, item := range root.Get(path).Array() {
			if openAIStreamItemHasVisibleOutput(item) {
				return true
			}
		}
	}
	for _, choice := range root.Get("choices").Array() {
		for _, path := range []string{"message", "delta"} {
			message := choice.Get(path)
			if message.Get("content").String() != "" || message.Get("reasoning_content").String() != "" || len(message.Get("tool_calls").Array()) > 0 {
				return true
			}
		}
	}
	return false
}
