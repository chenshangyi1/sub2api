//go:build unit

package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type cooldownAccountRepoStub struct {
	AccountRepository
	tempUnschedCalls int
}

func (r *cooldownAccountRepoStub) SetTempUnschedulable(_ context.Context, _ int64, _ time.Time, _ string) error {
	r.tempUnschedCalls++
	return nil
}

type selectionFallbackGroupRepoStub struct {
	GroupRepository
	group *Group
	err   error
	calls atomic.Int64
}

func (s *selectionFallbackGroupRepoStub) GetByIDLite(_ context.Context, _ int64) (*Group, error) {
	s.calls.Add(1)
	if s.err != nil {
		return nil, s.err
	}
	return s.group, nil
}

func TestGemini404IsFailoverWorthy(t *testing.T) {
	svc := &GeminiMessagesCompatService{}
	require.True(t, svc.shouldFailoverGeminiUpstreamError(404))
	require.True(t, svc.shouldFailoverGeminiUpstreamError(429))
	require.True(t, svc.shouldFailoverGeminiUpstreamError(500))
	require.False(t, svc.shouldFailoverGeminiUpstreamError(400))
	require.False(t, svc.shouldFailoverGeminiUpstreamError(422))
}

func TestGeminiSameAccountRetrySkipsPersistentUpstream5xx(t *testing.T) {
	svc := &GeminiMessagesCompatService{}
	apiKey := &Account{ID: 1, Type: AccountTypeAPIKey, Platform: PlatformGemini}
	require.True(t, svc.shouldRetryGeminiUpstreamError(apiKey, 429))
	require.False(t, svc.shouldRetryGeminiUpstreamError(apiKey, 500))
	require.False(t, svc.shouldRetryGeminiUpstreamError(apiKey, 502))
	require.False(t, svc.shouldRetryGeminiUpstreamError(apiKey, 503))
	require.False(t, svc.shouldRetryGeminiUpstreamError(apiKey, 504))
	require.False(t, svc.shouldRetryGeminiUpstreamError(apiKey, 529))
	require.False(t, svc.shouldRetryGeminiUpstreamError(apiKey, 403))
	require.Equal(t, 2, geminiMaxRetries)
	require.Equal(t, 200*time.Millisecond, geminiRetryBaseDelay)
	require.Equal(t, time.Second, geminiRetryMaxDelay)
}

func TestPool404FailoverHasNoSameAccountRetry(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &GeminiMessagesCompatService{}
	account := &Account{
		ID:          300,
		Type:        AccountTypeAPIKey,
		Platform:    PlatformGemini,
		Credentials: map[string]any{"pool_mode": true},
	}
	writer := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(writer)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	body := []byte(`{"error":{"message":"not found"}}`)

	failoverErr := svc.skippedErrorPolicyFailoverError(c, account, http.StatusNotFound, body, "req-1")
	require.NotNil(t, failoverErr)
	require.Equal(t, http.StatusNotFound, failoverErr.StatusCode)
	require.False(t, failoverErr.RetryableOnSameAccount)
	require.True(t, failoverErr.ShouldRetryNextAccount())
}

func TestConsecutiveUnusableFailuresCoolFiveMinutes(t *testing.T) {
	resetConsecutiveUnusableFailuresForTest()
	t.Cleanup(resetConsecutiveUnusableFailuresForTest)

	repo := &cooldownAccountRepoStub{}
	svc := &GatewayService{accountRepo: repo}

	svc.TempUnscheduleRetryableError(context.Background(), 9, &UpstreamFailoverError{StatusCode: http.StatusNotFound})
	require.Zero(t, repo.tempUnschedCalls, "first unusable failure should not cool yet")

	svc.TempUnscheduleRetryableError(context.Background(), 9, &UpstreamFailoverError{StatusCode: http.StatusNotFound})
	require.Equal(t, 1, repo.tempUnschedCalls, "second consecutive unusable failure cools for 5 minutes")
}

func TestEmptyAndUnusableCooldownsAreFiveMinutes(t *testing.T) {
	require.Equal(t, 5*time.Minute, emptyResponseCooldown)
	require.Equal(t, 5*time.Minute, googleConfigErrorCooldown)
	require.Equal(t, 5*time.Minute, consecutiveUnusableFailureCooldown)
	require.Equal(t, 2, consecutiveUnusableFailureThreshold)
}

func TestRequestScopedTransientStillSkipsUnschedule(t *testing.T) {
	repo := &cooldownAccountRepoStub{}
	svc := &GatewayService{accountRepo: repo}
	svc.TempUnscheduleRetryableError(context.Background(), 1, &UpstreamFailoverError{
		StatusCode:             http.StatusBadGateway,
		RetryableOnSameAccount: true,
		RequestScopedTransient: true,
	})
	require.Zero(t, repo.tempUnschedCalls)
}

func TestSelectionFallbackReadsColdGroupFromRepository(t *testing.T) {
	groupID := int64(194)
	repo := &selectionFallbackGroupRepoStub{group: &Group{ID: groupID, Platform: PlatformGemini, Status: StatusActive}}
	cfg := &config.Config{}
	cfg.Gateway.Scheduling.RequestFreshnessEnabled = false
	snapshot := NewSchedulerSnapshotService(nil, nil, nil, repo, cfg)
	svc := &GatewayService{groupRepo: repo, schedulerSnapshot: snapshot}

	got, err := svc.resolveGroupByID(withSchedulerSelectionFallback(withSchedulerSnapshotOnly(context.Background())), groupID)
	require.NoError(t, err)
	require.Equal(t, groupID, got.ID)
	require.Equal(t, int64(1), repo.calls.Load())
}

func TestCatalogSnapshotColdStillSkipsRepository(t *testing.T) {
	groupID := int64(97)
	repo := &selectionFallbackGroupRepoStub{
		group: &Group{ID: groupID, Platform: PlatformOpenAI, Status: StatusActive},
		err:   errors.New("should not be required for the request path"),
	}
	cfg := &config.Config{}
	cfg.Gateway.Scheduling.RequestFreshnessEnabled = false
	snapshot := NewSchedulerSnapshotService(nil, nil, nil, repo, cfg)
	svc := &GatewayService{groupRepo: repo, schedulerSnapshot: snapshot}

	got := svc.schedulingGroupForRequest(withSchedulerSnapshotOnly(context.Background()), &groupID)
	require.Nil(t, got)
}
