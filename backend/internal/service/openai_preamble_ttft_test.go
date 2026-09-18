package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOpenAINativePreambleFlushesBeforeVisibleOutput(t *testing.T) {
	created := "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_fast\"}}\n\n"
	inProgress := "data: {\"type\":\"response.in_progress\",\"response\":{\"id\":\"resp_fast\"}}\n\n"
	delta := "data: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}\n\n"
	completed := "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_fast\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\n"

	releaseVisible := make(chan struct{})
	waitingPreamble := make(chan struct{})
	body := &stagedOpenAISSEReadCloser{
		segments: [][]byte{
			[]byte(created + inProgress),
			[]byte(delta + completed),
		},
		gates:   []<-chan struct{}{nil, releaseVisible},
		waiting: []chan struct{}{waitingPreamble, nil},
	}
	recorder := newOpenAIResponseFlushRecorder()
	resultCh, errCh := runOpenAIResponseFlushTestAsync(recorder, body, config.GatewayConfig{MaxLineSize: defaultMaxLineSize})

	waitOpenAIResponseFlushSignal(t, waitingPreamble)
	waitOpenAIResponseFlushCount(t, recorder, 1)
	got, _ := recorder.snapshot()
	require.Contains(t, got, `"type":"response.created"`)
	require.Contains(t, got, "resp_fast")
	require.NotContains(t, got, `"delta":"hello"`)

	close(releaseVisible)
	require.NoError(t, <-errCh)
	result := <-resultCh
	require.NotNil(t, result)
	require.NotNil(t, result.firstTokenMs)
	finalBody, _ := recorder.snapshot()
	require.Contains(t, finalBody, `"delta":"hello"`)
	require.Contains(t, finalBody, `"type":"response.completed"`)
}

func TestOpenAINativePreambleFailoverRewritesRetryIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}}}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	firstResp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"X-Request-Id": []string{"request-first"}},
		Body: io.NopCloser(strings.NewReader(strings.Join([]string{
			`data: {"type":"response.created","response":{"id":"resp_first"}}`,
			"",
			`data: {"type":"response.failed","response":{"id":"resp_first","error":{"code":"server_error","message":"upstream processing failed"}}}`,
			"",
		}, "\n"))),
	}
	_, firstErr := svc.handleStreamingResponse(c.Request.Context(), firstResp, c, &Account{ID: 1, Platform: PlatformOpenAI, Name: "first"}, time.Now(), "model", "model")
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, firstErr, &failoverErr)
	require.True(t, failoverErr.SafeToFailoverAfterWrite)
	require.Contains(t, rec.Body.String(), `"type":"response.created"`)
	require.Contains(t, rec.Body.String(), "resp_first")
	require.NotContains(t, rec.Body.String(), "response.failed")

	secondResp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"X-Request-Id": []string{"request-second"}},
		Body: io.NopCloser(strings.NewReader(strings.Join([]string{
			`data: {"type":"response.created","response":{"id":"resp_second"}}`,
			"",
			`data: {"type":"response.in_progress","response":{"id":"resp_second"}}`,
			"",
			`data: {"type":"response.output_text.delta","delta":"hello"}`,
			"",
			`data: {"type":"response.completed","response":{"id":"resp_second","usage":{"input_tokens":1,"output_tokens":1}}}`,
			"",
		}, "\n"))),
	}
	result, secondErr := svc.handleStreamingResponse(c.Request.Context(), secondResp, c, &Account{ID: 2, Platform: PlatformOpenAI, Name: "second"}, time.Now(), "model", "model")
	require.NoError(t, secondErr)
	require.NotNil(t, result)
	body := rec.Body.String()
	require.Equal(t, 1, strings.Count(body, `"type":"response.created"`))
	require.Contains(t, body, "resp_first")
	require.NotContains(t, body, "resp_second")
	require.Contains(t, body, `"delta":"hello"`)
	require.Contains(t, body, `"type":"response.completed"`)
}

func TestOpenAIPassthroughPreambleFlushesBeforeVisibleOutput(t *testing.T) {
	preamble := "event: response.created\n" +
		`data: {"type":"response.created","response":{"id":"resp_pending"}}` + "\n\n"
	firstOutput := `data: {"type":"response.output_text.delta","delta":"ready"}` + "\n\n"
	terminalEvent := `data: {"type":"response.completed","response":{"id":"resp_pending","usage":{"input_tokens":4,"output_tokens":1,"total_tokens":5}}}` + "\n\n"

	releaseVisible := make(chan struct{})
	waitingPreamble := make(chan struct{})
	body := &stagedOpenAISSEReadCloser{
		segments: [][]byte{
			[]byte(preamble),
			[]byte(firstOutput + terminalEvent),
		},
		gates:   []<-chan struct{}{nil, releaseVisible},
		waiting: []chan struct{}{waitingPreamble, nil},
	}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	writer := &passthroughFlushTestWriter{ResponseWriter: c.Writer, recorder: recorder, failAfterWrites: -1}
	c.Writer = writer
	svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}}}
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       body,
	}

	errCh := make(chan error, 1)
	go func() {
		_, err := svc.handleStreamingResponsePassthrough(context.Background(), resp, c, &Account{ID: 1, Platform: PlatformOpenAI, Name: "flush-test"}, time.Now(), "", "")
		errCh <- err
	}()

	waitOpenAIResponseFlushSignal(t, waitingPreamble)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(recorder.Body.String(), `"type":"response.created"`) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	require.Contains(t, recorder.Body.String(), `"type":"response.created"`)
	require.NotContains(t, recorder.Body.String(), `"delta":"ready"`)

	close(releaseVisible)
	require.NoError(t, <-errCh)
	require.Contains(t, recorder.Body.String(), `"delta":"ready"`)
}

func TestOpenAIPassthroughPreambleFailoverRewritesRetryIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}}}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	account := &Account{ID: 1, Platform: PlatformOpenAI, Name: "first"}

	firstResp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader(strings.Join([]string{
			"event: response.created",
			`data: {"type":"response.created","response":{"id":"resp_first"}}`,
			"",
			"event: response.failed",
			`data: {"type":"response.failed","error":{"code":"server_error","message":"upstream processing failed"}}`,
			"",
		}, "\n"))),
	}
	_, firstErr := svc.handleStreamingResponsePassthrough(c.Request.Context(), firstResp, c, account, time.Now(), "", "")
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, firstErr, &failoverErr)
	require.True(t, failoverErr.SafeToFailoverAfterWrite)
	require.Contains(t, rec.Body.String(), `"type":"response.created"`)
	require.Contains(t, rec.Body.String(), "resp_first")
	require.NotContains(t, rec.Body.String(), "response.failed")

	secondResp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader(strings.Join([]string{
			"event: response.created",
			`data: {"type":"response.created","response":{"id":"resp_second"}}`,
			"",
			`data: {"type":"response.output_text.delta","delta":"hello"}`,
			"",
			`data: {"type":"response.completed","response":{"id":"resp_second","usage":{"input_tokens":1,"output_tokens":1}}}`,
			"",
		}, "\n"))),
	}
	result, secondErr := svc.handleStreamingResponsePassthrough(c.Request.Context(), secondResp, c, &Account{ID: 2, Platform: PlatformOpenAI, Name: "second"}, time.Now(), "", "")
	require.NoError(t, secondErr)
	require.NotNil(t, result)
	body := rec.Body.String()
	require.Equal(t, 1, strings.Count(body, `"type":"response.created"`))
	require.Contains(t, body, "resp_first")
	require.NotContains(t, body, "resp_second")
	require.Contains(t, body, `"delta":"hello"`)
}
