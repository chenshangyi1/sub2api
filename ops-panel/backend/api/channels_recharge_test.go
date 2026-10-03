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

type rechargeChannelServiceStub struct {
	*channel.Service
	info               *connector.RechargeInfo
	launch             *connector.RechargeLaunch
	subscriptionInfo   *connector.SubscriptionInfo
	subscriptionLaunch *connector.SubscriptionLaunch
	subscriptionUsage  *connector.SubscriptionUsageInfo
	subscriptionReq    connector.SubscriptionRequest
}

func (s *rechargeChannelServiceStub) GetRechargeInfo(ctx context.Context, channelID uint) (*connector.RechargeInfo, error) {
	return s.info, nil
}

func (s *rechargeChannelServiceStub) CreateRecharge(ctx context.Context, channelID uint, req connector.RechargeRequest) (*connector.RechargeLaunch, error) {
	return s.launch, nil
}

func (s *rechargeChannelServiceStub) GetSubscriptionInfo(ctx context.Context, channelID uint) (*connector.SubscriptionInfo, error) {
	return s.subscriptionInfo, nil
}

func (s *rechargeChannelServiceStub) CreateSubscription(ctx context.Context, channelID uint, req connector.SubscriptionRequest) (*connector.SubscriptionLaunch, error) {
	s.subscriptionReq = req
	return s.subscriptionLaunch, nil
}

func (s *rechargeChannelServiceStub) GetSubscriptionUsage(ctx context.Context, channelID uint) (*connector.SubscriptionUsageInfo, error) {
	return s.subscriptionUsage, nil
}

func TestChannelRechargeEndpoints(t *testing.T) {
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

	r := gin.New()
	apiGroup := r.Group("/api")
	stub := &rechargeChannelServiceStub{
		info: &connector.RechargeInfo{
			AmountLabel:   "充值金额",
			AmountStep:    0.01,
			MinAmount:     5,
			PresetAmounts: []float64{10, 20},
			Methods: []connector.RechargeMethod{
				{Type: "alipay", Name: "支付宝", MinAmount: 5},
			},
		},
		launch: &connector.RechargeLaunch{
			Mode:   "redirect",
			PayURL: "https://pay.example.com/go",
		},
		subscriptionInfo: &connector.SubscriptionInfo{
			Plans: []connector.SubscriptionPlan{{
				ID:       "7",
				Name:     "Pro",
				Price:    29,
				Currency: "CNY",
			}},
			Methods: []connector.SubscriptionMethod{{Type: "alipay", Name: "支付宝"}},
		},
		subscriptionLaunch: &connector.SubscriptionLaunch{
			Mode:   "redirect",
			PayURL: "https://pay.example.com/sub",
		},
		subscriptionUsage: &connector.SubscriptionUsageInfo{
			Items: []connector.SubscriptionUsage{{
				ID:        3,
				GroupName: "pro",
				Daily: &connector.SubscriptionUsageWindow{
					LimitUSD:         10,
					UsedUSD:          8,
					RemainingUSD:     2,
					RemainingPercent: 20,
					UsedPercent:      80,
				},
			}},
		},
	}
	registerChannels(apiGroup, &Deps{
		Channels:   channels,
		ChannelSvc: stub,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/channels/1/recharge-info", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("info status = %d body = %s", rec.Code, rec.Body.String())
	}

	var infoResp struct {
		Data struct {
			AmountLabel string `json:"amount_label"`
			Methods     []struct {
				Type string `json:"type"`
			} `json:"methods"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &infoResp); err != nil {
		t.Fatalf("decode info: %v", err)
	}
	if infoResp.Data.AmountLabel != "充值金额" || len(infoResp.Data.Methods) != 1 || infoResp.Data.Methods[0].Type != "alipay" {
		t.Fatalf("unexpected info: %#v", infoResp.Data)
	}

	postReq := httptest.NewRequest(http.MethodPost, "/api/channels/1/recharge", strings.NewReader(`{"amount":12.5,"payment_method":"alipay","is_mobile":true}`))
	postReq.Header.Set("Content-Type", "application/json")
	postRec := httptest.NewRecorder()
	r.ServeHTTP(postRec, postReq)
	if postRec.Code != http.StatusOK {
		t.Fatalf("recharge status = %d body = %s", postRec.Code, postRec.Body.String())
	}
	var launchResp struct {
		Data struct {
			Mode   string `json:"mode"`
			PayURL string `json:"pay_url"`
		} `json:"data"`
	}
	if err := json.Unmarshal(postRec.Body.Bytes(), &launchResp); err != nil {
		t.Fatalf("decode launch: %v", err)
	}
	if launchResp.Data.Mode != "redirect" || launchResp.Data.PayURL != "https://pay.example.com/go" {
		t.Fatalf("unexpected launch: %#v", launchResp.Data)
	}

	subInfoReq := httptest.NewRequest(http.MethodGet, "/api/channels/1/subscription-info", nil)
	subInfoRec := httptest.NewRecorder()
	r.ServeHTTP(subInfoRec, subInfoReq)
	if subInfoRec.Code != http.StatusOK {
		t.Fatalf("subscription info status = %d body = %s", subInfoRec.Code, subInfoRec.Body.String())
	}
	var subInfoResp struct {
		Data struct {
			Plans []struct {
				ID string `json:"id"`
			} `json:"plans"`
			Methods []struct {
				Type string `json:"type"`
			} `json:"methods"`
		} `json:"data"`
	}
	if err := json.Unmarshal(subInfoRec.Body.Bytes(), &subInfoResp); err != nil {
		t.Fatalf("decode subscription info: %v", err)
	}
	if len(subInfoResp.Data.Plans) != 1 || subInfoResp.Data.Plans[0].ID != "7" || len(subInfoResp.Data.Methods) != 1 {
		t.Fatalf("unexpected subscription info: %#v", subInfoResp.Data)
	}

	subPostReq := httptest.NewRequest(http.MethodPost, "/api/channels/1/subscription", strings.NewReader(`{"plan_id":"7","payment_method":"alipay","is_mobile":true}`))
	subPostReq.Header.Set("Content-Type", "application/json")
	subPostRec := httptest.NewRecorder()
	r.ServeHTTP(subPostRec, subPostReq)
	if subPostRec.Code != http.StatusOK {
		t.Fatalf("subscription status = %d body = %s", subPostRec.Code, subPostRec.Body.String())
	}
	var subLaunchResp struct {
		Data struct {
			Mode   string `json:"mode"`
			PayURL string `json:"pay_url"`
		} `json:"data"`
	}
	if err := json.Unmarshal(subPostRec.Body.Bytes(), &subLaunchResp); err != nil {
		t.Fatalf("decode subscription launch: %v", err)
	}
	if stub.subscriptionReq.PlanID != "7" || stub.subscriptionReq.PaymentMethod != "alipay" || !stub.subscriptionReq.IsMobile {
		t.Fatalf("subscription req = %#v", stub.subscriptionReq)
	}
	if subLaunchResp.Data.Mode != "redirect" || subLaunchResp.Data.PayURL != "https://pay.example.com/sub" {
		t.Fatalf("unexpected subscription launch: %#v", subLaunchResp.Data)
	}

	subUsageReq := httptest.NewRequest(http.MethodGet, "/api/channels/1/subscription-usage", nil)
	subUsageRec := httptest.NewRecorder()
	r.ServeHTTP(subUsageRec, subUsageReq)
	if subUsageRec.Code != http.StatusOK {
		t.Fatalf("subscription usage status = %d body = %s", subUsageRec.Code, subUsageRec.Body.String())
	}
	var subUsageResp struct {
		Data connector.SubscriptionUsageInfo `json:"data"`
	}
	if err := json.Unmarshal(subUsageRec.Body.Bytes(), &subUsageResp); err != nil {
		t.Fatalf("decode subscription usage: %v", err)
	}
	if len(subUsageResp.Data.Items) != 1 || subUsageResp.Data.Items[0].Daily == nil || subUsageResp.Data.Items[0].Daily.RemainingPercent != 20 {
		t.Fatalf("unexpected subscription usage: %#v", subUsageResp.Data)
	}
}
