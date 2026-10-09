package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestCORS_GatewayRoutes(t *testing.T) {
	r := gin.New()
	var gatewayRoutes []gin.RouteInfo
	r.Use(CORS(config.CORSConfig{AllowedOrigins: []string{"https://panel.example"}, AllowCredentials: true}, func(c *gin.Context) bool { return IsGatewayAPIRequest(c, gatewayRoutes) }))
	r.GET("/v1/models", func(c *gin.Context) { c.Status(200) })
	r.POST("/v1/chat/completions", func(c *gin.Context) { c.Status(200) })
	r.POST("/responses/*subpath", func(c *gin.Context) { c.Status(200) })
	r.GET("/v1beta/models/:model", func(c *gin.Context) { c.Status(200) })
	gatewayRoutes = r.Routes()
	r.GET("/api/v1/admin/settings", func(c *gin.Context) { c.Status(401) })
	r.POST("/api/v1/auth/login", func(c *gin.Context) { c.Status(401) })
	for _, origin := range []string{"https://chat.example", "null", "https://panel.example"} {
		for _, path := range []string{"/v1/models", "/v1/chat/completions", "/responses/compact", "/v1beta/models/gemini-test"} {
			t.Run(origin+path, func(t *testing.T) {
				req := httptest.NewRequest(http.MethodOptions, path, nil)
				req.Header.Set("Origin", origin)
				method := http.MethodPost
				if path == "/v1/models" || path == "/v1beta/models/gemini-test" {
					method = http.MethodGet
				}
				req.Header.Set("Access-Control-Request-Method", method)
				req.Header.Set("Access-Control-Request-Headers", "authorization,content-type,x-api-key,x-goog-api-key,anthropic-version,anthropic-dangerous-direct-browser-access")
				w := httptest.NewRecorder()
				r.ServeHTTP(w, req)
				require.Equal(t, 204, w.Code)
				require.Equal(t, "*", w.Header().Get("Access-Control-Allow-Origin"))
				require.Empty(t, w.Header().Get("Access-Control-Allow-Credentials"))
				for _, h := range []string{"Authorization", "X-API-Key", "X-Goog-API-Key", "Anthropic-Version", "Anthropic-Dangerous-Direct-Browser-Access"} {
					require.Contains(t, w.Header().Get("Access-Control-Allow-Headers"), h)
				}
			})
		}
	}
	for _, path := range []string{"/api/v1/admin/settings", "/api/v1/auth/login", "/api/v1/usage", "/v1/admin", "/v1evil/models", "/backend-api/admin", "/responses/../api/v1/admin/settings"} {
		req := httptest.NewRequest(http.MethodOptions, path, nil)
		req.Header.Set("Origin", "https://chat.example")
		req.Header.Set("Access-Control-Request-Method", "GET")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		require.Equal(t, 403, w.Code, path)
		require.Empty(t, w.Header().Get("Access-Control-Allow-Origin"), path)
	}
	req := httptest.NewRequest(http.MethodOptions, "/api/v1/admin/settings", nil)
	req.Header.Set("Origin", "https://panel.example")
	req.Header.Set("Access-Control-Request-Method", "GET")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, 204, w.Code)
	require.Equal(t, "https://panel.example", w.Header().Get("Access-Control-Allow-Origin"))
	require.Equal(t, "true", w.Header().Get("Access-Control-Allow-Credentials"))
}

func TestIsGatewayAPIRequest_ExcludesPanelAPI(t *testing.T) {
	routes := []gin.RouteInfo{{Method: "GET", Path: "/v1/models"}, {Method: "GET", Path: "/v1beta/models"}, {Method: "GET", Path: "/backend-api/codex/models"}, {Method: "GET", Path: "/responses"}, {Method: "GET", Path: "/chat/completions"}}
	for _, test := range []struct {
		path string
		want bool
	}{
		{"/v1/models", true}, {"/v1beta/models", true}, {"/responses", true},
		{"/backend-api/codex/models", true}, {"/chat/completions", true},
		{"/api/v1/admin/settings", false}, {"/api/v1/auth/login", false}, {"/v1evil/models", false},
	} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodOptions, test.path, nil)
		c.Request.Header.Set("Access-Control-Request-Method", "GET")
		require.Equal(t, test.want, IsGatewayAPIRequest(c, routes), test.path)
	}
}
