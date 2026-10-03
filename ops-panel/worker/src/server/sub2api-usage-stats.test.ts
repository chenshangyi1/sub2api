import assert from "node:assert/strict";
import test from "node:test";

import {
	accountUpstreamCostSql,
	calculateAccountUpstreamCost,
	calculateCacheHitRate,
	calculateProfitBreakdown,
	completeUsageHeatmap,
} from "@/server/sub2api-usage-stats";

test("profit stats use the per-request total cost and account rate", () => {
	assert.equal(calculateAccountUpstreamCost({
		totalCost: 80,
		accountRateMultiplier: 0.05,
	}), 4);
	assert.match(accountUpstreamCostSql, /total_cost/);
	assert.doesNotMatch(accountUpstreamCostSql, /account_stats_cost/);
	assert.match(accountUpstreamCostSql, /account_rate_multiplier/);
});

test("profit stats treat a zero account rate as zero upstream cost", () => {
	assert.equal(calculateAccountUpstreamCost({
		totalCost: 80,
		accountRateMultiplier: 0,
	}), 0);
});

test("profit stats fall back to total cost and a default account rate of one", () => {
	assert.equal(calculateAccountUpstreamCost({ totalCost: 8 }), 8);
});

test("cache hit rate uses all prompt tokens as the denominator", () => {
	const rate = calculateCacheHitRate({
		inputTokens: 50_900_000,
		cacheCreationTokens: 214_880,
		cacheReadTokens: 654_010_000,
	});

	assert.ok(rate !== null);
	assert.equal(rate, 654_010_000 / (50_900_000 + 214_880 + 654_010_000));
});

test("cache hit rate is absent when there are no prompt tokens", () => {
	assert.equal(calculateCacheHitRate({
		inputTokens: 0,
		cacheCreationTokens: 0,
		cacheReadTokens: 0,
	}), null);
});

test("profit subtracts total account cost while administrator cost remains informational", () => {
	assert.deepEqual(calculateProfitBreakdown({
		userCost: 12,
		upstreamCost: 6,
		administratorCost: 2,
	}), {
		profit: 6,
		profitMargin: 6 / 12,
	});
});

test("usage heatmap fills missing hours without inventing usage", () => {
	const cells = completeUsageHeatmap([
		{
			date: "2026-07-18",
			hour: 3,
			requests: 2,
			userCost: 1.5,
			upstreamCost: 0.5,
			profit: 1,
			avgFirstTokenMs: 800,
			p95FirstTokenMs: 900,
			slowRequests: 0,
		},
	], ["2026-07-18"]);

	assert.equal(cells.length, 24);
	assert.equal(cells[3].requests, 2);
	assert.equal(cells[4].requests, 0);
	assert.equal(cells[4].avgFirstTokenMs, null);
});
