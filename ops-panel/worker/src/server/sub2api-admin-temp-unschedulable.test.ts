import assert from "node:assert/strict";
import test from "node:test";

import { Sub2ApiAdminClient } from "@/server/clients/sub2api-admin";

test("reason-scoped temporary-state clear sends the matched keyword", async () => {
  const originalFetch = globalThis.fetch;
  let requestedUrl = "";
  globalThis.fetch = async (input) => {
    requestedUrl = String(input);
    return new Response(JSON.stringify({ code: 0, data: { cleared: true } }), { status: 200 });
  };

  try {
    const client = new Sub2ApiAdminClient("http://sub2api:8080", "test-key");
    await client.clearTempUnschedulable(42, "ops_balance_exhausted");
    assert.equal(
      requestedUrl,
      "http://sub2api:8080/api/v1/admin/accounts/42/temp-unschedulable?matched_keyword=ops_balance_exhausted",
    );
  } finally {
    globalThis.fetch = originalFetch;
  }
});
