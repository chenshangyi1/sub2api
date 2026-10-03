package captcha

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bejix/upstream-ops/backend/storage"
)

func TestFetchBalance(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`{"errorId":0,"balance":12.3456}`))
	}))
	t.Cleanup(srv.Close)

	res, err := FetchBalance(context.Background(), &storage.CaptchaConfig{
		Type:     storage.CaptchaTwoCaptcha,
		Endpoint: srv.URL,
	}, "key")
	if err != nil {
		t.Fatalf("fetch balance: %v", err)
	}
	if gotPath != "/getBalance" {
		t.Fatalf("path = %q, want /getBalance", gotPath)
	}
	if res.Balance != 12.3456 || res.Unit != "usd" {
		t.Fatalf("result = %#v", res)
	}
}

func TestFetchBalanceYesCaptchaUnit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"errorId":0,"balance":1071810}`))
	}))
	t.Cleanup(srv.Close)

	res, err := FetchBalance(context.Background(), &storage.CaptchaConfig{
		Type:     storage.CaptchaYesCaptcha,
		Endpoint: srv.URL,
	}, "key")
	if err != nil {
		t.Fatalf("fetch balance: %v", err)
	}
	if res.Balance != 1071810 || res.Unit != "points" {
		t.Fatalf("result = %#v", res)
	}
}

func TestFetchBalanceReturnsProviderError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"errorId":1,"errorCode":"ERROR_KEY_DOES_NOT_EXIST","errorDescription":"bad key"}`))
	}))
	t.Cleanup(srv.Close)

	_, err := FetchBalance(context.Background(), &storage.CaptchaConfig{
		Type:     storage.CaptchaAntiCaptcha,
		Endpoint: srv.URL,
	}, "key")
	if err == nil {
		t.Fatal("expected error")
	}
}
