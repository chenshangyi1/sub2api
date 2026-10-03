import assert from "node:assert/strict";
import test from "node:test";

import {
  classifyProbeHttpStatus,
  estimateStreamTokens,
  extractProbeSignal,
  extractStreamTextFromSse,
  runApiCapabilityProbeSuite,
} from "@/server/api-capability-probe";

test("optional capability probes classify contract rejections as unsupported", () => {
  assert.equal(classifyProbeHttpStatus("tool-calling", 400), "unsupported");
  assert.equal(classifyProbeHttpStatus("structured-output", 422), "unsupported");
  assert.equal(classifyProbeHttpStatus("web-search", 404), "unsupported");
  assert.equal(classifyProbeHttpStatus("text-generation", 400), "fail");
  assert.equal(classifyProbeHttpStatus("models", 401), "fail");
});

test("probe payload signals distinguish model, tool, and structured responses", () => {
  assert.equal(extractProbeSignal("openai", "models", { data: [{ id: "gpt-test" }] }), true);
  assert.equal(extractProbeSignal("openai", "tool-calling", {
    choices: [{ message: { tool_calls: [{ function: { name: "probe_status" } }] } }],
  }), true);
  assert.equal(extractProbeSignal("anthropic", "structured-output", {
    content: [{ type: "tool_use", name: "probe_status", input: { ok: true } }],
  }), true);
  assert.equal(extractProbeSignal("gemini", "tool-calling", {
    candidates: [{ content: { parts: [{ functionCall: { name: "probe_status" } }] } }],
  }), true);
  assert.equal(extractProbeSignal("openai", "tool-calling", { choices: [{ message: { content: "ok" } }] }), false);
  assert.equal(extractProbeSignal("openai", "web-search", {}), false);
  assert.equal(extractProbeSignal("openai", "web-search", {
    output: [{ type: "message", content: [{ type: "output_text", text: "ordinary answer" }] }],
  }), false);
  assert.equal(extractProbeSignal("openai", "web-search", {
    output: [{ type: "web_search_call", status: "completed" }],
  }), true);
});

test("Gemini bearer credentials stay in the Authorization header", async () => {
  let requestUrl = "";
  let authorization = "";
  const fetchImpl = (async (input: RequestInfo | URL, init?: RequestInit) => {
    requestUrl = String(input);
    authorization = new Headers(init?.headers).get("authorization") ?? "";
    return new Response(JSON.stringify({ models: [{ name: "models/test" }] }), {
      status: 200,
      headers: { "content-type": "application/json" },
    });
  }) as typeof fetch;

  const suite = await runApiCapabilityProbeSuite({
    target: {
      accountId: 1,
      accountName: "oauth",
      provider: "gemini",
      endpoint: "https://generativelanguage.googleapis.com",
      apiKey: "BEARER_TOKEN",
      authScheme: "bearer",
    },
    model: "gemini-test",
    probeIds: ["models"],
    fetchImpl,
  });

  assert.equal(suite.status, "pass");
  assert.equal(requestUrl.includes("BEARER_TOKEN"), false);
  assert.equal(requestUrl.includes("key="), false);
  assert.equal(authorization, "Bearer BEARER_TOKEN");
});

test("stream parser extracts provider text and estimates nonzero token counts", () => {
  const openai = [
    'data: {"choices":[{"delta":{"content":"hello "}}]}',
    'data: {"choices":[{"delta":{"content":"world"}}]}',
    "data: [DONE]",
  ].join("\n\n");
  const anthropic = 'data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"hello"}}\n\n';
  const google = 'data: {"candidates":[{"content":{"parts":[{"text":"你好"}]}}]}\n\n';

  assert.equal(extractStreamTextFromSse("openai", openai), "hello world");
  assert.equal(extractStreamTextFromSse("anthropic", anthropic), "hello");
  assert.equal(extractStreamTextFromSse("gemini", google), "你好");
  assert.ok(estimateStreamTokens("hello world 你好") >= 3);
});
