import assert from "node:assert/strict";
import test from "node:test";

import {
  credentialProbeDisposition,
  nextMonitorFailureCounts,
  selectMonitorModelCandidates,
  selectMonitorCheckMode,
  shouldPublishExternalMonitorResult,
  shouldRunHeavyMonitorCheck,
  shouldUseCredentialProbe,
} from "@/server/upstream-monitor";

test("layered monitor runs heavy when no previous heavy sample exists", () => {
  assert.equal(shouldRunHeavyMonitorCheck(null, new Date("2026-07-19T00:00:00Z"), 60), true);
});

test("layered monitor keeps frequent checks light until the heavy interval elapses", () => {
  const last = new Date("2026-07-19T00:00:00Z");
  assert.equal(shouldRunHeavyMonitorCheck(last, new Date("2026-07-19T00:59:59Z"), 60), false);
  assert.equal(shouldRunHeavyMonitorCheck(last, new Date("2026-07-19T01:00:00Z"), 60), true);
});

test("layered monitor normalizes invalid heavy intervals", () => {
  const last = new Date("2026-07-19T00:00:00Z");
  assert.equal(shouldRunHeavyMonitorCheck(last, new Date("2026-07-19T00:01:00Z"), 0), true);
});

test("expired pauses require a heavy recovery check", () => {
  const now = new Date("2026-07-19T01:00:00Z");
  assert.equal(selectMonitorCheckMode({
    lastHeavyCheckedAt: new Date("2026-07-19T00:55:00Z"),
    pausedUntil: new Date("2026-07-19T01:00:00Z"),
    now,
    heavyIntervalMinutes: 60,
  }), "heavy");
});

test("active pauses keep interim checks light", () => {
  const now = new Date("2026-07-19T01:00:00Z");
  assert.equal(selectMonitorCheckMode({
    lastHeavyCheckedAt: new Date("2026-07-19T00:55:00Z"),
    pausedUntil: new Date("2026-07-19T01:30:00Z"),
    now,
    heavyIntervalMinutes: 60,
  }), "light");
});

test("light success does not erase heavy failures", () => {
  assert.deepEqual(nextMonitorFailureCounts({
    checkMode: "light",
    success: true,
    lightFailures: 2,
    heavyFailures: 2,
  }), {
    lightFailures: 0,
    heavyFailures: 2,
  });
});

test("heavy failures accumulate independently across light cycles", () => {
  assert.deepEqual(nextMonitorFailureCounts({
    checkMode: "heavy",
    success: false,
    lightFailures: 0,
    heavyFailures: 2,
  }), {
    lightFailures: 0,
    heavyFailures: 3,
  });
});

test("missing balance endpoints fall back to a credential probe", () => {
  assert.equal(shouldUseCredentialProbe({ status: "unsupported" }), true);
  assert.equal(shouldUseCredentialProbe({ status: "error", message: "HTTP 404: not found" }), true);
  assert.equal(shouldUseCredentialProbe({ status: "error", message: "HTTP 401: billing scope unavailable" }), true);
});

test("missing model endpoints stay neutral while auth failures fail", () => {
  assert.equal(credentialProbeDisposition({ status: "fail", httpStatus: 404 }), "neutral");
  assert.equal(credentialProbeDisposition({ status: "fail", httpStatus: 405 }), "neutral");
  assert.equal(credentialProbeDisposition({ status: "fail", httpStatus: 401 }), "fail");
  assert.equal(credentialProbeDisposition({ status: "pass", httpStatus: 200 }), "pass");
});

test("balance exhaustion never publishes a failed channel monitor result", () => {
  assert.equal(shouldPublishExternalMonitorResult(true), false);
  assert.equal(shouldPublishExternalMonitorResult(false), true);
});

test("automatic monitor models filter non-text models and prefer lower-cost tiers", () => {
  assert.deepEqual(selectMonitorModelCandidates([
    { id: "gpt-image-1" },
    { id: "text-embedding-3-large" },
    { id: "gpt-5.4" },
    { id: "claude-sonnet-4-6" },
    { id: "gemini-2.5-flash" },
    { id: "gpt-5.4-mini" },
    { id: "gpt-5.4-nano" },
    { id: "gpt-5.4-mini" },
  ]), ["gpt-5.4-nano", "gpt-5.4-mini", "gemini-2.5-flash"]);
});

test("premium family names are not treated as low-cost models", () => {
  assert.deepEqual(selectMonitorModelCandidates([
    { id: "claude-sonnet-4-6" },
    { id: "vendor-text" },
    { id: "claude-3-5-haiku" },
  ]), ["claude-3-5-haiku", "claude-sonnet-4-6", "vendor-text"]);
});

test("automatic monitor models cap retries and retain unknown account models", () => {
  assert.deepEqual(selectMonitorModelCandidates([
    { id: "vendor-custom-c" },
    { id: "vendor-custom-a" },
    { id: "vendor-custom-b" },
    { id: "vendor-custom-d" },
  ]), ["vendor-custom-c", "vendor-custom-a", "vendor-custom-b"]);
});
