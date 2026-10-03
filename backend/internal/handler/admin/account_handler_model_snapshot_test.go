package admin

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type modelSnapshotAdminStub struct {
	*availableModelsAdminService
	updates   map[string]any
	updateErr error
}

func (s *modelSnapshotAdminStub) UpdateAccountExtra(_ context.Context, _ int64, updates map[string]any) error {
	s.updates = updates
	if s.updateErr != nil {
		return s.updateErr
	}
	if s.account.Extra == nil {
		s.account.Extra = map[string]any{}
	}
	for key, value := range updates {
		s.account.Extra[key] = value
	}
	return nil
}

func TestSyncUpstreamModelsPersistsSnapshot(t *testing.T) {
	for _, tc := range []struct {
		name       string
		status     int
		body       string
		prior      bool
		writeError bool
		wantHTTP   int
		wantStatus string
	}{
		{"success", 200, `{"data":[{"id":"gpt-5.4"},{"id":"gpt-5.4"}]}`, false, false, 200, "fresh"},
		{"replaces obsolete list", 200, `{"data":[{"id":"gpt-5.4"}]}`, true, false, 200, "fresh"},
		{"error retains list", 503, `{"error":"upstream secret"}`, true, false, 502, "stale"},
		{"empty retains list", 200, `{"data":[]}`, true, false, 502, "stale"},
		{"no previous list", 503, `{"error":"upstream secret"}`, false, false, 502, "unknown"},
		{"write failure", 200, `{"data":[{"id":"gpt-5.4"}]}`, false, true, 500, "fresh"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := &modelSnapshotAdminStub{availableModelsAdminService: &availableModelsAdminService{stubAdminService: newStubAdminService(), account: service.Account{ID: 44, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Credentials: map[string]any{"api_key": "test", "base_url": "https://openai.example.com/v1"}, Extra: map[string]any{"unrelated": "keep"}}}}
			if tc.prior {
				svc.account.Extra[service.UpstreamModelsExtraKey] = []string{"old-model"}
				svc.account.Extra[service.UpstreamModelsSyncStatusExtraKey] = "fresh"
				svc.account.Extra[service.UpstreamModelsSyncedAtExtraKey] = "2026-01-01T00:00:00Z"
			}
			if tc.writeError {
				svc.updateErr = errors.New("db secret")
			}
			router := setupSyncUpstreamModelsRouter(svc, &syncUpstreamHTTPUpstream{resp: &http.Response{StatusCode: tc.status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(tc.body))}})
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/44/models/sync-upstream", nil))
			require.Equal(t, tc.wantHTTP, rec.Code, rec.Body.String())
			require.NotContains(t, rec.Body.String(), "secret")
			require.Equal(t, tc.wantStatus, svc.updates[service.UpstreamModelsSyncStatusExtraKey])
			require.Equal(t, "keep", svc.account.Extra["unrelated"])
			if tc.wantStatus == "fresh" && !tc.writeError {
				require.Equal(t, []string{"gpt-5.4"}, svc.account.Extra[service.UpstreamModelsExtraKey])
				_, err := time.Parse(time.RFC3339, svc.account.Extra[service.UpstreamModelsSyncedAtExtraKey].(string))
				require.NoError(t, err)
				require.Equal(t, "models_api", svc.account.Extra[service.UpstreamModelsSourceExtraKey])
			}
			if tc.wantStatus == "stale" {
				require.Equal(t, []string{"old-model"}, svc.account.Extra[service.UpstreamModelsExtraKey])
				require.Equal(t, "2026-01-01T00:00:00Z", svc.account.Extra[service.UpstreamModelsSyncedAtExtraKey])
			}
		})
	}
}
