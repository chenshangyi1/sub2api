//go:build unit

package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Gemini 原生路径带内信号契约：
//   - 内容策略 / 错误信封：客户端字节原样透传，只登记 ops；用量照常从 usageMetadata 提取。
//   - 空响应（EMPTY_STREAM / EMPTY_RESPONSE）：尚未写出语义字节时转为账号 failover
//     （502，同账号重试 1 次），避免中转 2xx 空体被当成成功。
//   - 内容策略类不归因上游账号、不计入 SLA；错误信封与空响应按上游失败归因并计入 SLA。
// ---------------------------------------------------------------------------

const geminiSignalTestFinishMessage = "The model output could not be generated. This output contains sensitive words that violate Google's [Generative AI Prohibited Use policy](https://policies.google.com/terms/generative-ai/use-policy). If you think this was an error, [send feedback](https://ai.google.dev/gemini-api/docs/troubleshooting)."

const geminiSignalTestProhibitedSSE = `data: {"candidates":[{"content":{"parts":[{"text":"hello"}],"role":"model"}}],"modelVersion":"gemini-3.7-flash","responseId":"r1","usageMetadata":{"candidatesTokenCount":26,"promptTokenCount":21775,"thoughtsTokenCount":606,"totalTokenCount":22407}}

data: {"candidates":[{"content":{"parts":[{"text":" world"}],"role":"model"}}],"modelVersion":"gemini-3.7-flash","responseId":"r1","usageMetadata":{"candidatesTokenCount":1289,"promptTokenCount":21775,"thoughtsTokenCount":606,"totalTokenCount":23670}}

data: {"candidates":[{"content":{},"finishMessage":"` + geminiSignalTestFinishMessage + `","finishReason":"PROHIBITED_CONTENT"}],"modelVersion":"gemini-3.7-flash","responseId":"r1","usageMetadata":{"candidatesTokenCount":1289,"promptTokenCount":21775,"thoughtsTokenCount":606,"totalTokenCount":23670}}

`

const geminiSignalTestStopSSE = `data: {"candidates":[{"content":{"parts":[{"text":"hello"}],"role":"model"}}],"usageMetadata":{"candidatesTokenCount":3,"promptTokenCount":10,"totalTokenCount":13}}

data: {"candidates":[{"content":{"parts":[{"text":"!"}],"role":"model"},"finishReason":"STOP"}],"usageMetadata":{"candidatesTokenCount":4,"promptTokenCount":10,"totalTokenCount":14}}

`

func geminiSignalTestAccount() *Account {
	return &Account{
		ID:       703,
		Name:     "gemini-signal-test",
		Platform: PlatformGemini,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key": "test-key",
		},
	}
}

type geminiSignalHTTPUpstreamStub struct {
	response *http.Response
}

func (s *geminiSignalHTTPUpstreamStub) Do(req *http.Request, proxyURL string, accountID int64, accountConcurrency int) (*http.Response, error) {
	if s == nil || s.response == nil {
		return nil, io.ErrUnexpectedEOF
	}
	resp := *s.response
	return &resp, nil
}

func (s *geminiSignalHTTPUpstreamStub) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, profile *tlsfingerprint.Profile) (*http.Response, error) {
	return s.Do(req, proxyURL, accountID, accountConcurrency)
}

func newGeminiSignalService(contentType string, body string) *GeminiMessagesCompatService {
	httpStub := &geminiSignalHTTPUpstreamStub{
		response: &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{contentType}, "X-Request-Id": []string{"upstream-req-1"}},
			Body:       io.NopCloser(strings.NewReader(body)),
		},
	}
	return &GeminiMessagesCompatService{
		httpUpstream: httpStub,
		cfg:          &config.Config{},
	}
}

func geminiSignalTestRequest() []byte {
	return []byte(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`)
}

func geminiSignalTestContext(t *testing.T) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-2.5-flash:generateContent", strings.NewReader("{}"))
	return c, rec
}

func upstreamErrorEventsFromContext(t *testing.T, c *gin.Context) []*OpsUpstreamErrorEvent {
	t.Helper()
	v, ok := c.Get(OpsUpstreamErrorsKey)
	if !ok {
		return nil
	}
	events, ok := v.([]*OpsUpstreamErrorEvent)
	require.True(t, ok)
	return events
}

func requireGeminiEmptyFailover(t *testing.T, err error, rec *httptest.ResponseRecorder, stream bool) {
	t.Helper()
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Equal(t, http.StatusBadGateway, failoverErr.StatusCode)
	require.True(t, failoverErr.RetryableOnSameAccount)
	require.Equal(t, 1, failoverErr.SameAccountRetryMax)
	require.Equal(t, stream, failoverErr.SafeToFailoverAfterWrite)
	if rec != nil && !stream {
		require.Empty(t, rec.Body.String(), "non-stream empty 2xx must not reach the client")
	}
}

func TestGeminiForwardNative_StreamProhibitedContentMarksInBandErrorAndKeepsBillingUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := newGeminiSignalService("text/event-stream; charset=utf-8", geminiSignalTestProhibitedSSE)
	c, rec := geminiSignalTestContext(t)

	result, err := svc.ForwardNative(context.Background(), c, geminiSignalTestAccount(),
		"gemini-3.7-flash", "streamGenerateContent", true, geminiSignalTestRequest())
	require.NoError(t, err)
	require.NotNil(t, result)

	// 客户端字节原样透传，wire 状态 200。
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, geminiSignalTestProhibitedSSE, rec.Body.String())

	// 用量照常提取：input=promptTokenCount，output=candidates+thoughts。
	require.Equal(t, 21775, result.Usage.InputTokens)
	require.Equal(t, 1289+606, result.Usage.OutputTokens)
	require.NotNil(t, result.FirstTokenMs)

	// 带内错误登记为请求级内容策略：不计 SLA、不归因上游账号。
	streamErrs := GetOpsStreamErrors(c)
	require.Len(t, streamErrs, 1)
	require.Equal(t, "invalid_request_error", streamErrs[0].ErrType)
	require.Equal(t, "PROHIBITED_CONTENT", streamErrs[0].Code)
	require.Equal(t, http.StatusBadRequest, streamErrs[0].IntendedStatus)
	require.False(t, streamErrs[0].CountTowardsSLA)
	require.True(t, streamErrs[0].RequestScoped)
	require.False(t, streamErrs[0].NonStream)
	require.Contains(t, streamErrs[0].Message, "finishReason=PROHIBITED_CONTENT")
	require.Contains(t, streamErrs[0].Message, "Prohibited Use policy")

	_, hasUpstreamStatus := c.Get(OpsUpstreamStatusCodeKey)
	require.False(t, hasUpstreamStatus, "内容策略不应写上游错误上下文")
	require.Empty(t, upstreamErrorEventsFromContext(t, c))
}

func TestGeminiForwardNative_StreamErrorEnvelopeMarksUpstreamFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := `data: {"candidates":[{"content":{"parts":[{"text":"partial"}],"role":"model"}}],"usageMetadata":{"candidatesTokenCount":2,"promptTokenCount":10,"totalTokenCount":12}}

data: {"error":{"code":429,"message":"Resource has been exhausted (e.g. check quota).","status":"RESOURCE_EXHAUSTED"}}

`
	svc := newGeminiSignalService("text/event-stream", body)
	c, rec := geminiSignalTestContext(t)

	result, err := svc.ForwardNative(context.Background(), c, geminiSignalTestAccount(),
		"gemini-3.7-flash", "streamGenerateContent", true, geminiSignalTestRequest())
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, body, rec.Body.String(), "错误信封也原样透传，交给 SDK 客户端按错误处理")
	require.Equal(t, 10, result.Usage.InputTokens)
	require.Equal(t, 2, result.Usage.OutputTokens)

	streamErrs := GetOpsStreamErrors(c)
	require.Len(t, streamErrs, 1)
	require.Equal(t, "rate_limit_error", streamErrs[0].ErrType)
	require.Equal(t, "RESOURCE_EXHAUSTED", streamErrs[0].Code)
	require.Equal(t, http.StatusTooManyRequests, streamErrs[0].IntendedStatus)
	require.True(t, streamErrs[0].CountTowardsSLA)
	require.False(t, streamErrs[0].RequestScoped)
	require.False(t, streamErrs[0].NonStream)
	require.Equal(t, "Resource has been exhausted (e.g. check quota).", streamErrs[0].Message)

	status, ok := c.Get(OpsUpstreamStatusCodeKey)
	require.True(t, ok)
	require.Equal(t, http.StatusTooManyRequests, status)

	events := upstreamErrorEventsFromContext(t, c)
	require.Len(t, events, 1)
	require.Equal(t, "stream_failed", events[0].Kind)
	require.Equal(t, int64(703), events[0].AccountID)
	require.Equal(t, PlatformGemini, events[0].Platform)
	require.Equal(t, http.StatusTooManyRequests, events[0].UpstreamStatusCode)
	require.Equal(t, "upstream-req-1", events[0].UpstreamRequestID)
}

func TestGeminiForwardNative_StreamPromptBlockedMarksContentPolicy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := `data: {"promptFeedback":{"blockReason":"SAFETY","safetyRatings":[{"category":"HARM_CATEGORY_HARASSMENT","probability":"HIGH","blocked":true}]},"usageMetadata":{"promptTokenCount":10,"totalTokenCount":10}}

`
	svc := newGeminiSignalService("text/event-stream", body)
	c, rec := geminiSignalTestContext(t)

	result, err := svc.ForwardNative(context.Background(), c, geminiSignalTestAccount(),
		"gemini-3.7-flash", "streamGenerateContent", true, geminiSignalTestRequest())
	require.NoError(t, err)
	require.Equal(t, body, rec.Body.String())
	require.Equal(t, 10, result.Usage.InputTokens)

	streamErrs := GetOpsStreamErrors(c)
	require.Len(t, streamErrs, 1)
	require.Equal(t, "invalid_request_error", streamErrs[0].ErrType)
	require.Equal(t, "SAFETY", streamErrs[0].Code)
	require.Contains(t, streamErrs[0].Message, "blockReason=SAFETY")
	require.False(t, streamErrs[0].CountTowardsSLA)
	require.Empty(t, upstreamErrorEventsFromContext(t, c))
}

func TestGeminiForwardNative_EmptyStreamMarksUpstreamFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := newGeminiSignalService("text/event-stream", "")
	c, rec := geminiSignalTestContext(t)

	result, err := svc.ForwardNative(context.Background(), c, geminiSignalTestAccount(),
		"gemini-3.7-flash", "streamGenerateContent", true, geminiSignalTestRequest())
	require.Nil(t, result)
	requireGeminiEmptyFailover(t, err, rec, true)

	streamErrs := GetOpsStreamErrors(c)
	require.Len(t, streamErrs, 1)
	require.Equal(t, "upstream_error", streamErrs[0].ErrType)
	require.Equal(t, geminiSignalEmptyStreamReason, streamErrs[0].Code)
	require.Equal(t, http.StatusBadGateway, streamErrs[0].IntendedStatus)
	require.True(t, streamErrs[0].CountTowardsSLA)

	events := upstreamErrorEventsFromContext(t, c)
	require.Len(t, events, 1)
	require.Equal(t, "stream_failed", events[0].Kind)
	require.Equal(t, int64(703), events[0].AccountID)
}

func TestGeminiForwardNative_NormalStreamLeavesNoMark(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := newGeminiSignalService("text/event-stream", geminiSignalTestStopSSE)
	c, rec := geminiSignalTestContext(t)

	result, err := svc.ForwardNative(context.Background(), c, geminiSignalTestAccount(),
		"gemini-3.7-flash", "streamGenerateContent", true, geminiSignalTestRequest())
	require.NoError(t, err)
	require.Equal(t, geminiSignalTestStopSSE, rec.Body.String())
	require.Equal(t, 4, result.Usage.OutputTokens)
	require.Empty(t, GetOpsStreamErrors(c))
	require.Empty(t, upstreamErrorEventsFromContext(t, c))
	_, hasUpstreamStatus := c.Get(OpsUpstreamStatusCodeKey)
	require.False(t, hasUpstreamStatus)
}

func TestGeminiForwardNative_NonStreamProhibitedContentMarksInBandError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := `{"candidates":[{"content":{},"finishMessage":"` + geminiSignalTestFinishMessage + `","finishReason":"PROHIBITED_CONTENT"}],"modelVersion":"gemini-3.7-flash","usageMetadata":{"candidatesTokenCount":120,"promptTokenCount":900,"thoughtsTokenCount":30,"totalTokenCount":1050}}`
	svc := newGeminiSignalService("application/json", body)
	c, rec := geminiSignalTestContext(t)

	result, err := svc.ForwardNative(context.Background(), c, geminiSignalTestAccount(),
		"gemini-3.7-flash", "generateContent", false, geminiSignalTestRequest())
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, body, rec.Body.String())
	require.Equal(t, 900, result.Usage.InputTokens)
	require.Equal(t, 150, result.Usage.OutputTokens)

	streamErrs := GetOpsStreamErrors(c)
	require.Len(t, streamErrs, 1)
	require.Equal(t, "invalid_request_error", streamErrs[0].ErrType)
	require.Equal(t, "PROHIBITED_CONTENT", streamErrs[0].Code)
	require.False(t, streamErrs[0].CountTowardsSLA)
	require.True(t, streamErrs[0].RequestScoped)
	require.True(t, streamErrs[0].NonStream)
	require.Empty(t, upstreamErrorEventsFromContext(t, c))
}

func TestGeminiForwardNative_NonStreamErrorEnvelopeOn200MarksUpstreamFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := `{"error":{"code":500,"message":"Internal error encountered.","status":"INTERNAL"}}`
	svc := newGeminiSignalService("application/json", body)
	c, rec := geminiSignalTestContext(t)

	result, err := svc.ForwardNative(context.Background(), c, geminiSignalTestAccount(),
		"gemini-3.7-flash", "generateContent", false, geminiSignalTestRequest())
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, body, rec.Body.String())

	streamErrs := GetOpsStreamErrors(c)
	require.Len(t, streamErrs, 1)
	require.Equal(t, "upstream_error", streamErrs[0].ErrType)
	require.Equal(t, "INTERNAL", streamErrs[0].Code)
	require.Equal(t, http.StatusInternalServerError, streamErrs[0].IntendedStatus)
	require.True(t, streamErrs[0].CountTowardsSLA)

	require.True(t, streamErrs[0].NonStream)

	events := upstreamErrorEventsFromContext(t, c)
	require.Len(t, events, 1)
	require.Equal(t, "http_error", events[0].Kind)
}

func TestGeminiForwardNative_StreamErrorEnvelopeAfterContentFilterWins(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := `data: {"candidates":[{"content":{},"finishReason":"SAFETY"}]}

data: {"error":{"code":503,"message":"The model is overloaded. Please try again later.","status":"UNAVAILABLE"}}

`
	svc := newGeminiSignalService("text/event-stream", body)
	c, rec := geminiSignalTestContext(t)

	_, err := svc.ForwardNative(context.Background(), c, geminiSignalTestAccount(),
		"gemini-3.7-flash", "streamGenerateContent", true, geminiSignalTestRequest())
	require.NoError(t, err)
	require.Equal(t, body, rec.Body.String())

	streamErrs := GetOpsStreamErrors(c)
	require.Len(t, streamErrs, 1)
	require.Equal(t, "UNAVAILABLE", streamErrs[0].Code, "错误信封优先于内容过滤")
	require.Equal(t, "upstream_error", streamErrs[0].ErrType)
	require.Equal(t, http.StatusServiceUnavailable, streamErrs[0].IntendedStatus)
	require.True(t, streamErrs[0].CountTowardsSLA)
	require.Len(t, upstreamErrorEventsFromContext(t, c), 1)
}

func TestGeminiForwardNative_StreamNonSSEJSONBodyIsInspected(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Run("json array with content filter", func(t *testing.T) {
		body := `[{
  "candidates": [{"content": {"parts": [{"text": "hello"}], "role": "model"}}],
  "usageMetadata": {"promptTokenCount": 10, "candidatesTokenCount": 1}
}
,
{
  "candidates": [{"content": {}, "finishReason": "PROHIBITED_CONTENT"}]
}
]
`
		svc := newGeminiSignalService("application/json", body)
		c, rec := geminiSignalTestContext(t)
		_, err := svc.ForwardNative(context.Background(), c, geminiSignalTestAccount(),
			"gemini-3.7-flash", "streamGenerateContent", true, geminiSignalTestRequest())
		require.NoError(t, err)
		require.Equal(t, body, rec.Body.String())
		streamErrs := GetOpsStreamErrors(c)
		require.Len(t, streamErrs, 1)
		require.Equal(t, "PROHIBITED_CONTENT", streamErrs[0].Code)
		require.True(t, streamErrs[0].RequestScoped)
	})
	t.Run("json object error envelope", func(t *testing.T) {
		body := `{"error":{"code":429,"message":"quota","status":"RESOURCE_EXHAUSTED"}}` + "\n"
		svc := newGeminiSignalService("application/json", body)
		c, _ := geminiSignalTestContext(t)
		_, err := svc.ForwardNative(context.Background(), c, geminiSignalTestAccount(),
			"gemini-3.7-flash", "streamGenerateContent", true, geminiSignalTestRequest())
		require.NoError(t, err)
		streamErrs := GetOpsStreamErrors(c)
		require.Len(t, streamErrs, 1)
		require.Equal(t, "RESOURCE_EXHAUSTED", streamErrs[0].Code)
		require.True(t, streamErrs[0].CountTowardsSLA)
	})
	t.Run("healthy json object leaves no mark", func(t *testing.T) {
		body := `{"candidates":[{"content":{"parts":[{"text":"hello"}],"role":"model"},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":1}}` + "\n"
		svc := newGeminiSignalService("application/json", body)
		c, _ := geminiSignalTestContext(t)
		_, err := svc.ForwardNative(context.Background(), c, geminiSignalTestAccount(),
			"gemini-3.7-flash", "streamGenerateContent", true, geminiSignalTestRequest())
		require.NoError(t, err)
		require.Empty(t, GetOpsStreamErrors(c))
		require.Empty(t, upstreamErrorEventsFromContext(t, c))
	})
}

func TestGeminiForwardNative_StreamOversizedNonSSEBodyLeavesNoMark(t *testing.T) {
	gin.SetMode(gin.TestMode)
	filler := strings.Repeat("x", geminiSSEFallbackBodyLimit)
	body := `{"candidates":[{"content":{"parts":[{"text":"` + filler + `"}],"role":"model"},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":1}}` + "\n"
	svc := newGeminiSignalService("application/json", body)
	c, rec := geminiSignalTestContext(t)
	_, err := svc.ForwardNative(context.Background(), c, geminiSignalTestAccount(),
		"gemini-3.7-flash", "streamGenerateContent", true, geminiSignalTestRequest())
	require.NoError(t, err)
	require.Equal(t, len(body), rec.Body.Len())
	require.Empty(t, GetOpsStreamErrors(c), "超限的兜底体放弃判定，不得记成空流")
	require.Empty(t, upstreamErrorEventsFromContext(t, c))
}

func TestGeminiForwardNative_StreamNonSSEEmptyBodyIsEmpty(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for name, body := range map[string]string{
		"empty object":     "{}\n",
		"empty candidates": `{"candidates":[],"usageMetadata":{"promptTokenCount":3}}` + "\n",
		"empty array":      "[]\n",
		"array of empties": "[{\"candidates\":[]}\n,\n{}]\n",
	} {
		t.Run(name, func(t *testing.T) {
			svc := newGeminiSignalService("application/json", body)
			c, rec := geminiSignalTestContext(t)
			_, err := svc.ForwardNative(context.Background(), c, geminiSignalTestAccount(),
				"gemini-3.7-flash", "streamGenerateContent", true, geminiSignalTestRequest())
			requireGeminiEmptyFailover(t, err, rec, true)
			streamErrs := GetOpsStreamErrors(c)
			require.Len(t, streamErrs, 1)
			require.Equal(t, geminiSignalEmptyStreamReason, streamErrs[0].Code)
			require.True(t, streamErrs[0].CountTowardsSLA)
		})
	}
}

func TestGeminiForwardNative_StreamWithoutDataEventsIsEmpty(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for name, body := range map[string]string{
		"keepalive comments only": ": keepalive\n\n: keepalive\n\n",
		"done marker only":        "data: [DONE]\n\n",
		"blank data only":         "data: \n\n",
		"whitespace only":         "\n\n   \n",
	} {
		t.Run(name, func(t *testing.T) {
			svc := newGeminiSignalService("text/event-stream", body)
			c, rec := geminiSignalTestContext(t)
			_, err := svc.ForwardNative(context.Background(), c, geminiSignalTestAccount(),
				"gemini-3.7-flash", "streamGenerateContent", true, geminiSignalTestRequest())
			requireGeminiEmptyFailover(t, err, rec, true)
			streamErrs := GetOpsStreamErrors(c)
			require.Len(t, streamErrs, 1)
			require.Equal(t, geminiSignalEmptyStreamReason, streamErrs[0].Code)
			require.True(t, streamErrs[0].CountTowardsSLA)
			require.False(t, streamErrs[0].NonStream)
		})
	}
}

func TestGeminiForwardNative_StreamOtherFinishReasonLeavesNoMark(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := `data: {"candidates":[{"content":{"parts":[{"text":"full answer"}],"role":"model"},"finishReason":"OTHER"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":2}}

`
	svc := newGeminiSignalService("text/event-stream", body)
	c, _ := geminiSignalTestContext(t)
	result, err := svc.ForwardNative(context.Background(), c, geminiSignalTestAccount(),
		"gemini-3.7-flash", "streamGenerateContent", true, geminiSignalTestRequest())
	require.NoError(t, err)
	require.Equal(t, 2, result.Usage.OutputTokens)
	require.Empty(t, GetOpsStreamErrors(c))
	require.Empty(t, upstreamErrorEventsFromContext(t, c))
}

func TestGeminiForwardNative_StreamMalformedFunctionCallLeavesNoMark(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := `data: {"candidates":[{"content":{"parts":[{"text":"partial"}],"role":"model"},"finishReason":"MALFORMED_FUNCTION_CALL"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":2}}

`
	svc := newGeminiSignalService("text/event-stream", body)
	c, _ := geminiSignalTestContext(t)
	result, err := svc.ForwardNative(context.Background(), c, geminiSignalTestAccount(),
		"gemini-3.7-flash", "streamGenerateContent", true, geminiSignalTestRequest())
	require.NoError(t, err)
	require.Equal(t, 2, result.Usage.OutputTokens)
	require.Empty(t, GetOpsStreamErrors(c))
	require.Empty(t, upstreamErrorEventsFromContext(t, c))
}

func TestGeminiForwardNative_NonStreamEmptyBodyMarksEmptyResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for name, body := range map[string]string{
		"blank":            "",
		"empty object":     "{}",
		"empty candidates": `{"candidates":[],"usageMetadata":{"promptTokenCount":3,"totalTokenCount":3}}`,
	} {
		t.Run(name, func(t *testing.T) {
			svc := newGeminiSignalService("application/json", body)
			c, rec := geminiSignalTestContext(t)
			result, err := svc.ForwardNative(context.Background(), c, geminiSignalTestAccount(),
				"gemini-3.7-flash", "generateContent", false, geminiSignalTestRequest())
			require.Nil(t, result)
			requireGeminiEmptyFailover(t, err, rec, false)
			streamErrs := GetOpsStreamErrors(c)
			require.Len(t, streamErrs, 1)
			require.Equal(t, geminiSignalEmptyResponseReason, streamErrs[0].Code)
			require.Equal(t, "upstream_error", streamErrs[0].ErrType)
			require.True(t, streamErrs[0].CountTowardsSLA)
			require.True(t, streamErrs[0].NonStream)
			events := upstreamErrorEventsFromContext(t, c)
			require.Len(t, events, 1)
			require.Equal(t, "http_error", events[0].Kind)
		})
	}
}

func TestGeminiForwardNative_CountTokensBodyLeavesNoMark(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := newGeminiSignalService("application/json", `{"totalTokens":12}`)
	c, rec := geminiSignalTestContext(t)
	_, err := svc.ForwardNative(context.Background(), c, geminiSignalTestAccount(),
		"gemini-3.7-flash", "countTokens", false, geminiSignalTestRequest())
	require.NoError(t, err)
	require.Equal(t, `{"totalTokens":12}`, rec.Body.String())
	require.Empty(t, GetOpsStreamErrors(c))
	require.Empty(t, upstreamErrorEventsFromContext(t, c))
}

func TestCollectGeminiSSEObserved_SeesTerminalChunkEvenWhenAggregateDropsIt(t *testing.T) {
	var observed []string
	collected, usage, stats, err := collectGeminiSSEObserved(strings.NewReader(geminiSignalTestProhibitedSSE), false, func(raw []byte) {
		observed = append(observed, string(raw))
	})
	require.NoError(t, err)
	require.Len(t, observed, 3)
	require.Equal(t, 3, stats.dataEvents)
	require.Empty(t, stats.fallback.Bytes())
	require.Equal(t, 21775, usage.InputTokens)

	var hit bool
	for _, raw := range observed {
		if sig, ok := detectGeminiResponseSignal([]byte(raw)); ok {
			hit = true
			require.Equal(t, geminiSignalContentFilter, sig.Kind)
		}
	}
	require.True(t, hit)
	// 聚合结果沿用"最后一个带 parts 的块"，本身不携带末块的 finishReason；检测必须逐事件进行。
	aggregated, err := json.Marshal(collected)
	require.NoError(t, err)
	require.NotContains(t, string(aggregated), "PROHIBITED_CONTENT")
}

func TestGeminiForwardAsChatCompletions_NonStreamEmptyBodyFailsOver(t *testing.T) {
	gin.SetMode(gin.TestMode)
	httpStub := &geminiSignalHTTPUpstreamStub{
		response: &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"candidates":[],"usageMetadata":{"promptTokenCount":3}}`)),
		},
	}
	svc := &GeminiMessagesCompatService{httpUpstream: httpStub, cfg: &config.Config{}}
	account := &Account{
		ID:          201,
		Platform:    PlatformGemini,
		Type:        AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "test-key"},
	}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	body := []byte(`{"model":"gemini-2.5-pro","messages":[{"role":"user","content":"hi"}]}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(string(body)))

	result, err := svc.ForwardAsChatCompletions(context.Background(), c, account, body)
	require.Nil(t, result)
	requireGeminiEmptyFailover(t, err, rec, false)
}

func TestGeminiForward_NonStreamEmptyBodyFailsOver(t *testing.T) {
	gin.SetMode(gin.TestMode)
	httpStub := &geminiSignalHTTPUpstreamStub{
		response: &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{}`)),
		},
	}
	svc := &GeminiMessagesCompatService{httpUpstream: httpStub, cfg: &config.Config{}}
	account := &Account{
		ID:          202,
		Platform:    PlatformGemini,
		Type:        AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "test-key"},
	}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	body := []byte(`{"model":"gemini-2.5-pro","max_tokens":16,"messages":[{"role":"user","content":"hi"}]}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(string(body)))

	result, err := svc.Forward(context.Background(), c, account, body)
	require.Nil(t, result)
	requireGeminiEmptyFailover(t, err, rec, false)
}

func newGeminiSignalAntigravityService() *AntigravityGatewayService {
	return &AntigravityGatewayService{
		settingService: &SettingService{cfg: &config.Config{}},
	}
}

func TestAntigravityHandleGeminiStreamToNonStreaming_EmptyCandidatesFailsOver(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := newGeminiSignalAntigravityService()
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-2.5-pro:generateContent", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader(
			`data: {"candidates":[],"usageMetadata":{"promptTokenCount":3}}` + "\n\n" +
				"data: [DONE]\n\n",
		)),
	}

	result, err := svc.handleGeminiStreamToNonStreaming(c, resp, time.Now())
	require.Nil(t, result)
	requireGeminiEmptyFailover(t, err, rec, false)
}

func TestAntigravityHandleGeminiStreamToNonStreaming_EmptyObjectFailsOver(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := newGeminiSignalAntigravityService()
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-2.5-pro:generateContent", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader("data: {}\n\n")),
	}

	result, err := svc.handleGeminiStreamToNonStreaming(c, resp, time.Now())
	require.Nil(t, result)
	requireGeminiEmptyFailover(t, err, rec, false)
}
