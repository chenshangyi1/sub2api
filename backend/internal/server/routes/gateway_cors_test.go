package routes

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGatewayCORS_AllRegisteredRoutes(t *testing.T) {
	r := gin.New()
	var routes []gin.RouteInfo
	r.Use(middleware.CORS(config.CORSConfig{}, func(c *gin.Context) bool { return middleware.IsGatewayAPIRequest(c, routes) }))
	RegisterGatewayRoutes(r, &handler.Handlers{Gateway: &handler.GatewayHandler{}, OpenAIGateway: &handler.OpenAIGatewayHandler{}, AsyncImage: handler.NewAsyncImageHandler(nil, nil)},
		middleware.NewAPIKeyAuthMiddleware(nil, nil, &config.Config{}), nil, nil, nil, nil, nil, &config.Config{})
	routes = r.Routes()
	for _, route := range routes {
		path := strings.Split(route.Path, "*")[0]
		if strings.Contains(route.Path, "*") {
			path += "compact"
		}
		parts := strings.Split(path, "/")
		for i, part := range parts {
			if strings.HasPrefix(part, ":") {
				parts[i] = "cors-test"
			}
		}
		path = strings.Join(parts, "/")
		t.Run(route.Method+path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodOptions, path, nil)
			req.Header.Set("Origin", "https://chat.example")
			req.Header.Set("Access-Control-Request-Method", route.Method)
			req.Header.Set("Access-Control-Request-Headers", "authorization,content-type")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			require.Equal(t, 204, w.Code)
			require.Equal(t, "*", w.Header().Get("Access-Control-Allow-Origin"))
			require.Empty(t, w.Header().Get("Access-Control-Allow-Credentials"))
		})
	}
	for _, path := range []string{"/v1/models", "/models", "/backend-api/codex/models"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Origin", "https://chat.example")
		req.Header.Set("Cookie", "session=fake-login")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		require.Equal(t, 401, w.Code, path)
		require.Equal(t, "*", w.Header().Get("Access-Control-Allow-Origin"))
	}
}
