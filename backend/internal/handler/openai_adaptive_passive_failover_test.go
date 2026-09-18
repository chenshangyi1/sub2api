package handler

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAdaptivePassiveFailoverSwitchesOnThirdFailure(t *testing.T) {
	c := adaptiveFailureTestContext(&openAIAdaptiveSession{ParentGroupID: 108, CurrentLeafID: 85})
	nextGroupID := int64(34)
	apiKey := &service.APIKey{AdaptivePassiveFailoverEnabled: true}
	failoverErr := &service.UpstreamFailoverError{StatusCode: 502}
	handler := &OpenAIGatewayHandler{}

	first := handler.decideAdaptiveLeafFailure(c, apiKey, failoverErr, &nextGroupID)
	second := handler.decideAdaptiveLeafFailure(c, apiKey, failoverErr, &nextGroupID)
	third := handler.decideAdaptiveLeafFailure(c, apiKey, failoverErr, &nextGroupID)

	require.Equal(t, adaptiveLeafFailureHold, first.Action)
	require.Equal(t, 1, first.FailureCount)
	require.Equal(t, adaptiveLeafFailureHold, second.Action)
	require.Equal(t, 2, second.FailureCount)
	require.Equal(t, adaptiveLeafFailureSwitch, third.Action)
	require.Equal(t, adaptivePassiveFailureThreshold, third.FailureCount)
	require.Equal(t, "passive", third.Owner)
}

func TestAdaptivePassiveFailoverCounterResetsForNextLeaf(t *testing.T) {
	session := &openAIAdaptiveSession{
		ParentGroupID:               108,
		CurrentLeafID:               85,
		CurrentLeafUpstreamFailures: 2,
		Plan:                        adaptiveTestPlan(85, 34),
		Billing: &service.AdaptiveBillingContext{
			ReservationID:    "reservation-passive-reset",
			LogicalRequestID: "request-passive-reset",
			OwnerID:          "request-owner",
			FencingToken:     1,
			RowVersion:       1,
		},
	}
	c := adaptiveFailureTestContext(session)
	repository := &adaptiveHeartbeatRepository{}
	handler := &OpenAIGatewayHandler{
		adaptiveBilling: service.NewAdaptiveBillingCoordinator(repository, nil),
	}

	require.True(t, handler.markAdaptiveLeafInFlight(context.Background(), c, 34, 2, nil))

	require.Zero(t, session.CurrentLeafUpstreamFailures)
	require.Equal(t, int64(34), session.CurrentLeafID)
}

func TestAdaptiveAntiStallExclusivelyOwnsFailureCount(t *testing.T) {
	session := &openAIAdaptiveSession{ParentGroupID: 108, CurrentLeafID: 85}
	c := adaptiveFailureTestContext(session)
	antiCfg := service.ResolveAntiStallForKey(service.DefaultAntiStallAdminConfig(), service.AntiStallTierBasic)
	anti := service.NewAntiStallSession(antiCfg)
	_ = service.InstallAntiStallProWriter(c, anti)
	// The gateway preserves production ordering: settlement marks the failed
	// attempt and starts recovery before the owner decision consumes its state.
	anti.BeginRecovery()
	nextGroupID := int64(34)

	decision := (&OpenAIGatewayHandler{}).decideAdaptiveLeafFailure(
		c,
		&service.APIKey{AdaptivePassiveFailoverEnabled: true},
		&service.UpstreamFailoverError{StatusCode: 502},
		&nextGroupID,
	)

	require.Equal(t, "anti_stall", decision.Owner)
	require.Equal(t, 1, decision.FailureCount)
	require.Zero(t, session.CurrentLeafUpstreamFailures)
}

func TestAdaptivePassiveFailoverDisabledPreservesLegacyDecision(t *testing.T) {
	session := &openAIAdaptiveSession{ParentGroupID: 108, CurrentLeafID: 85}
	c := adaptiveFailureTestContext(session)
	nextGroupID := int64(34)

	decision := (&OpenAIGatewayHandler{}).decideAdaptiveLeafFailure(
		c,
		&service.APIKey{},
		&service.UpstreamFailoverError{StatusCode: 502},
		&nextGroupID,
	)

	require.Equal(t, adaptiveLeafFailureLegacy, decision.Action)
	require.Equal(t, "legacy", decision.Owner)
	require.Zero(t, session.CurrentLeafUpstreamFailures)
}

func TestAdaptivePassiveFailoverIgnoresTerminalFailures(t *testing.T) {
	session := &openAIAdaptiveSession{ParentGroupID: 108, CurrentLeafID: 85}
	c := adaptiveFailureTestContext(session)
	nextGroupID := int64(34)

	decision := (&OpenAIGatewayHandler{}).decideAdaptiveLeafFailure(
		c,
		&service.APIKey{AdaptivePassiveFailoverEnabled: true},
		&service.UpstreamFailoverError{StatusCode: 400, NextAccountAction: service.NextAccountStop},
		&nextGroupID,
	)

	require.Equal(t, adaptiveLeafFailureLegacy, decision.Action)
	require.Zero(t, session.CurrentLeafUpstreamFailures)
}

func adaptiveFailureTestContext(session *openAIAdaptiveSession) *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set(ginKeyOpenAIAdaptiveSession, session)
	return c
}
