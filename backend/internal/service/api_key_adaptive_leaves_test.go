//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAttachAdaptiveLeavesStampsParentIdentityAndLeafPicker(t *testing.T) {
	parent := Group{
		ID:             108,
		Name:           "Adaptive Auto",
		Platform:       PlatformOpenAI,
		RateMultiplier: 1,
		Status:         StatusActive,
		UserVisible:    true,
		UserVisibleSet: true,
	}
	leaf := Group{
		ID:             85,
		Name:           "claude-leaf",
		Platform:       PlatformAnthropic,
		RateMultiplier: 2,
		Status:         StatusActive,
		UserVisible:    false,
		UserVisibleSet: true,
	}
	svc := &APIKeyService{
		groupRepo: &stubGroupRepoForAvailable{activeGroups: []Group{parent, leaf}},
		adaptivePool: stubAdaptivePoolRepo{snapshot: &AdaptivePoolSnapshot{
			ParentGroupID: 108,
			Platform:      PlatformOpenAI,
			Enabled:       true,
			AllowHybrid:   true,
			Members:       []AdaptiveLeafRef{{LeafGroupID: 85, Enabled: true, SortOrder: 10}},
		}},
	}

	groups := []Group{parent}
	svc.attachAdaptiveLeaves(context.Background(), groups)
	require.Equal(t, PlatformAdaptive, groups[0].Platform)
	require.Len(t, groups[0].AdaptiveLeaves, 1)
	require.Equal(t, int64(85), groups[0].AdaptiveLeaves[0].ID)
	require.Equal(t, PlatformAnthropic, groups[0].AdaptiveLeaves[0].Platform)
}

func TestStampAdaptiveParentIdentityOnAPIKeys(t *testing.T) {
	parent := Group{
		ID:             108,
		Name:           "Adaptive Auto",
		Platform:       PlatformOpenAI,
		RateMultiplier: 1,
		Status:         StatusActive,
		UserVisible:    true,
		UserVisibleSet: true,
	}
	leaf := Group{
		ID:             14,
		Name:           "gpt-leaf",
		Platform:       PlatformOpenAI,
		RateMultiplier: 1,
		Status:         StatusActive,
		UserVisible:    false,
		UserVisibleSet: true,
	}
	svc := &APIKeyService{
		groupRepo: &stubGroupRepoForAvailable{activeGroups: []Group{parent, leaf}},
		adaptivePool: stubAdaptivePoolRepo{snapshot: &AdaptivePoolSnapshot{
			ParentGroupID: 108,
			Platform:      PlatformOpenAI,
			Enabled:       true,
			Members:       []AdaptiveLeafRef{{LeafGroupID: 14, Enabled: true}},
		}},
	}
	keys := []APIKey{{Group: &Group{
		ID:             108,
		Name:           "Adaptive Auto",
		Platform:       PlatformOpenAI,
		RateMultiplier: 1,
		Status:         StatusActive,
		UserVisible:    true,
		UserVisibleSet: true,
	}}}
	svc.stampAdaptiveParentIdentity(context.Background(), keys)
	require.Equal(t, PlatformAdaptive, keys[0].Group.Platform)
	require.Len(t, keys[0].Group.AdaptiveLeaves, 1)
	require.Equal(t, int64(14), keys[0].Group.AdaptiveLeaves[0].ID)
}

func TestStampAdaptiveParentOnKeyUsesPoolWithoutHydratingLeaves(t *testing.T) {
	parentID := int64(108)
	svc := &APIKeyService{
		adaptivePool: stubAdaptivePoolRepo{snapshot: &AdaptivePoolSnapshot{
			ParentGroupID: parentID,
			Platform:      PlatformOpenAI,
			Enabled:       true,
			Members:       []AdaptiveLeafRef{{LeafGroupID: 85, Enabled: true}},
		}},
	}
	apiKey := &APIKey{
		GroupID: &parentID,
		Group: &Group{
			ID:       parentID,
			Name:     "Adaptive Auto",
			Platform: PlatformOpenAI,
			Status:   StatusActive,
		},
	}
	svc.stampAdaptiveParentOnKey(context.Background(), apiKey)
	require.Equal(t, PlatformAdaptive, apiKey.Group.Platform)
	require.Empty(t, apiKey.Group.AdaptiveLeaves)
}
