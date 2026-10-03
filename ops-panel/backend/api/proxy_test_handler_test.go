package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestProxyTestFallsBackToSecondProvider(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ipify":
			http.Error(w, "down", http.StatusBadGateway)
		case "/ipinfo":
			_, _ = w.Write([]byte(`{"ip":"203.0.113.9"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	proxyHits := 0
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyHits++
		target := r.URL.String()
		if !strings.HasPrefix(target, "http") {
			target = upstream.URL + r.URL.Path
		}
		resp, err := http.Get(target)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
	}))
	defer proxy.Close()

	oldProviders := proxyIPProviders
	proxyIPProviders = []proxyIPProvider{
		{
			name: "ipify",
			url:  upstream.URL + "/ipify",
			parseIP: func(body []byte) string {
				var out struct {
					IP string `json:"ip"`
				}
				_ = json.Unmarshal(body, &out)
				return out.IP
			},
		},
		{
			name: "ipinfo",
			url:  upstream.URL + "/ipinfo",
			parseIP: func(body []byte) string {
				var out struct {
					IP string `json:"ip"`
				}
				_ = json.Unmarshal(body, &out)
				return out.IP
			},
		},
	}
	t.Cleanup(func() { proxyIPProviders = oldProviders })

	proxyURL, err := url.Parse(proxy.URL)
	if err != nil {
		t.Fatalf("parse proxy url: %v", err)
	}
	r := gin.New()
	apiGroup := r.Group("/api")
	registerSettings(apiGroup, &Deps{})

	body := `{"protocol":"http","host":"` + proxyURL.Hostname() + `","port":` + proxyURL.Port() + `}`
	req := httptest.NewRequest(http.MethodPost, "/api/settings/proxy/test", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}

	var got struct {
		Data proxyTestResult `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.Data.OK || got.Data.Provider != "ipinfo" || got.Data.IP != "203.0.113.9" {
		t.Fatalf("result = %#v", got.Data)
	}
	if proxyHits != 2 {
		t.Fatalf("proxy hits = %d, want 2", proxyHits)
	}
}
