import assert from "node:assert/strict";
import test from "node:test";
import { planWorkerRun } from "./worker-run-plan";

test("scheduled and requested cycle modes run the complete worker cycle", () => {
  for (const mode of [undefined, null, "", "cycle", "unknown"]) {
    assert.deepEqual(planWorkerRun(mode), {
      cleanupLogs: true,
      balanceAlerts: true,
      upstreamMonitors: true,
      ratePolicies: true,
      accountPoolPolicies: true,
    });
  }
});

test("probe requests run only upstream monitors", () => {
  assert.deepEqual(planWorkerRun("probe:light"), {
    cleanupLogs: false,
    balanceAlerts: false,
    upstreamMonitors: true,
    ratePolicies: false,
    accountPoolPolicies: false,
    forceCheckMode: "light",
  });
  assert.deepEqual(planWorkerRun("probe:heavy"), {
    cleanupLogs: false,
    balanceAlerts: false,
    upstreamMonitors: true,
    ratePolicies: false,
    accountPoolPolicies: false,
    forceCheckMode: "heavy",
  });
  assert.deepEqual(planWorkerRun("probe:custom"), {
    cleanupLogs: false,
    balanceAlerts: false,
    upstreamMonitors: true,
    ratePolicies: false,
    accountPoolPolicies: false,
    forceCheckMode: undefined,
  });
});

test("automation requests run only account pool policies", () => {
  assert.deepEqual(planWorkerRun("automation"), {
    cleanupLogs: false,
    balanceAlerts: false,
    upstreamMonitors: false,
    ratePolicies: false,
    accountPoolPolicies: true,
  });
});

test("rate rule requests run only extension rate policies", () => {
  assert.deepEqual(planWorkerRun("rate-rules"), {
    cleanupLogs: false,
    balanceAlerts: false,
    upstreamMonitors: false,
    ratePolicies: true,
    accountPoolPolicies: false,
  });
});
