package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bejix/upstream-ops/backend/config"
	"github.com/bejix/upstream-ops/backend/storage"
)

func TestNormalizeUserAgentMode(t *testing.T) {
	cases := map[string]string{
		"":            storage.GatewayUserAgentModePassthrough,
		"passthrough": storage.GatewayUserAgentModePassthrough,
		"GROUP":       storage.GatewayUserAgentModeGroup,
		"custom":      storage.GatewayUserAgentModeCustom,
		"other":       storage.GatewayUserAgentModePassthrough,
	}
	for in, want := range cases {
		if got := NormalizeUserAgentMode(in); got != want {
			t.Fatalf("NormalizeUserAgentMode(%q)=%q want %q", in, got, want)
		}
	}
}

func TestResolveRouteUserAgent(t *testing.T) {
	// passthrough always empty override
	if got := ResolveRouteUserAgent("passthrough", "X", "G"); got != "" {
		t.Fatalf("passthrough = %q", got)
	}
	// group uses group UA
	if got := ResolveRouteUserAgent("group", "X", " Group-UA/1 "); got != "Group-UA/1" {
		t.Fatalf("group = %q", got)
	}
	// group empty → no rewrite
	if got := ResolveRouteUserAgent("group", "X", "  "); got != "" {
		t.Fatalf("group empty = %q", got)
	}
	// custom uses custom
	if got := ResolveRouteUserAgent("custom", " Custom-UA/2 ", "G"); got != "Custom-UA/2" {
		t.Fatalf("custom = %q", got)
	}
	// custom empty → no rewrite
	if got := ResolveRouteUserAgent("custom", "", "G"); got != "" {
		t.Fatalf("custom empty = %q", got)
	}
}

func TestWithDefaultUserAgent(t *testing.T) {
	if got := withDefaultUserAgent(" Custom ", "def"); got != "Custom" {
		t.Fatalf("prefer resolved = %q", got)
	}
	if got := withDefaultUserAgent("", " def-ua "); got != "def-ua" {
		t.Fatalf("fallback default = %q", got)
	}
	if got := withDefaultUserAgent("", ""); got != config.DefaultUpstreamUserAgent {
		t.Fatalf("builtin default = %q", got)
	}
}

func TestResolveAdminUserAgent_EmptyFallsBackToDefault(t *testing.T) {
	svc := NewService(nil, nil, nil, nil, nil, nil, nil, nil, nil)
	svc.UpdateUpstreamConfig(config.UpstreamConfig{UserAgent: "settings-ua/1"})

	g := &storage.GatewayGroup{UserAgent: ""}
	r := &storage.GatewayRoute{UserAgentMode: storage.GatewayUserAgentModeGroup}
	if got := svc.resolveAdminUserAgent(g, r); got != "settings-ua/1" {
		t.Fatalf("group empty should use settings default, got %q", got)
	}
	r.UserAgentMode = storage.GatewayUserAgentModePassthrough
	if got := svc.resolveAdminUserAgent(g, r); got != "settings-ua/1" {
		t.Fatalf("passthrough admin should use default, got %q", got)
	}
	r.UserAgentMode = storage.GatewayUserAgentModeCustom
	r.UserAgentCustom = "route-ua"
	if got := svc.resolveAdminUserAgent(g, r); got != "route-ua" {
		t.Fatalf("custom should win, got %q", got)
	}
}

func TestResolveRouteUserAgentFrom(t *testing.T) {
	g := &storage.GatewayGroup{UserAgent: "G-UA"}
	r := &storage.GatewayRoute{
		UserAgentMode:   storage.GatewayUserAgentModeGroup,
		UserAgentCustom: "C-UA",
	}
	if got := ResolveRouteUserAgentFrom(g, r); got != "G-UA" {
		t.Fatalf("from group mode = %q", got)
	}
	r.UserAgentMode = storage.GatewayUserAgentModeCustom
	if got := ResolveRouteUserAgentFrom(g, r); got != "C-UA" {
		t.Fatalf("from custom mode = %q", got)
	}
	if got := ResolveRouteUserAgentFrom(nil, nil); got != "" {
		t.Fatalf("nil = %q", got)
	}
}

func TestBuildUpstreamHTTPRequest_RouteUserAgentOverride(t *testing.T) {
	svc := NewService(nil, nil, nil, nil, nil, nil, nil, nil, nil)
	in := http.Header{}
	in.Set("User-Agent", "OpenAI/JS 6.26.0")
	in.Set("Content-Type", "application/json")

	// 无 override：透传客户端
	req, err := svc.buildUpstreamHTTPRequest(
		context.Background(),
		&upstreamTarget{
			BaseURL:  "https://upstream.example",
			APIKey:   "sk-test",
			Provider: &storage.GatewayProvider{AuthStyle: storage.GatewayProviderAuthBearer},
		},
		"/v1/chat/completions",
		http.MethodPost,
		in,
		[]byte(`{"model":"x"}`),
		protocolOpenAI,
		false,
	)
	if err != nil {
		t.Fatalf("build empty override: %v", err)
	}
	if got := req.Header.Get("User-Agent"); got != "OpenAI/JS 6.26.0" {
		t.Fatalf("passthrough got %q", got)
	}

	// 有 override：覆盖客户端
	req2, err := svc.buildUpstreamHTTPRequest(
		context.Background(),
		&upstreamTarget{
			BaseURL:           "https://upstream.example",
			APIKey:            "sk-test",
			UserAgentOverride: "Mozilla/5.0 (compatible; upstream-ops)",
			Provider:          &storage.GatewayProvider{AuthStyle: storage.GatewayProviderAuthBearer},
		},
		"/v1/chat/completions",
		http.MethodPost,
		in,
		[]byte(`{"model":"x"}`),
		protocolOpenAI,
		false,
	)
	if err != nil {
		t.Fatalf("build override: %v", err)
	}
	if got := req2.Header.Get("User-Agent"); got != "Mozilla/5.0 (compatible; upstream-ops)" {
		t.Fatalf("override = %q", got)
	}

	// 监控渠道同样支持 override
	req3, err := svc.buildUpstreamHTTPRequest(
		context.Background(),
		&upstreamTarget{
			BaseURL:           "https://monitor.example",
			APIKey:            "sk-m",
			UserAgentOverride: "Group-UA/1",
		},
		"/v1/chat/completions",
		http.MethodPost,
		in,
		[]byte(`{"model":"x"}`),
		protocolOpenAI,
		false,
	)
	if err != nil {
		t.Fatalf("build monitor: %v", err)
	}
	if got := req3.Header.Get("User-Agent"); got != "Group-UA/1" {
		t.Fatalf("monitor override = %q", got)
	}
}

func TestFetchUpstreamModels_AppliesUserAgent(t *testing.T) {
	var gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"m1"}]}`))
	}))
	defer srv.Close()

	svc := NewService(nil, nil, nil, nil, nil, nil, nil, nil, nil)
	ch := &storage.Channel{SiteURL: srv.URL}
	models, err := svc.fetchUpstreamModels(context.Background(), ch, "sk", "Test-UA/models")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(models) != 1 || models[0] != "m1" {
		t.Fatalf("models=%v", models)
	}
	if gotUA != "Test-UA/models" {
		t.Fatalf("User-Agent=%q", gotUA)
	}

	// empty UA: 回落默认 upstream UA
	svc.UpdateUpstreamConfig(config.UpstreamConfig{UserAgent: "default-models-ua"})
	gotUA = ""
	models, err = svc.fetchUpstreamModels(context.Background(), ch, "sk", "")
	if err != nil {
		t.Fatalf("fetch empty: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("models empty path: %v", models)
	}
	if gotUA != "default-models-ua" {
		t.Fatalf("empty UA should use default, got %q", gotUA)
	}
}

func TestSaveRoutes_UserAgentFields(t *testing.T) {
	db := openGatewayTestDB(t)
	groupsRepo := storage.NewGatewayGroups(db)
	routesRepo := storage.NewGatewayRoutes(db)
	svc := NewService(groupsRepo, storage.NewGatewayKeys(db), routesRepo, nil, nil, nil, nil, nil, nil)

	g := &storage.GatewayGroup{
		Name:              "ua-group",
		Status:            storage.GatewayGroupStatusActive,
		RateSortDirection: "asc",
		UserAgent:         "Group-Default-UA",
	}
	if err := groupsRepo.Create(g); err != nil {
		t.Fatalf("create group: %v", err)
	}

	// need a channel for monitor route - create minimal channel table row via raw insert through storage
	ch := &storage.Channel{
		Name:           "ch-ua",
		Type:           storage.ChannelTypeNewAPI,
		SiteURL:        "https://example.com",
		Username:       "u",
		PasswordCipher: "x",
		CredentialMode: storage.CredentialModePassword,
	}
	if err := db.Create(ch).Error; err != nil {
		t.Fatalf("create channel: %v", err)
	}

	saved, err := svc.SaveRoutes(g.ID, []RouteInput{{
		SourceKind:      storage.GatewayRouteSourceMonitor,
		SourceChannelID: ch.ID,
		Weight:          1,
		Enabled:         true,
		UserAgentMode:   storage.GatewayUserAgentModeCustom,
		UserAgentCustom: " Route-Custom ",
	}})
	if err != nil {
		t.Fatalf("SaveRoutes: %v", err)
	}
	if len(saved) != 1 {
		t.Fatalf("len=%d", len(saved))
	}
	if saved[0].UserAgentMode != storage.GatewayUserAgentModeCustom {
		t.Fatalf("mode=%q", saved[0].UserAgentMode)
	}
	if saved[0].UserAgentCustom != "Route-Custom" {
		t.Fatalf("custom=%q", saved[0].UserAgentCustom)
	}
	// non-custom should clear custom
	saved2, err := svc.SaveRoutes(g.ID, []RouteInput{{
		ID:              saved[0].ID,
		SourceKind:      storage.GatewayRouteSourceMonitor,
		SourceChannelID: ch.ID,
		Weight:          1,
		Enabled:         true,
		UserAgentMode:   storage.GatewayUserAgentModeGroup,
		UserAgentCustom: "should-clear",
	}})
	if err != nil {
		t.Fatalf("SaveRoutes2: %v", err)
	}
	if saved2[0].UserAgentMode != storage.GatewayUserAgentModeGroup {
		t.Fatalf("mode2=%q", saved2[0].UserAgentMode)
	}
	if saved2[0].UserAgentCustom != "" {
		t.Fatalf("custom should clear, got %q", saved2[0].UserAgentCustom)
	}
}

func TestCreateUpdateGroup_UserAgent(t *testing.T) {
	db := openGatewayTestDB(t)
	groupsRepo := storage.NewGatewayGroups(db)
	svc := NewService(groupsRepo, storage.NewGatewayKeys(db), storage.NewGatewayRoutes(db), nil, nil, nil, nil, nil, nil)

	g, err := svc.CreateGroup(CreateGroupInput{
		Name:      "g-ua",
		UserAgent: "  Group-UA/1  ",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if g.UserAgent != "Group-UA/1" {
		t.Fatalf("create user_agent=%q", g.UserAgent)
	}
	cleared := ""
	g2, err := svc.UpdateGroup(g.ID, UpdateGroupInput{UserAgent: &cleared})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if g2.UserAgent != "" {
		t.Fatalf("clear user_agent got %q", g2.UserAgent)
	}
}
