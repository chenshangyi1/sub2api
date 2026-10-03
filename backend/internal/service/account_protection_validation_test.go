package service

import (
	"encoding/json"
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

// productionOpenAILegacyAccount2738 matches the live extra shape of account
// 2738 (OpenAI OAuth, unversioned legacy marker, nodejs24 TLS).
func productionOpenAILegacyAccount2738() *Account {
	return &Account{
		ID:          2738,
		Name:        "team-aatrisaorbal@gmail.com-ws-e8edc59f",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Concurrency: 10,
		Extra: map[string]any{
			"anti_degradation":       true,
			"protection_scope":       "legacy",
			"enable_tls_fingerprint": true,
			"tls_fingerprint_builtin": "nodejs24",
			"codex_fingerprint_mode":  "session",
			"codex_fingerprint_seed":  testCodexFingerprintSeed,
			"anti_degrade": map[string]any{
				"enabled": true,
				"mode":    "legacy",
			},
		},
	}
}

func TestValidateRegisteredProtection_OpenAILegacyTLSFromProduction2738(t *testing.T) {
	a := productionOpenAILegacyAccount2738()
	require.True(t, a.IsTLSFingerprintEnabled())
	require.Equal(t, "nodejs24", tlsFingerprintBuiltinName(a))
	require.NoError(t, validateRegisteredProtection(a))

	stored, err := json.Marshal(a.Extra)
	require.NoError(t, err)
	var loaded map[string]any
	require.NoError(t, json.Unmarshal(stored, &loaded))
	roundTrip := *a
	roundTrip.Extra = loaded
	require.True(t, roundTrip.IsTLSFingerprintEnabled())
	require.NoError(t, validateRegisteredProtection(&roundTrip))

	profile, err := resolveMode1TLSProfile(&roundTrip)
	require.NoError(t, err)
	require.NotNil(t, profile)
	require.Equal(t, "Node.js 24 compatibility", profile.Name)
}

func TestValidateRegisteredProtection_OpenAILegacyTLSMismatchStillFails(t *testing.T) {
	a := productionOpenAILegacyAccount2738()
	a.Extra["enable_tls_fingerprint"] = false
	err := validateRegisteredProtection(a)
	require.Error(t, err)
	require.True(t, infraerrors.IsBadRequest(err))
	require.ErrorContains(t, err, "TLS 配置与已选策略不一致")
	require.ErrorContains(t, err, "PROTECTION_CONFIGURATION_INVALID")
}
