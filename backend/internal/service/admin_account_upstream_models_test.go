//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUpstreamModelSnapshotIdentity(t *testing.T) {
	account := observedModelTestAccount(91, PlatformOpenAI, []string{"gpt-5.4"})
	account.Credentials = map[string]any{"base_url": "https://one.example/v1", "api_key": "first-key"}
	account.Extra[UpstreamModelsIdentityExtraKey] = account.UpstreamModelsIdentity()
	require.False(t, account.SupportsUpstreamModel("unlisted"))
	account.Credentials["api_key"] = "second-key"
	require.Equal(t, UpstreamModelSyncStatusUnknown, account.UpstreamModelCapability().Status)
	require.True(t, supportsUpstreamModelForRequest(context.Background(), &account, "unlisted", false))
	account.Credentials["api_key"] = "first-key"
	account.Credentials["base_url"] = "https://two.example/v1"
	require.True(t, account.SupportsUpstreamModel("unlisted"))
}

func TestUpstreamModelSnapshotManagedCreateAndDuplicate(t *testing.T) {
	for _, create := range []bool{false, true} {
		extra := map[string]any{UpstreamModelsExtraKey: []string{"old"}, UpstreamModelsSyncStatusExtraKey: "fresh", UpstreamModelsIdentityExtraKey: "old-identity", "display": "keep"}
		var result map[string]any
		if create {
			account, err := buildAccountForCreate(&CreateAccountInput{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}, extra)
			require.NoError(t, err)
			result = account.Extra
		} else {
			var err error
			result, err = duplicateAccountExtra(extra)
			require.NoError(t, err)
		}
		require.NotContains(t, result, UpstreamModelsExtraKey)
		require.NotContains(t, result, UpstreamModelsSyncStatusExtraKey)
		require.NotContains(t, result, UpstreamModelsIdentityExtraKey)
		require.Equal(t, "keep", result["display"])
	}
}

func TestUpstreamModelSnapshotManagedUpdate(t *testing.T) {
	for _, existing := range []bool{false, true} {
		account := observedModelTestAccount(92, PlatformOpenAI, nil)
		if existing {
			account.Extra = map[string]any{UpstreamModelsExtraKey: []string{"current"}, UpstreamModelsSyncStatusExtraKey: "fresh"}
		}
		repo := &upstreamBillingProbeAdminRepo{upstreamBillingProbeAccountRepo: &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{92: &account}}}
		svc := &adminServiceImpl{accountRepo: repo}
		updated, err := svc.UpdateAccount(context.Background(), 92, &UpdateAccountInput{Extra: map[string]any{UpstreamModelsExtraKey: []string{"stale-client-copy"}, UpstreamModelsSyncStatusExtraKey: "fresh"}})
		require.NoError(t, err)
		if existing {
			require.Equal(t, []string{"current"}, updated.Extra[UpstreamModelsExtraKey])
		} else {
			require.NotContains(t, updated.Extra, UpstreamModelsExtraKey)
		}
	}
}
