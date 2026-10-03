package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bejix/upstream-ops/backend/channel"
	"github.com/bejix/upstream-ops/backend/connector"
	"github.com/bejix/upstream-ops/backend/storage"
	"github.com/gin-gonic/gin"
)

type apiKeyChannelServiceStub struct {
	*channel.Service
	page      *connector.APIKeyPage
	groups    []connector.APIKeyGroup
	created   *connector.APIKey
	updated   *connector.APIKey
	revealed  string
	lastQuery connector.APIKeyQuery
}

func (s *apiKeyChannelServiceStub) GetRechargeInfo(ctx context.Context, channelID uint) (*connector.RechargeInfo, error) {
	return nil, nil
}

func (s *apiKeyChannelServiceStub) CreateRecharge(ctx context.Context, channelID uint, req connector.RechargeRequest) (*connector.RechargeLaunch, error) {
	return nil, nil
}

func (s *apiKeyChannelServiceStub) GetSubscriptionInfo(ctx context.Context, channelID uint) (*connector.SubscriptionInfo, error) {
	return nil, nil
}

func (s *apiKeyChannelServiceStub) CreateSubscription(ctx context.Context, channelID uint, req connector.SubscriptionRequest) (*connector.SubscriptionLaunch, error) {
	return nil, nil
}

func (s *apiKeyChannelServiceStub) GetSubscriptionUsage(ctx context.Context, channelID uint) (*connector.SubscriptionUsageInfo, error) {
	return nil, nil
}

func (s *apiKeyChannelServiceStub) ListAPIKeys(ctx context.Context, channelID uint, query connector.APIKeyQuery) (*connector.APIKeyPage, error) {
	s.lastQuery = query
	return s.page, nil
}

func (s *apiKeyChannelServiceStub) ListAPIKeyGroups(ctx context.Context, channelID uint) ([]connector.APIKeyGroup, error) {
	return s.groups, nil
}

func (s *apiKeyChannelServiceStub) CreateAPIKey(ctx context.Context, channelID uint, req connector.APIKeyCreateRequest) (*connector.APIKey, error) {
	return s.created, nil
}

func (s *apiKeyChannelServiceStub) UpdateAPIKey(ctx context.Context, channelID uint, keyID int64, req connector.APIKeyUpdateRequest) (*connector.APIKey, error) {
	return s.updated, nil
}

func (s *apiKeyChannelServiceStub) DeleteAPIKey(ctx context.Context, channelID uint, keyID int64) error {
	return nil
}

func (s *apiKeyChannelServiceStub) RevealAPIKey(ctx context.Context, channelID uint, keyID int64) (string, error) {
	return s.revealed, nil
}

func TestChannelAPIKeyEndpoints(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := openTestDB(t)
	channels := storage.NewChannels(db)
	if err := channels.Create(&storage.Channel{
		Name:           "a",
		Type:           storage.ChannelTypeNewAPI,
		SiteURL:        "https://a.example.com",
		Username:       "u1",
		PasswordCipher: "x",
		MonitorEnabled: true,
	}); err != nil {
		t.Fatalf("create channel: %v", err)
	}

	stub := &apiKeyChannelServiceStub{
		page: &connector.APIKeyPage{
			Items:    []connector.APIKey{{ID: 11, Name: "key1", Status: "active", GroupName: "default", GroupDescription: "desc", GroupRatio: 1}},
			Total:    1,
			Page:     1,
			PageSize: 20,
			Pages:    1,
		},
		groups:   []connector.APIKeyGroup{{Name: "default", Description: "desc", Ratio: 1}},
		created:  &connector.APIKey{ID: 12, Name: "created", Status: "active"},
		updated:  &connector.APIKey{ID: 11, Name: "updated", Status: "disabled"},
		revealed: "sk-full",
	}
	r := gin.New()
	apiGroup := r.Group("/api")
	registerChannels(apiGroup, &Deps{Channels: channels, ChannelSvc: stub})

	req := httptest.NewRequest(http.MethodGet, "/api/channels/1/api-keys?page=1&page_size=20&search=abc", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d body = %s", rec.Code, rec.Body.String())
	}
	if stub.lastQuery.Page != 1 || stub.lastQuery.PageSize != 20 || stub.lastQuery.Search != "abc" {
		t.Fatalf("query = %#v", stub.lastQuery)
	}
	var listResp struct {
		Data connector.APIKeyPage `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listResp.Data.Items) != 1 || listResp.Data.Items[0].ID != 11 {
		t.Fatalf("list response = %#v", listResp.Data)
	}

	groupsReq := httptest.NewRequest(http.MethodGet, "/api/channels/1/api-keys/groups", nil)
	groupsRec := httptest.NewRecorder()
	r.ServeHTTP(groupsRec, groupsReq)
	if groupsRec.Code != http.StatusOK {
		t.Fatalf("groups status = %d body = %s", groupsRec.Code, groupsRec.Body.String())
	}

	createReq := httptest.NewRequest(http.MethodPost, "/api/channels/1/api-keys", strings.NewReader(`{"name":"created"}`))
	createReq.Header.Set("Content-Type", "application/json")
	createRec := httptest.NewRecorder()
	r.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusOK {
		t.Fatalf("create status = %d body = %s", createRec.Code, createRec.Body.String())
	}

	updateReq := httptest.NewRequest(http.MethodPut, "/api/channels/1/api-keys/11", strings.NewReader(`{"status":"disabled"}`))
	updateReq.Header.Set("Content-Type", "application/json")
	updateRec := httptest.NewRecorder()
	r.ServeHTTP(updateRec, updateReq)
	if updateRec.Code != http.StatusOK {
		t.Fatalf("update status = %d body = %s", updateRec.Code, updateRec.Body.String())
	}

	revealReq := httptest.NewRequest(http.MethodPost, "/api/channels/1/api-keys/11/reveal", nil)
	revealRec := httptest.NewRecorder()
	r.ServeHTTP(revealRec, revealReq)
	if revealRec.Code != http.StatusOK {
		t.Fatalf("reveal status = %d body = %s", revealRec.Code, revealRec.Body.String())
	}
	var revealResp struct {
		Data struct {
			Key string `json:"key"`
		} `json:"data"`
	}
	if err := json.Unmarshal(revealRec.Body.Bytes(), &revealResp); err != nil {
		t.Fatalf("decode reveal: %v", err)
	}
	if revealResp.Data.Key != "sk-full" {
		t.Fatalf("revealed key = %q", revealResp.Data.Key)
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/channels/1/api-keys/11", nil)
	deleteRec := httptest.NewRecorder()
	r.ServeHTTP(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusOK {
		t.Fatalf("delete status = %d body = %s", deleteRec.Code, deleteRec.Body.String())
	}
}

func TestChannelAPIKeyValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	apiGroup := r.Group("/api")
	registerChannels(apiGroup, &Deps{ChannelSvc: &apiKeyChannelServiceStub{}})

	req := httptest.NewRequest(http.MethodGet, "/api/channels/1/api-keys?page=0", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("list invalid status = %d body = %s", rec.Code, rec.Body.String())
	}

	createReq := httptest.NewRequest(http.MethodPost, "/api/channels/1/api-keys", strings.NewReader(`{"name":" "}`))
	createReq.Header.Set("Content-Type", "application/json")
	createRec := httptest.NewRecorder()
	r.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusBadRequest {
		t.Fatalf("create invalid status = %d body = %s", createRec.Code, createRec.Body.String())
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/channels/1/api-keys/0", nil)
	deleteRec := httptest.NewRecorder()
	r.ServeHTTP(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusBadRequest {
		t.Fatalf("delete invalid status = %d body = %s", deleteRec.Code, deleteRec.Body.String())
	}
}
