package sub2api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAdminClientUsesAPIKeyAndDecodesAccountWrites(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/groups/all", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "admin-key" {
			t.Fatalf("x-api-key = %q", r.Header.Get("x-api-key"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": []map[string]any{{"id": 1, "name": "g1", "rate_multiplier": 0.06, "status": "active"}},
		})
	})
	mux.HandleFunc("/api/v1/admin/proxies/all", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "admin-key" {
			t.Fatalf("x-api-key = %q", r.Header.Get("x-api-key"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": []map[string]any{{"id": 2, "name": "p1", "protocol": "socks5", "host": "127.0.0.1", "port": 1080, "status": "active"}},
		})
	})
	mux.HandleFunc("/api/v1/admin/accounts", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "admin-key" {
			t.Fatalf("x-api-key = %q", r.Header.Get("x-api-key"))
		}
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 0,
				"data": map[string]any{"items": []map[string]any{{"id": 7, "name": "a1"}}},
			})
		case http.MethodPost:
			var body AdminAccount
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode create body: %v", err)
			}
			if body.Credentials["api_key"] != "sk-test" || len(body.GroupIDs) != 1 || body.GroupIDs[0] != 1 {
				t.Fatalf("create body = %#v", body)
			}
			body.ID = 8
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": body})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/api/v1/admin/accounts/8", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "admin-key" {
			t.Fatalf("x-api-key = %q", r.Header.Get("x-api-key"))
		}
		switch r.Method {
		case http.MethodPut:
			var body AdminAccount
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode update body: %v", err)
			}
			if body.Status != "inactive" {
				t.Fatalf("update status = %q, want inactive", body.Status)
			}
			body.ID = 8
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": body})
		case http.MethodDelete:
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{}})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/api/v1/admin/accounts/8/schedulable", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "admin-key" {
			t.Fatalf("x-api-key = %q", r.Header.Get("x-api-key"))
		}
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var body struct {
			Schedulable bool `json:"schedulable"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode schedulable body: %v", err)
		}
		if body.Schedulable {
			t.Fatalf("schedulable = true, want false")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{"id": 8, "name": "updated", "status": "inactive", "schedulable": false},
		})
	})
	mux.HandleFunc("/api/v1/admin/accounts/8/models/sync-upstream", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "admin-key" {
			t.Fatalf("x-api-key = %q", r.Header.Get("x-api-key"))
		}
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{"models": []map[string]any{{"id": "gpt-a"}, {"id": "gpt-b"}}},
		})
	})
	mux.HandleFunc("/api/v1/admin/accounts/8/models", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "admin-key" {
			t.Fatalf("x-api-key = %q", r.Header.Get("x-api-key"))
		}
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": []map[string]any{{"id": "gpt-a"}, {"id": "gpt-b"}},
		})
	})
	mux.HandleFunc("/api/v1/admin/accounts/8/test", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "admin-key" {
			t.Fatalf("x-api-key = %q", r.Header.Get("x-api-key"))
		}
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var body struct {
			ModelID string `json:"model_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode test body: %v", err)
		}
		if body.ModelID != "gpt-a" {
			t.Fatalf("model_id = %q, want gpt-a", body.ModelID)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"test_start\",\"model\":\"gpt-a\"}\n\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"content\",\"text\":\"pong\"}\n\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"test_complete\",\"success\":true}\n\n"))
	})
	mux.HandleFunc("/api/v1/admin/groups/1", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "admin-key" {
			t.Fatalf("x-api-key = %q", r.Header.Get("x-api-key"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{}})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := NewAdminClient()
	target := AdminTarget{BaseURL: srv.URL, APIKey: "admin-key"}
	groups, err := client.ListGroups(context.Background(), target, true)
	if err != nil {
		t.Fatalf("ListGroups: %v", err)
	}
	if len(groups) != 1 || groups[0].ID != 1 || groups[0].Ratio != 0.06 {
		t.Fatalf("groups = %#v", groups)
	}
	proxies, err := client.ListProxies(context.Background(), target)
	if err != nil {
		t.Fatalf("ListProxies: %v", err)
	}
	if len(proxies) != 1 || proxies[0].ID != 2 || proxies[0].Name != "p1" {
		t.Fatalf("proxies = %#v", proxies)
	}
	account, err := client.CreateAccount(context.Background(), target, AdminAccount{
		Name:        "a1",
		Type:        "apikey",
		GroupIDs:    []int64{1},
		Credentials: map[string]any{"api_key": "sk-test"},
	})
	if err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	if account.ID != 8 {
		t.Fatalf("created account = %#v", account)
	}
	account, err = client.UpdateAccount(context.Background(), target, 8, AdminAccount{Name: "updated", Status: "disabled"})
	if err != nil {
		t.Fatalf("UpdateAccount: %v", err)
	}
	if account.Name != "updated" || account.Status != "inactive" {
		t.Fatalf("updated account = %#v", account)
	}
	account, err = client.SetAccountSchedulable(context.Background(), target, 8, false)
	if err != nil {
		t.Fatalf("SetAccountSchedulable: %v", err)
	}
	if account.ID != 8 || account.Schedulable {
		t.Fatalf("schedulable account = %#v", account)
	}
	models, err := client.SyncAccountModelsFromUpstream(context.Background(), target, 8)
	if err != nil {
		t.Fatalf("SyncAccountModelsFromUpstream: %v", err)
	}
	if len(models) != 2 || models[0] != "gpt-a" || models[1] != "gpt-b" {
		t.Fatalf("models = %#v", models)
	}
	models, err = client.ListAccountModels(context.Background(), target, 8)
	if err != nil {
		t.Fatalf("ListAccountModels: %v", err)
	}
	if len(models) != 2 || models[0] != "gpt-a" || models[1] != "gpt-b" {
		t.Fatalf("account models = %#v", models)
	}
	testResult, err := client.TestAccount(context.Background(), target, 8, models[0])
	if err != nil {
		t.Fatalf("TestAccount: %v", err)
	}
	if testResult.Model != "gpt-a" || testResult.ResponseText != "pong" {
		t.Fatalf("test result = %#v", testResult)
	}
	if err := client.DeleteAccount(context.Background(), target, 8); err != nil {
		t.Fatalf("DeleteAccount: %v", err)
	}
	if err := client.DeleteGroup(context.Background(), target, 1); err != nil {
		t.Fatalf("DeleteGroup: %v", err)
	}
}
