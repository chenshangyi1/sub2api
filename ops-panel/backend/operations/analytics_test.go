package operations

import (
	"context"
	"math"
	"testing"
	"time"
)

func TestAnalyticsUsesChannelActualCostAndPreservesUserCharge(t *testing.T) {
	db := openOperationsPostgresSchema(t, "ops_analytics")
	statements := []string{
		`CREATE TABLE channels (id BIGINT PRIMARY KEY, today_cost NUMERIC NOT NULL)`,
		`CREATE TABLE cost_snapshots (
			id BIGSERIAL PRIMARY KEY, channel_id BIGINT NOT NULL, today_cost NUMERIC NOT NULL,
			sampled_at TIMESTAMPTZ NOT NULL
		)`,
		`CREATE TABLE users (id BIGINT PRIMARY KEY, username TEXT, email TEXT NOT NULL, role TEXT NOT NULL)`,
		`CREATE TABLE usage_logs (
			id BIGINT PRIMARY KEY, created_at TIMESTAMPTZ NOT NULL, user_id BIGINT NOT NULL,
			account_id BIGINT NOT NULL, model TEXT NOT NULL, stream BOOLEAN NOT NULL,
			duration_ms INTEGER, first_token_ms INTEGER, status_code INTEGER, error TEXT,
			input_tokens BIGINT, output_tokens BIGINT, cache_read_tokens BIGINT, cache_creation_tokens BIGINT,
			actual_cost NUMERIC, total_cost NUMERIC, account_stats_cost NUMERIC, account_rate_multiplier NUMERIC
		)`,
		`INSERT INTO users (id, username, email, role) VALUES
			(1, 'customer', 'customer@example.test', 'user'),
			(2, 'administrator', 'admin@example.test', 'admin')`,
		`INSERT INTO channels (id, today_cost) VALUES (11, 12.5)`,
		`INSERT INTO cost_snapshots (channel_id, today_cost, sampled_at) VALUES
			(11, 8.5, '2026-07-29T02:00:00Z'),
			(11, 11.5, '2026-07-29T03:00:00Z')`,
		`INSERT INTO usage_logs
			(id, created_at, user_id, account_id, model, stream, duration_ms, first_token_ms, status_code, error,
			 input_tokens, output_tokens, cache_read_tokens, cache_creation_tokens,
			 actual_cost, total_cost, account_stats_cost, account_rate_multiplier)
		 VALUES
			(1, '2026-07-29T02:00:00Z', 1, 11, 'model-a', true, 70000, 12000, 500, 'upstream failed',
			 100, 50, 25, 5, 10, 4, 2, 1.5),
			(2, '2026-07-29T03:00:00Z', 2, 12, 'model-b', false, 1000, 100, 200, '',
			 20, 10, 0, 0, 5, 1, 1, 2)`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}

	service := &Service{
		db: db, mainDB: db,
		now: func() time.Time { return time.Date(2026, 7, 29, 4, 0, 0, 0, time.UTC) },
	}
	result, err := service.GetAnalytics(context.Background(), "day")
	if err != nil {
		t.Fatalf("get analytics: %v", err)
	}
	if result.Summary.Requests != 2 || result.Summary.ActiveUsers != 2 || result.Summary.StreamRequests != 1 {
		t.Fatalf("summary counts = %+v", result.Summary)
	}
	if result.Summary.TotalTokens != 210 {
		t.Fatalf("summary total tokens = %d, want 210", result.Summary.TotalTokens)
	}
	assertClose(t, result.Summary.UserCost, 10)
	assertClose(t, result.Summary.UpstreamCost, 12.5)
	assertClose(t, result.Summary.AdministratorCost, 2)
	assertClose(t, result.Summary.Profit, -2.5)
	assertClose(t, result.Daily[0].UserCost, 10)
	assertClose(t, result.Daily[0].UpstreamCost, 12.5)
	assertClose(t, result.Daily[0].Profit, -2.5)
	if len(result.Daily) != 1 || result.Daily[0].Requests != 2 || result.Daily[0].TotalTokens != 210 {
		t.Fatalf("daily analytics = %+v", result.Daily)
	}
	if len(result.Heatmap) != 168 {
		t.Fatalf("heatmap cells = %d, want 168", len(result.Heatmap))
	}
	var heatmapRequests, heatmapFailures int64
	for _, cell := range result.Heatmap {
		heatmapRequests += cell.Requests
		heatmapFailures += cell.Failures
	}
	if heatmapRequests != 1 || heatmapFailures != 1 {
		t.Fatalf("heatmap totals = requests:%d failures:%d", heatmapRequests, heatmapFailures)
	}
	if len(result.SlowRequests) != 1 || result.SlowRequests[0].ID != 1 || result.SlowRequests[0].StatusCode != 500 {
		t.Fatalf("slow requests = %+v", result.SlowRequests)
	}
}

func assertClose(t *testing.T, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("got %v, want %v", got, want)
	}
}
