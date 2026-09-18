package handler

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestBuildOpenAIFallbackGroupAttemptsLazyKeepsConfiguredFallbackIDs(t *testing.T) {
	primaryGroupID := int64(1)
	apiKey := &service.APIKey{
		ID:      99,
		GroupID: &primaryGroupID,
		Group: &service.Group{
			ID:       primaryGroupID,
			Platform: service.PlatformOpenAI,
		},
		FallbackGroupIDs: []int64{2, 0, 2, 1, 3},
	}

	attempts := (*OpenAIGatewayHandler)(nil).buildOpenAIFallbackGroupAttempts(context.Background(), apiKey, false, false, zap.NewNop())

	require.Len(t, attempts, 1)
	require.False(t, attempts[0].Fallback)
	require.Equal(t, primaryGroupID, *attempts[0].GroupID)
}

func TestBuildOpenAIFallbackGroupAttemptsUsesOnlyAPIKeyFallbackGroups(t *testing.T) {
	primaryGroupID := int64(10)
	systemFallbackGroupID := int64(99)
	apiKey := &service.APIKey{
		ID:      99,
		GroupID: &primaryGroupID,
		Group: &service.Group{
			ID:              primaryGroupID,
			Platform:        service.PlatformOpenAI,
			FallbackGroupID: &systemFallbackGroupID,
		},
	}

	attempts := (*OpenAIGatewayHandler)(nil).buildOpenAIFallbackGroupAttempts(context.Background(), apiKey, true, false, zap.NewNop())

	require.Len(t, attempts, 1)
	require.False(t, hasNextOpenAIFallbackGroup(attempts, 0))
	require.Nil(t, nextOpenAIFallbackGroupID(attempts, 0))
}

func TestShouldFastSwitchOpenAIFallbackGroupOnlyFor5xx(t *testing.T) {
	require.False(t, shouldFastSwitchOpenAIFallbackGroup(nil))
	require.False(t, shouldFastSwitchOpenAIFallbackGroup(&service.UpstreamFailoverError{StatusCode: 429}))
	require.True(t, shouldFastSwitchOpenAIFallbackGroup(&service.UpstreamFailoverError{StatusCode: 502}))
	require.True(t, shouldFastSwitchOpenAIFallbackGroup(&service.UpstreamFailoverError{StatusCode: 503}))
}

func TestOpenAIShouldGroupFailoverAntiStallScope(t *testing.T) {
	// No gin / no Anti-Stall session → only legacy 5xx group fast-switch.
	require.False(t, openAIShouldGroupFailover(nil, &service.UpstreamFailoverError{StatusCode: 429}))
	require.False(t, openAIShouldGroupFailover(nil, &service.UpstreamFailoverError{StatusCode: 401}))
	require.True(t, openAIShouldGroupFailover(nil, &service.UpstreamFailoverError{StatusCode: 502}))

	// Force leaf for capacity/auth/quota-like; hold leaf for generic 5xx.
	require.True(t, antiStallForceLeafSwitchOnStatus(429))
	require.True(t, antiStallForceLeafSwitchOnStatus(401))
	require.True(t, antiStallForceLeafSwitchOnStatus(403))
	require.True(t, antiStallForceLeafSwitchOnStatus(413))
	require.True(t, antiStallForceLeafSwitchOnStatus(529))
	require.False(t, antiStallForceLeafSwitchOnStatus(502))
	require.False(t, antiStallForceLeafSwitchOnStatus(503))

	// Body-too-large always force-switch under helper.
	require.True(t, antiStallForceLeafSwitch(&service.UpstreamFailoverError{
		StatusCode: 413,
		Reason:     service.GatewayFailureReason("openai_request_body_too_large"),
	}))
}

func TestOpenAIPrimaryGroupAllowsImageGeneration(t *testing.T) {
	groupID := int64(1)
	fallbackGroupID := int64(2)

	t.Run("disabled primary is not overridden by image fallback", func(t *testing.T) {
		apiKey := &service.APIKey{
			GroupID: &groupID,
			Group:   &service.Group{ID: groupID, AllowImageGeneration: false},
		}
		attempts := []openAIFallbackGroupAttempt{
			{APIKey: apiKey, GroupID: &groupID, Group: apiKey.Group},
			{GroupID: &fallbackGroupID, Group: &service.Group{ID: fallbackGroupID, AllowImageGeneration: true}, Fallback: true},
		}

		require.False(t, openAIPrimaryGroupAllowsImageGeneration(apiKey, attempts))
	})

	t.Run("configured unresolved group fails closed", func(t *testing.T) {
		apiKey := &service.APIKey{GroupID: &groupID}

		require.False(t, openAIPrimaryGroupAllowsImageGeneration(apiKey, nil))
	})

	t.Run("ungrouped key preserves legacy allow behavior", func(t *testing.T) {
		require.True(t, openAIPrimaryGroupAllowsImageGeneration(&service.APIKey{}, nil))
	})
}

func TestWithOpenAIFallbackResponseHeaderTimeoutOnlyBeforeNextAttempt(t *testing.T) {
	h := &OpenAIGatewayHandler{
		cfg: &config.Config{
			Gateway: config.GatewayConfig{
				OpenAIFallbackResponseHeaderTimeout: 12,
			},
		},
	}
	primaryGroupID := int64(1)
	fallbackGroupID := int64(2)
	apiKey := &service.APIKey{
		GroupID: &primaryGroupID,
		Group:   &service.Group{ID: primaryGroupID, Platform: service.PlatformOpenAI},
	}
	attempts := []openAIFallbackGroupAttempt{
		{GroupID: &primaryGroupID, APIKey: apiKey, Group: apiKey.Group},
		{GroupID: &fallbackGroupID, Fallback: true, APIKey: apiKey, Group: apiKey.Group},
	}

	ctx := h.withOpenAIFallbackResponseHeaderTimeout(context.Background(), apiKey, attempts, 0, true, false, zap.NewNop())
	timeout, ok := service.HTTPUpstreamResponseHeaderTimeoutFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, 12*time.Second, timeout)

	lastCtx := h.withOpenAIFallbackResponseHeaderTimeout(context.Background(), apiKey, attempts, 1, true, false, zap.NewNop())
	_, ok = service.HTTPUpstreamResponseHeaderTimeoutFromContext(lastCtx)
	require.False(t, ok)
}

func TestNextOpenAIFallbackGroupIDSkipsSkippedAttempts(t *testing.T) {
	primaryGroupID := int64(1)
	skippedGroupID := int64(2)
	nextGroupID := int64(3)
	attempts := []openAIFallbackGroupAttempt{
		{GroupID: &primaryGroupID},
		{GroupID: &skippedGroupID, Fallback: true, Skipped: true},
		{GroupID: &nextGroupID, Fallback: true},
	}

	require.True(t, hasNextOpenAIFallbackGroup(attempts, 0))
	require.Equal(t, nextGroupID, *nextOpenAIFallbackGroupID(attempts, 0))
	require.True(t, hasNextOpenAIFallbackGroup(attempts, 1))
	require.Equal(t, nextGroupID, *nextOpenAIFallbackGroupID(attempts, 1))
	require.False(t, hasNextOpenAIFallbackGroup(attempts, 2))
}

func TestWithOpenAIFallbackResponseHeaderTimeoutIgnoresSkippedAttempts(t *testing.T) {
	h := &OpenAIGatewayHandler{
		cfg: &config.Config{
			Gateway: config.GatewayConfig{
				OpenAIFallbackResponseHeaderTimeout: 12,
			},
		},
	}
	primaryGroupID := int64(1)
	fallbackGroupID := int64(2)
	apiKey := &service.APIKey{
		GroupID: &primaryGroupID,
		Group:   &service.Group{ID: primaryGroupID, Platform: service.PlatformOpenAI},
	}
	attempts := []openAIFallbackGroupAttempt{
		{GroupID: &primaryGroupID, APIKey: apiKey, Group: apiKey.Group},
		{GroupID: &fallbackGroupID, Fallback: true, Skipped: true},
	}

	ctx := h.withOpenAIFallbackResponseHeaderTimeout(context.Background(), apiKey, attempts, 0, true, false, zap.NewNop())
	_, ok := service.HTTPUpstreamResponseHeaderTimeoutFromContext(ctx)
	require.False(t, ok)
}

func TestNextUsableOpenAIFallbackGroupIDSkipsImageDisabledGroups(t *testing.T) {
	h := &OpenAIGatewayHandler{}
	primaryGroupID := int64(1)
	imageDisabledGroupID := int64(2)
	imageAllowedGroupID := int64(3)
	apiKey := &service.APIKey{
		GroupID: &primaryGroupID,
		Group:   &service.Group{ID: primaryGroupID, Platform: service.PlatformOpenAI, AllowImageGeneration: true},
	}
	imageDisabledAPIKey := &service.APIKey{
		GroupID: &imageDisabledGroupID,
		Group:   &service.Group{ID: imageDisabledGroupID, Platform: service.PlatformOpenAI},
	}
	imageAllowedAPIKey := &service.APIKey{
		GroupID: &imageAllowedGroupID,
		Group:   &service.Group{ID: imageAllowedGroupID, Platform: service.PlatformOpenAI, AllowImageGeneration: true},
	}
	attempts := []openAIFallbackGroupAttempt{
		{GroupID: &primaryGroupID, APIKey: apiKey, Group: apiKey.Group},
		{GroupID: &imageDisabledGroupID, APIKey: imageDisabledAPIKey, Group: imageDisabledAPIKey.Group, Fallback: true},
		{GroupID: &imageAllowedGroupID, APIKey: imageAllowedAPIKey, Group: imageAllowedAPIKey.Group, Fallback: true},
	}

	got := h.nextUsableOpenAIFallbackGroupID(context.Background(), apiKey, attempts, 0, true, true, zap.NewNop())
	require.NotNil(t, got)
	require.Equal(t, imageAllowedGroupID, *got)
	require.True(t, attempts[1].Skipped)
}
