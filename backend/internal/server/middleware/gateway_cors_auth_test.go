//go:build unit

package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGatewayCORS_RequiresAPIKeyNotSessionCookie(t *testing.T) {
	group := &service.Group{ID: 1, Status: service.StatusActive, Hydrated: true}
	key := &service.APIKey{ID: 1, Key: "cors-test-key", Status: service.StatusActive, User: &service.User{ID: 1, Status: service.StatusActive, Balance: 10}, Group: group, GroupID: &group.ID}
	repo := &stubApiKeyRepo{getByKey: func(_ context.Context, value string) (*service.APIKey, error) {
		if value != key.Key {
			return nil, service.ErrAPIKeyNotFound
		}
		copy := *key
		return &copy, nil
	}}
	cfg := &config.Config{RunMode: config.RunModeSimple}
	svc := service.NewAPIKeyService(repo, nil, nil, nil, nil, nil, cfg)
	r := gin.New()
	var routes []gin.RouteInfo
	r.Use(CORS(config.CORSConfig{AllowCredentials: true}, func(c *gin.Context) bool { return IsGatewayAPIRequest(c, routes) }))
	r.GET("/v1/models", gin.HandlerFunc(NewAPIKeyAuthMiddleware(svc, nil, cfg)), func(c *gin.Context) { c.JSON(200, gin.H{"data": []any{}}) })
	routes = r.Routes()
	for _, tt := range []struct {
		name, authorization, cookie string
		status                      int
	}{
		{"missing", "", "", 401},
		{"cookie_only", "", "session=fake-jwt; access_token=fake", 401},
		{"invalid", "Bearer invalid-cors-test", "", 401},
		{"valid", "Bearer cors-test-key", "", 200},
	} {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
			req.Header.Set("Origin", "https://chat.example")
			req.Header.Set("Authorization", tt.authorization)
			req.Header.Set("Cookie", tt.cookie)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			require.Equal(t, tt.status, w.Code)
			require.Equal(t, "*", w.Header().Get("Access-Control-Allow-Origin"))
			require.Empty(t, w.Header().Get("Access-Control-Allow-Credentials"))
		})
	}
}
