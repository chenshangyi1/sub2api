import assert from "node:assert/strict";
import test from "node:test";

import { isAccountBalanceExhausted, shouldProbeSub2ApiUsage, supportsSub2ApiUsage } from "@/server/account-balance";

test("zero remaining balance is treated as exhausted while unavailable measurements are not", () => {
  assert.equal(isAccountBalanceExhausted({ status: "ok", remaining: 0 }), true);
  assert.equal(isAccountBalanceExhausted({ status: "ok", remaining: -0.01 }), true);
  assert.equal(isAccountBalanceExhausted({ status: "ok", remaining: 0.01 }), false);
  assert.equal(isAccountBalanceExhausted({ status: "unsupported", remaining: 0 }), false);
  assert.equal(isAccountBalanceExhausted({ status: "ok", remaining: null }), false);
});

test("passive usage only probes Anthropic OAuth and setup token accounts", () => {
  assert.equal(supportsSub2ApiUsage({ platform: "anthropic", type: "oauth" }, false), true);
  assert.equal(supportsSub2ApiUsage({ platform: "anthropic", type: "setup-token" }, false), true);
  assert.equal(supportsSub2ApiUsage({ platform: "openai", type: "oauth" }, false), false);
  assert.equal(supportsSub2ApiUsage({ platform: "openai", type: "apikey" }, false), false);
});

test("active usage only probes account families supported by Sub2API", () => {
  assert.equal(supportsSub2ApiUsage({ platform: "openai", type: "oauth" }, true), true);
  assert.equal(supportsSub2ApiUsage({ platform: "openai", type: "apikey" }, true), false);
  assert.equal(supportsSub2ApiUsage({ platform: "gemini", type: "apikey" }, true), true);
  assert.equal(supportsSub2ApiUsage({ platform: "antigravity", type: "oauth" }, true), true);
  assert.equal(supportsSub2ApiUsage({ platform: "grok", type: "oauth" }, true), true);
  assert.equal(supportsSub2ApiUsage(undefined, true), false);
});

test("usage probing remains available when account export fails", () => {
  assert.equal(shouldProbeSub2ApiUsage(undefined, false, "export unavailable"), true);
  assert.equal(shouldProbeSub2ApiUsage(undefined, true, "export unavailable"), true);
  assert.equal(shouldProbeSub2ApiUsage(undefined, true), false);
  assert.equal(shouldProbeSub2ApiUsage({ platform: "openai", type: "apikey" }, true, "stale issue"), false);
});
