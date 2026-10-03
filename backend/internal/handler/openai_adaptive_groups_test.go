package handler

import (
	"context"
	"errors"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

type adaptiveReleaseContextRepository struct {
	service.UsageBillingReservationRepository
	called     bool
	contextErr error
}

type adaptiveHeartbeatRepository struct {
	service.UsageBillingReservationRepository
	mu              sync.Mutex
	renewCalls      int
	releaseCalls    int
	markFailedCalls int
	markInFlightCmd *service.UsageReservationMarkInFlightCommand
	releaseCmd      *service.UsageReservationReleaseCommand
	transitionOrder []string
	renewStarted    chan struct{}
	allowRenew      chan struct{}
	releaseErr      error
}

func (r *adaptiveHeartbeatRepository) Renew(
	ctx context.Context,
	cmd *service.UsageReservationRenewCommand,
) (*service.UsageReservationResult, error) {
	r.mu.Lock()
	r.renewCalls++
	r.mu.Unlock()
	select {
	case r.renewStarted <- struct{}{}:
	default:
	}
	select {
	case <-r.allowRenew:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return &service.UsageReservationResult{Reservation: &service.UsageBillingReservation{
		ID:           cmd.ReservationID,
		OwnerID:      cmd.OwnerID,
		FencingToken: cmd.FencingToken,
		RowVersion:   cmd.RowVersion + 1,
		Status:       service.UsageReservationStatusAuthorized,
	}}, nil
}

func (r *adaptiveHeartbeatRepository) MarkInFlight(
	_ context.Context,
	cmd *service.UsageReservationMarkInFlightCommand,
) (*service.UsageReservationResult, error) {
	r.mu.Lock()
	r.markInFlightCmd = cmd
	r.mu.Unlock()
	leafGroupID := cmd.LeafGroupID
	attemptNo := cmd.AttemptNo
	return &service.UsageReservationResult{Reservation: &service.UsageBillingReservation{
		ID:                cmd.ReservationID,
		OwnerID:           cmd.OwnerID,
		FencingToken:      cmd.FencingToken,
		RowVersion:        cmd.RowVersion + 1,
		Status:            service.UsageReservationStatusInFlight,
		ActiveLeafGroupID: &leafGroupID,
		ActiveAttemptNo:   &attemptNo,
	}}, nil
}

func (r *adaptiveHeartbeatRepository) MarkAttemptFailed(
	_ context.Context,
	cmd *service.UsageReservationAttemptFailedCommand,
) (*service.UsageReservationResult, error) {
	r.mu.Lock()
	r.markFailedCalls++
	r.transitionOrder = append(r.transitionOrder, "mark_failed")
	r.mu.Unlock()
	return &service.UsageReservationResult{Reservation: &service.UsageBillingReservation{
		ID:           cmd.ReservationID,
		OwnerID:      cmd.OwnerID,
		FencingToken: cmd.FencingToken,
		RowVersion:   cmd.RowVersion + 1,
		Status:       service.UsageReservationStatusAuthorized,
	}}, nil
}

func (r *adaptiveHeartbeatRepository) Release(
	_ context.Context,
	cmd *service.UsageReservationReleaseCommand,
) (*service.UsageReservationResult, error) {
	r.mu.Lock()
	r.releaseCalls++
	r.releaseCmd = cmd
	r.transitionOrder = append(r.transitionOrder, "release")
	r.mu.Unlock()
	if r.releaseErr != nil {
		return nil, r.releaseErr
	}
	return &service.UsageReservationResult{Reservation: &service.UsageBillingReservation{
		ID:           cmd.ReservationID,
		OwnerID:      cmd.OwnerID,
		FencingToken: cmd.FencingToken,
		RowVersion:   cmd.RowVersion + 1,
		Status:       service.UsageReservationStatusReleased,
	}}, nil
}

func adaptiveTestPlan(leafIDs ...int64) *service.AdaptiveRoutePlan {
	candidates := make([]service.AdaptiveRouteCandidate, 0, len(leafIDs))
	for _, id := range leafIDs {
		candidates = append(candidates, service.AdaptiveRouteCandidate{
			LeafGroupID:          id,
			FrozenRateMultiplier: 0.1,
		})
	}
	return service.NewAdaptiveRoutePlan(candidates)
}

func TestAdaptiveBillingHeartbeatSerializesTransitionsAndStopsOnSettlementQueued(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	repository := &adaptiveHeartbeatRepository{
		renewStarted: make(chan struct{}, 1),
		allowRenew:   make(chan struct{}),
	}
	handler := &OpenAIGatewayHandler{
		adaptiveBilling: service.NewAdaptiveBillingCoordinator(repository, nil),
	}
	session := &openAIAdaptiveSession{
		Plan: adaptiveTestPlan(34),
		Billing: &service.AdaptiveBillingContext{
			ReservationID:    "reservation-heartbeat",
			LogicalRequestID: "request-heartbeat",
			OwnerID:          "request-owner",
			FencingToken:     1,
			RowVersion:       1,
		},
	}
	c.Set(ginKeyOpenAIAdaptiveSession, session)
	session.startAdaptiveBillingHeartbeat(handler.adaptiveBilling, 50*time.Millisecond, 2*time.Minute, nil)

	select {
	case <-repository.renewStarted:
	case <-time.After(time.Second):
		t.Fatal("heartbeat did not renew the reservation")
	}

	markDone := make(chan struct{})
	go func() {
		handler.markAdaptiveLeafInFlight(context.Background(), c, 34, 1, nil)
		close(markDone)
	}()
	select {
	case <-markDone:
		t.Fatal("attempt transition raced with the active renewal")
	case <-time.After(20 * time.Millisecond):
	}

	close(repository.allowRenew)
	select {
	case <-markDone:
	case <-time.After(time.Second):
		t.Fatal("attempt transition did not resume after renewal")
	}

	billing, queuedSession := prepareAdaptiveSessionSettlement(c)
	// Settlement works on a deep snapshot, not the shared mutable context.
	require.NotSame(t, session.Billing, billing)
	require.Equal(t, session.Billing, billing)
	require.Same(t, session, queuedSession)
	handler.finishAdaptiveOpenAISession(context.Background(), c, "request_end", nil)

	// Settlement-queued must stop the heartbeat: across multiple tick windows
	// no further Renew may be issued (version conflict window is closed).
	time.Sleep(250 * time.Millisecond)
	repository.mu.Lock()
	renewCallsAfterQueue := repository.renewCalls
	require.GreaterOrEqual(t, renewCallsAfterQueue, 2)
	require.Zero(t, repository.releaseCalls)
	repository.mu.Unlock()

	time.Sleep(100 * time.Millisecond)
	repository.mu.Lock()
	require.Equal(t, renewCallsAfterQueue, repository.renewCalls, "heartbeat must not renew after settlement queued")
	require.NotNil(t, repository.markInFlightCmd)
	require.Equal(t, int64(2), repository.markInFlightCmd.RowVersion)
	repository.mu.Unlock()

	// stopAdaptiveBillingHeartbeat must return immediately (already stopped).
	stopDone := make(chan struct{})
	go func() {
		session.stopAdaptiveBillingHeartbeat()
		close(stopDone)
	}()
	select {
	case <-stopDone:
	case <-time.After(time.Second):
		t.Fatal("heartbeat did not stop after settlement queued")
	}
}

func TestResetAdaptiveOpenAISessionReleasesAndClearsBeforeReplan(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	repository := &adaptiveHeartbeatRepository{}
	handler := &OpenAIGatewayHandler{
		adaptiveBilling: service.NewAdaptiveBillingCoordinator(repository, nil),
	}
	session := &openAIAdaptiveSession{Billing: &service.AdaptiveBillingContext{
		ReservationID:    "reservation-before-replan",
		LogicalRequestID: "request-before-replan",
		OwnerID:          "request-owner",
		FencingToken:     1,
		RowVersion:       1,
	}}
	c.Set(ginKeyOpenAIAdaptiveSession, session)

	err := handler.resetAdaptiveOpenAISession(context.Background(), c, "image_tools_replanned", nil)

	require.NoError(t, err)
	require.Nil(t, getOpenAIAdaptiveSession(c))
	repository.mu.Lock()
	require.Equal(t, 1, repository.releaseCalls)
	repository.mu.Unlock()
}

func TestResetAdaptiveOpenAISessionKeepsCurrentSessionWhenReleaseFails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	releaseErr := errors.New("release failed")
	repository := &adaptiveHeartbeatRepository{releaseErr: releaseErr}
	handler := &OpenAIGatewayHandler{
		adaptiveBilling: service.NewAdaptiveBillingCoordinator(repository, nil),
	}
	session := &openAIAdaptiveSession{Billing: &service.AdaptiveBillingContext{
		ReservationID:    "reservation-failed-replan",
		LogicalRequestID: "request-failed-replan",
		OwnerID:          "request-owner",
		FencingToken:     1,
		RowVersion:       1,
	}}
	c.Set(ginKeyOpenAIAdaptiveSession, session)

	err := handler.resetAdaptiveOpenAISession(context.Background(), c, "image_tools_replanned", nil)

	require.ErrorIs(t, err, releaseErr)
	require.Same(t, session, getOpenAIAdaptiveSession(c))
}

func TestFinishAdaptiveOpenAISessionMarksInFlightAttemptFailedBeforeRelease(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	repository := &adaptiveHeartbeatRepository{}
	handler := &OpenAIGatewayHandler{
		adaptiveBilling: service.NewAdaptiveBillingCoordinator(repository, nil),
	}
	session := &openAIAdaptiveSession{Billing: &service.AdaptiveBillingContext{
		ReservationID:     "reservation-final-attempt-failed",
		LogicalRequestID:  "request-final-attempt-failed",
		OwnerID:           "request-owner",
		FencingToken:      1,
		RowVersion:        1,
		ParentGroupID:     108,
		LeafGroupID:       85,
		AttemptNo:         1,
		PricingGeneration: 1,
		ConfigGeneration:  1,
		PricingSnapshotID: "pricing-final-attempt-failed",
		ManagementFeeBPS:  service.DefaultAdaptiveManagementFeeBPS,
		HeldBaseCost:      decimal.RequireFromString("5.0000000000"),
		HeldManagementFee: decimal.RequireFromString("0.7500000000"),
		HeldTotal:         decimal.RequireFromString("5.7500000000"),
		FundingSource:     service.UsageReservationFundingBalance,
	}}
	c.Set(ginKeyOpenAIAdaptiveSession, session)

	err := handler.finishAdaptiveOpenAISession(context.Background(), c, "request_end", nil)

	require.NoError(t, err)
	repository.mu.Lock()
	require.Equal(t, 1, repository.markFailedCalls)
	require.Equal(t, 1, repository.releaseCalls)
	require.Equal(t, []string{"mark_failed", "release"}, repository.transitionOrder)
	require.NotNil(t, repository.releaseCmd)
	require.Equal(t, int64(2), repository.releaseCmd.RowVersion)
	require.Len(t, repository.releaseCmd.EvidenceHash, 64)
	require.Equal(t, session.Billing.LastFailedEvidenceHash, repository.releaseCmd.EvidenceHash)
	repository.mu.Unlock()
}

func TestAdaptiveAntiStallWriterInstallsOnceAndCleansUpAtRequestEnd(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	originalWriter := c.Writer
	settings := service.AntiStallProSettings{Enabled: true, BufferTokens: 8}

	require.True(t, installAdaptiveAntiStallWriterOnce(c, service.NewAntiStallSession(settings)))
	installedWriter := c.Writer
	require.NotEqual(t, originalWriter, installedWriter)
	require.False(t, installAdaptiveAntiStallWriterOnce(c, service.NewAntiStallSession(settings)))
	require.Equal(t, installedWriter, c.Writer)

	handler := &OpenAIGatewayHandler{}
	require.NoError(t, handler.finishAdaptiveOpenAIRequest(context.Background(), c, "request_end", nil))
	require.Equal(t, originalWriter, c.Writer)
}

func (r *adaptiveReleaseContextRepository) Release(
	ctx context.Context,
	_ *service.UsageReservationReleaseCommand,
) (*service.UsageReservationResult, error) {
	r.called = true
	r.contextErr = ctx.Err()
	return &service.UsageReservationResult{}, ctx.Err()
}

func TestFinishAdaptiveOpenAISessionReleasesWithDetachedContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	billing := &service.AdaptiveBillingContext{
		ReservationID:    "reservation-detached-release",
		LogicalRequestID: "request-detached-release",
		OwnerID:          "request-owner",
		FencingToken:     1,
		RowVersion:       1,
	}
	c.Set(ginKeyOpenAIAdaptiveSession, &openAIAdaptiveSession{Billing: billing})

	repository := &adaptiveReleaseContextRepository{}
	handler := &OpenAIGatewayHandler{
		adaptiveBilling: service.NewAdaptiveBillingCoordinator(repository, nil),
	}
	requestCtx, cancel := context.WithCancel(context.Background())
	cancel()

	err := handler.finishAdaptiveOpenAISession(requestCtx, c, "client_disconnected", nil)

	require.NoError(t, err)
	require.True(t, repository.called)
	require.NoError(t, repository.contextErr)
}

type adaptiveAttemptTransitionRepository struct {
	service.UsageBillingReservationRepository
	failedCmd   *service.UsageReservationAttemptFailedCommand
	inFlightCmd *service.UsageReservationMarkInFlightCommand
	captureCmd  *service.UsageReservationCaptureCommand
}

func (r *adaptiveAttemptTransitionRepository) MarkAttemptFailed(
	_ context.Context,
	cmd *service.UsageReservationAttemptFailedCommand,
) (*service.UsageReservationResult, error) {
	r.failedCmd = cmd
	if cmd.FailureClass != "precommit_upstream" {
		return nil, service.ErrUsageReservationInvalid
	}
	return &service.UsageReservationResult{Reservation: &service.UsageBillingReservation{
		ID:           cmd.ReservationID,
		OwnerID:      cmd.OwnerID,
		FencingToken: cmd.FencingToken,
		RowVersion:   cmd.RowVersion + 1,
		Status:       service.UsageReservationStatusAuthorized,
	}}, nil
}

func (r *adaptiveAttemptTransitionRepository) MarkInFlight(
	_ context.Context,
	cmd *service.UsageReservationMarkInFlightCommand,
) (*service.UsageReservationResult, error) {
	r.inFlightCmd = cmd
	if cmd.RowVersion != 2 {
		return nil, service.ErrUsageReservationAttemptConflict
	}
	leafGroupID := cmd.LeafGroupID
	attemptNo := cmd.AttemptNo
	return &service.UsageReservationResult{Reservation: &service.UsageBillingReservation{
		ID:                cmd.ReservationID,
		OwnerID:           cmd.OwnerID,
		FencingToken:      cmd.FencingToken,
		RowVersion:        cmd.RowVersion + 1,
		Status:            service.UsageReservationStatusInFlight,
		ActiveLeafGroupID: &leafGroupID,
		ActiveAttemptNo:   &attemptNo,
	}}, nil
}

func (r *adaptiveAttemptTransitionRepository) Capture(
	_ context.Context,
	cmd *service.UsageReservationCaptureCommand,
) (*service.UsageReservationResult, error) {
	r.captureCmd = cmd
	return &service.UsageReservationResult{Reservation: &service.UsageBillingReservation{
		ID:           cmd.ReservationID,
		OwnerID:      cmd.OwnerID,
		FencingToken: cmd.FencingToken,
		RowVersion:   cmd.RowVersion + 1,
		Status:       service.UsageReservationStatusCaptured,
	}}, nil
}

type adaptiveTransitionUsageLogRepository struct{}

func (r *adaptiveTransitionUsageLogRepository) Create(_ context.Context, log *service.UsageLog) (bool, error) {
	log.ID = 901
	log.CreatedAt = time.Date(2026, 7, 26, 7, 0, 0, 0, time.UTC)
	return true, nil
}

func (r *adaptiveTransitionUsageLogRepository) GetByID(context.Context, int64) (*service.UsageLog, error) {
	return nil, service.ErrAdaptiveUsageEvidenceConflict
}

func TestAdaptiveFailureTransitionAttributesUsageToFallbackLeaf(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	billing := &service.AdaptiveBillingContext{
		ReservationID:     "2fa97db0-6c9f-48d0-a1fb-a526521dd3a0",
		LogicalRequestID:  "adaptive-fallback-request",
		OwnerID:           "gateway-1",
		FencingToken:      1,
		RowVersion:        1,
		ParentGroupID:     108,
		LeafGroupID:       85,
		AttemptNo:         1,
		PricingGeneration: 1,
		ConfigGeneration:  1,
		PricingSnapshotID: "pricing-v1",
		ManagementFeeBPS:  service.DefaultAdaptiveManagementFeeBPS,
		HeldBaseCost:      decimal.RequireFromString("5.0000000000"),
		HeldManagementFee: decimal.RequireFromString("0.7500000000"),
		HeldTotal:         decimal.RequireFromString("5.7500000000"),
		FundingSource:     service.UsageReservationFundingBalance,
	}
	c.Set(ginKeyOpenAIAdaptiveSession, &openAIAdaptiveSession{
		Billing:       billing,
		CurrentLeafID: 85,
		Plan:          adaptiveTestPlan(85, 34),
	})

	reservations := &adaptiveAttemptTransitionRepository{}
	handler := &OpenAIGatewayHandler{
		adaptiveBilling: service.NewAdaptiveBillingCoordinator(reservations, &adaptiveTransitionUsageLogRepository{}),
	}

	handler.markAdaptiveLeafFailed(context.Background(), c, "upstream_507", nil)
	handler.markAdaptiveLeafInFlight(context.Background(), c, 34, 2, nil)

	usageLog := &service.UsageLog{
		UserID:      104,
		APIKeyID:    200,
		AccountID:   328,
		RequestID:   "fallback-usage",
		Model:       "gpt-5.6-sol",
		BillingType: service.BillingTypeBalance,
	}
	_, _, err := handler.adaptiveBilling.Capture(
		context.Background(), billing, usageLog, decimal.RequireFromString("0.1000000000"),
	)
	require.NoError(t, err)
	require.NotNil(t, reservations.failedCmd)
	require.Equal(t, "precommit_upstream", reservations.failedCmd.FailureClass)
	require.NotNil(t, reservations.inFlightCmd)
	require.Equal(t, int64(34), reservations.inFlightCmd.LeafGroupID)
	require.Equal(t, 2, reservations.inFlightCmd.AttemptNo)
	require.NotNil(t, reservations.captureCmd)
	require.Equal(t, int64(34), reservations.captureCmd.WinningLeafGroupID)
	require.Equal(t, 2, reservations.captureCmd.AttemptNo)
	require.Equal(t, int64(34), *usageLog.RoutedGroupID)
	require.Equal(t, 2, *usageLog.AdaptiveAttemptNo)
}

func TestMarkAdaptiveLeafInFlightPassesThroughWithoutAdaptiveSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	handler := &OpenAIGatewayHandler{}

	require.True(t, handler.markAdaptiveLeafInFlight(context.Background(), c, 17, 1, nil),
		"non-adaptive request without session must not fail closed")
}

func TestMarkAdaptiveLeafInFlightFailsClosedWhenFenceCannotPersist(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	repository := &adaptiveHeartbeatRepository{}
	handler := &OpenAIGatewayHandler{
		adaptiveBilling: service.NewAdaptiveBillingCoordinator(repository, nil),
	}
	session := &openAIAdaptiveSession{Billing: &service.AdaptiveBillingContext{
		ReservationID:    "reservation-fence-fail",
		LogicalRequestID: "request-fence-fail",
		OwnerID:          "request-owner",
		FencingToken:     1,
		RowVersion:       1,
	}}
	c.Set(ginKeyOpenAIAdaptiveSession, session)

	require.False(t, handler.markAdaptiveLeafInFlight(context.Background(), c, 0, 1, nil),
		"invalid leaf group id must fail closed")
	require.False(t, handler.markAdaptiveLeafInFlight(context.Background(), c, 17, 0, nil),
		"invalid attempt number must fail closed")
}

func TestNormalizeAdaptivePrecommitFailureClass(t *testing.T) {
	tests := map[string]string{
		"upstream_507":                    "precommit_upstream",
		"non_retryable_upstream_502":      "precommit_upstream",
		"account_select_failed":           "precommit_policy",
		"all_fallback_attempts_exhausted": "precommit_policy",
		"client_disconnected":             "precommit_cancelled",
		"network_timeout":                 "precommit_transport",
		"precommit_upstream":              "precommit_upstream",
	}
	for input, expected := range tests {
		t.Run(input, func(t *testing.T) {
			require.Equal(t, expected, normalizeAdaptivePrecommitFailureClass(input))
		})
	}
}
