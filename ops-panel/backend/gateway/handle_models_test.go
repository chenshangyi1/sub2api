package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bejix/upstream-ops/backend/crypto"
	"github.com/bejix/upstream-ops/backend/storage"
	"github.com/gin-gonic/gin"
)

func TestHandleModels_AggregatesProviderRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)

	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk-upstream-test" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"data": []map[string]any{
				{"id": "deepseek-v4-flash"},
				{"id": "kimi-k2.5"},
			},
		})
	}))
	t.Cleanup(up.Close)

	db := openGatewayTestDB(t)
	groupsRepo := storage.NewGatewayGroups(db)
	keysRepo := storage.NewGatewayKeys(db)
	routesRepo := storage.NewGatewayRoutes(db)
	providersRepo := storage.NewGatewayProviders(db)
	cipher, err := crypto.NewCipher("test-secret-handle-models")
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}

	g := &storage.GatewayGroup{
		Name:              "ds-test",
		Status:            storage.GatewayGroupStatusActive,
		ModelsMode:        storage.GatewayModelsModeAuto,
		RateSortDirection: "asc",
	}
	if err := groupsRepo.Create(g); err != nil {
		t.Fatalf("create group: %v", err)
	}

	cipherText, err := cipher.Encrypt("sk-upstream-test")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	p := &storage.GatewayProvider{
		Name:         "opencode",
		BaseURL:      up.URL,
		APIKeyCipher: cipherText,
		APIKeyHint:   "sk-u…test",
		Enabled:      true,
	}
	if err := providersRepo.Create(p); err != nil {
		t.Fatalf("create provider: %v", err)
	}

	if err := routesRepo.SaveForGroup(g.ID, []storage.GatewayRoute{{
		GatewayGroupID:    g.ID,
		Position:          0,
		SourceKind:        storage.GatewayRouteSourceProvider,
		GatewayProviderID: p.ID,
		Weight:            1,
		Enabled:           true,
		RateConvertMode:   "custom",
		RateConvertValue:  1,
	}}); err != nil {
		t.Fatalf("save routes: %v", err)
	}

	clientKey := "sk-5f613bdceadebf61"
	keyCipher, err := cipher.Encrypt(clientKey)
	if err != nil {
		t.Fatalf("encrypt client key: %v", err)
	}
	gk := &storage.GatewayKey{
		GroupID:   g.ID,
		Name:      "client",
		KeyHash:   HashAPIKey(clientKey),
		KeyPrefix: KeyPrefix(clientKey),
		KeyCipher: keyCipher,
		Status:    storage.GatewayKeyStatusActive,
	}
	if err := keysRepo.Create(gk); err != nil {
		t.Fatalf("create key: %v", err)
	}

	svc := NewService(groupsRepo, keysRepo, routesRepo, nil, nil, nil, nil, cipher, nil)
	svc.SetProviders(providersRepo)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer "+clientKey)
	c.Request = req

	svc.HandleModels(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var payload struct {
		Object string `json:"object"`
		Data   []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v body=%s", err, w.Body.String())
	}
	if len(payload.Data) != 2 {
		t.Fatalf("want 2 models, got %d body=%s", len(payload.Data), w.Body.String())
	}
	got := map[string]bool{}
	for _, m := range payload.Data {
		got[m.ID] = true
	}
	if !got["deepseek-v4-flash"] || !got["kimi-k2.5"] {
		t.Fatalf("models=%v", payload.Data)
	}
}
