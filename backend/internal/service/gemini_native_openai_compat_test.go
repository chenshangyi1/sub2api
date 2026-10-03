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

type geminiOpenAICompatHTTPStub struct {
	status      int
	body        string
	contentType string
	urls        []string
	lastURL     string
	lastAuth    string
	lastAccept  string
}

func (s *geminiOpenAICompatHTTPStub) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	if req != nil {
		s.urls = append(s.urls, req.URL.String())
		s.lastURL = req.URL.String()
		s.lastAuth = req.Header.Get("Authorization")
		s.lastAccept = req.Header.Get("Accept")
	}
	ct := s.contentType
	if ct == "" {
		ct = "application/json"
	}
	return &http.Response{
		StatusCode: s.status,
		Header:     http.Header{"Content-Type": []string{ct}, "X-Request-Id": []string{"compat-req-1"}},
		Body:       io.NopCloser(strings.NewReader(s.body)),
	}, nil
}

func (s *geminiOpenAICompatHTTPStub) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return s.Do(req, proxyURL, accountID, accountConcurrency)
}

func geminiOpenAICompatAccount() *Account {
	return &Account{
		ID:       2593,
		Name:     "mdkj-s2a-gemini",
		Platform: PlatformGemini,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":      "sk-test",
			"base_url":     "https://vip.mdkj.lol/v1",
			"api_protocol": APIProtocolChatCompletions,
		},
	}
}

func geminiCustomEmptyProtocolAccount() *Account {
	return &Account{
		ID:       2498,
		Name:     "custom-gemini-relay",
		Platform: PlatformGemini,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "sk-test",
			"base_url": "https://relay.example.com",
		},
	}
}

type geminiNativeThenOpenAIHTTPStub struct {
	nativeStatus int
	nativeBody   string
	openaiStatus int
	openaiBody   string
	openaiCT     string
	urls         []string
	auths        []string
}

func (s *geminiNativeThenOpenAIHTTPStub) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	url := ""
	auth := ""
	if req != nil && req.URL != nil {
		url = req.URL.String()
		auth = req.Header.Get("Authorization")
	}
	s.urls = append(s.urls, url)
	s.auths = append(s.auths, auth)
	if strings.Contains(url, "/v1beta/") {
		return &http.Response{
			StatusCode: s.nativeStatus,
			Header:     http.Header{"Content-Type": []string{"application/json"}, "X-Request-Id": []string{"native-req-1"}},
			Body:       io.NopCloser(strings.NewReader(s.nativeBody)),
		}, nil
	}
	ct := s.openaiCT
	if ct == "" {
		ct = "application/json"
	}
	return &http.Response{
		StatusCode: s.openaiStatus,
		Header:     http.Header{"Content-Type": []string{ct}, "X-Request-Id": []string{"compat-req-1"}},
		Body:       io.NopCloser(strings.NewReader(s.openaiBody)),
	}, nil
}

func (s *geminiNativeThenOpenAIHTTPStub) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return s.Do(req, proxyURL, accountID, accountConcurrency)
}

func geminiOpenAICompatContext(t *testing.T, path string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader("{}"))
	return c, rec
}

func TestForwardNativeOpenAICompatUsesChatCompletionsAndFailsOverOn404(t *testing.T) {
	stub := &geminiOpenAICompatHTTPStub{
		status: http.StatusNotFound,
		body:   `<html><title>404 Not Found</title></html>`,
	}
	svc := &GeminiMessagesCompatService{httpUpstream: stub, cfg: &config.Config{}}
	c, rec := geminiOpenAICompatContext(t, "/v1beta/models/gemini-3.8-flash:generateContent")

	result, err := svc.ForwardNative(context.Background(), c, geminiOpenAICompatAccount(),
		"gemini-3.8-flash", "generateContent", false, geminiSignalTestRequest())

	require.Nil(t, result)
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Equal(t, http.StatusNotFound, failoverErr.StatusCode)
	require.False(t, failoverErr.RetryableOnSameAccount)
	require.Empty(t, rec.Body.String(), "404 must failover instead of writing Google HTML to the client")
	require.Contains(t, stub.lastURL, "/chat/completions")
	require.NotContains(t, stub.lastURL, "/v1beta/models/")
	require.Equal(t, "Bearer sk-test", stub.lastAuth)
}

func TestForwardNativeOpenAICompatEmptyChatBodyFailsOver(t *testing.T) {
	stub := &geminiOpenAICompatHTTPStub{
		status: http.StatusOK,
		body:   `{"choices":[]}`,
	}
	svc := &GeminiMessagesCompatService{httpUpstream: stub, cfg: &config.Config{}}
	c, rec := geminiOpenAICompatContext(t, "/v1beta/models/gemini-3.8-flash:generateContent")

	result, err := svc.ForwardNative(context.Background(), c, geminiOpenAICompatAccount(),
		"gemini-3.8-flash", "generateContent", false, geminiSignalTestRequest())

	require.Nil(t, result)
	requireGeminiEmptyFailover(t, err, rec, false)
}

func TestForwardNativeOpenAICompatStreamEmptyFailsOverWithoutWrite(t *testing.T) {
	stub := &geminiOpenAICompatHTTPStub{
		status:      http.StatusOK,
		contentType: "text/event-stream",
		body:        "data: {\"choices\":[{\"delta\":{\"content\":\"\"}}]}\n\ndata: [DONE]\n\n",
	}
	svc := &GeminiMessagesCompatService{httpUpstream: stub, cfg: &config.Config{}}
	c, rec := geminiOpenAICompatContext(t, "/v1beta/models/gemini-3.8-flash:streamGenerateContent")

	result, err := svc.ForwardNative(context.Background(), c, geminiOpenAICompatAccount(),
		"gemini-3.8-flash", "streamGenerateContent", true, geminiSignalTestRequest())

	require.Nil(t, result)
	requireGeminiEmptyFailover(t, err, rec, true)
	require.Empty(t, rec.Body.String(), "empty OpenAI stream must be collected before any client write")
	require.Equal(t, "text/event-stream", stub.lastAccept)
}

func TestForwardClaudeViaOpenAICompatWritesClaudeSSENotGeminiJSON(t *testing.T) {
	stub := &geminiOpenAICompatHTTPStub{
		status:      http.StatusOK,
		contentType: "text/event-stream",
		body: strings.Join([]string{
			`data: {"choices":[{"delta":{"content":"hello from relay"}}],"usage":{"prompt_tokens":3,"completion_tokens":4}}`,
			`data: [DONE]`,
			"",
		}, "\n"),
	}
	svc := &GeminiMessagesCompatService{httpUpstream: stub, cfg: &config.Config{}}
	c, rec := geminiOpenAICompatContext(t, "/v1/messages")
	body := []byte(`{"model":"gemini-3.8-flash","stream":true,"messages":[{"role":"user","content":"hi"}]}`)

	result, err := svc.Forward(context.Background(), c, geminiOpenAICompatAccount(), body)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Stream)
	require.Equal(t, 3, result.Usage.InputTokens)
	require.Equal(t, 4, result.Usage.OutputTokens)
	require.Contains(t, stub.lastURL, "/chat/completions")
	out := rec.Body.String()
	require.Contains(t, out, "event: message_start")
	require.Contains(t, out, "hello from relay")
	require.NotContains(t, out, `"candidates"`)
}

func TestForwardChatCompletionsViaOpenAICompatNonStream(t *testing.T) {
	stub := &geminiOpenAICompatHTTPStub{
		status: http.StatusOK,
		body:   `{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`,
	}
	svc := &GeminiMessagesCompatService{httpUpstream: stub, cfg: &config.Config{}}
	c, rec := geminiOpenAICompatContext(t, "/v1/chat/completions")
	claudeBody := []byte(`{"model":"gemini-3.8-flash","stream":false,"messages":[{"role":"user","content":"hi"}]}`)

	result, err := svc.forwardClaudeBodyAsChatCompletions(
		context.Background(),
		c,
		geminiOpenAICompatAccount(),
		claudeBody,
		"gemini-3.8-flash",
		false,
		false,
		time.Now(),
		nil,
		geminiCompatFormatChat,
	)
	require.NoError(t, err)
	require.NotNil(t, result)
	var payload map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &payload))
	require.Equal(t, "chat.completion", payload["object"])
	choices, _ := payload["choices"].([]any)
	require.NotEmpty(t, choices)
}

func TestForwardNativePrefersV1BetaThenFallsBackToOpenAIOnPath404(t *testing.T) {
	stub := &geminiNativeThenOpenAIHTTPStub{
		nativeStatus: http.StatusNotFound,
		nativeBody:   `<html><title>404 Not Found</title></html>`,
		openaiStatus: http.StatusOK,
		openaiBody:   `{"choices":[{"message":{"content":"from openai"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":3}}`,
	}
	svc := &GeminiMessagesCompatService{httpUpstream: stub, cfg: &config.Config{}}
	c, rec := geminiOpenAICompatContext(t, "/v1beta/models/gemini-3.8-flash:generateContent")

	result, err := svc.ForwardNative(context.Background(), c, geminiCustomEmptyProtocolAccount(),
		"gemini-3.8-flash", "generateContent", false, geminiSignalTestRequest())

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 2, result.Usage.InputTokens)
	require.Equal(t, 3, result.Usage.OutputTokens)
	require.Len(t, stub.urls, 2)
	require.Contains(t, stub.urls[0], "/v1beta/models/")
	require.Contains(t, stub.urls[1], "/chat/completions")
	require.Equal(t, "Bearer sk-test", stub.auths[1])
	require.Contains(t, rec.Body.String(), "from openai")
}

func TestForwardNativeKeepsNativeWhenCustomRelaySupportsV1Beta(t *testing.T) {
	stub := &geminiNativeThenOpenAIHTTPStub{
		nativeStatus: http.StatusOK,
		nativeBody:   `{"candidates":[{"content":{"role":"model","parts":[{"text":"native ok"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":2}}`,
		openaiStatus: http.StatusOK,
		openaiBody:   `{"choices":[{"message":{"content":"should not be used"}}]}`,
	}
	svc := &GeminiMessagesCompatService{httpUpstream: stub, cfg: &config.Config{}}
	c, rec := geminiOpenAICompatContext(t, "/v1beta/models/gemini-3.8-flash:generateContent")

	result, err := svc.ForwardNative(context.Background(), c, geminiCustomEmptyProtocolAccount(),
		"gemini-3.8-flash", "generateContent", false, geminiSignalTestRequest())

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, stub.urls, 1)
	require.Contains(t, stub.urls[0], "/v1beta/models/")
	require.Contains(t, rec.Body.String(), "native ok")
	require.NotContains(t, rec.Body.String(), "should not be used")
}

func TestForwardNativeGoogleJSON404DoesNotFallbackToOpenAI(t *testing.T) {
	stub := &geminiNativeThenOpenAIHTTPStub{
		nativeStatus: http.StatusNotFound,
		nativeBody:   `{"error":{"code":404,"message":"models/gemini-3.8-flash is not found","status":"NOT_FOUND"}}`,
		openaiStatus: http.StatusOK,
		openaiBody:   `{"choices":[{"message":{"content":"should not be used"}}]}`,
	}
	svc := &GeminiMessagesCompatService{httpUpstream: stub, cfg: &config.Config{}}
	c, rec := geminiOpenAICompatContext(t, "/v1beta/models/gemini-3.8-flash:generateContent")

	result, err := svc.ForwardNative(context.Background(), c, geminiCustomEmptyProtocolAccount(),
		"gemini-3.8-flash", "generateContent", false, geminiSignalTestRequest())

	require.Nil(t, result)
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Equal(t, http.StatusNotFound, failoverErr.StatusCode)
	require.Len(t, stub.urls, 1)
	require.Contains(t, stub.urls[0], "/v1beta/models/")
	require.Empty(t, rec.Body.String())
}

func TestIsGeminiNativePathMismatch(t *testing.T) {
	t.Parallel()
	require.True(t, isGeminiNativePathMismatch(http.StatusNotFound, []byte(`<html>404</html>`)))
	require.True(t, isGeminiNativePathMismatch(http.StatusNotFound, nil))
	require.True(t, isGeminiNativePathMismatch(http.StatusMethodNotAllowed, []byte(`{"message":"not allowed"}`)))
	require.False(t, isGeminiNativePathMismatch(http.StatusNotFound, []byte(`{"error":{"code":404,"status":"NOT_FOUND"}}`)))
	require.False(t, isGeminiNativePathMismatch(http.StatusInternalServerError, []byte(`<html>500</html>`)))
}
