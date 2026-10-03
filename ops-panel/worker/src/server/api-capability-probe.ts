import type { AccountProbeTarget } from "@/server/sub2api-channel-monitor-sync";

export const apiCapabilityProbeIds = [
  "models",
  "text-generation",
  "tool-calling",
  "structured-output",
  "web-search",
] as const;

export type ApiCapabilityProbeId = (typeof apiCapabilityProbeIds)[number];
export type ApiCapabilityProbeStatus = "pass" | "fail" | "unsupported";

export type ApiCapabilityProbeResult = {
  id: ApiCapabilityProbeId;
  status: ApiCapabilityProbeStatus;
  latencyMs: number;
  summary: string;
  httpStatus?: number;
};

export type ApiCapabilityProbeSuite = {
  provider: AccountProbeTarget["provider"];
  model: string;
  status: "pass" | "partial" | "fail";
  startedAt: string;
  finishedAt: string;
  results: ApiCapabilityProbeResult[];
};

export type StreamPerformanceResult = {
  success: boolean;
  message: string;
  latencyMs: number;
  firstTokenMs: number | null;
  streamTps: number | null;
  tokenCount: number;
};

type FetchLike = typeof fetch;

const optionalProbeIds = new Set<ApiCapabilityProbeId>(["tool-calling", "structured-output", "web-search"]);
const unsupportedStatuses = new Set([400, 404, 405, 415, 422]);

function compact(value: string, max = 180) {
  const normalized = value.replace(/\s+/g, " ").trim();
  return normalized.length > max ? `${normalized.slice(0, max)}...` : normalized;
}

function redact(value: string, secret: string) {
  return compact(secret ? value.split(secret).join("[redacted]") : value);
}

function jsonRecord(value: unknown): Record<string, unknown> | null {
  return value && typeof value === "object" && !Array.isArray(value) ? value as Record<string, unknown> : null;
}

function nested(value: unknown, ...keys: Array<string | number>) {
  let current: unknown = value;
  for (const key of keys) {
    if (Array.isArray(current)) {
      const index = typeof key === "number" ? key : Number(key);
      current = Number.isInteger(index) ? current[index] : undefined;
      continue;
    }
    current = jsonRecord(current)?.[String(key)];
  }
  return current;
}

function textValue(value: unknown) {
  return typeof value === "string" ? value : "";
}

function collectText(value: unknown): string[] {
  if (Array.isArray(value)) return value.flatMap(collectText);
  const record = jsonRecord(value);
  if (!record) return [];
  const direct = [record.text, record.output_text].filter((item): item is string => typeof item === "string");
  return [...direct, ...Object.values(record).flatMap(collectText)];
}

function isOpenAICompatibleProvider(provider: AccountProbeTarget["provider"]) {
  return provider === "openai" || provider === "grok";
}

function responseText(provider: AccountProbeTarget["provider"], payload: unknown) {
  if (isOpenAICompatibleProvider(provider)) {
    return textValue(nested(payload, "choices", 0, "message", "content"))
      || textValue(nested(payload, "output_text"))
      || collectText(nested(payload, "output")).join("");
  }
  if (provider === "anthropic") {
    const content = nested(payload, "content");
    return Array.isArray(content)
      ? content.map((item) => textValue(jsonRecord(item)?.text)).join("")
      : "";
  }
  const parts = nested(payload, "candidates");
  if (!Array.isArray(parts)) return "";
  return parts.flatMap((candidate) => {
    const candidateParts = nested(candidate, "content", "parts");
    return Array.isArray(candidateParts) ? candidateParts.map((part) => textValue(jsonRecord(part)?.text)) : [];
  }).join("");
}

function containsNamedFunction(value: unknown, name: string): boolean {
  if (Array.isArray(value)) return value.some((item) => containsNamedFunction(item, name));
  const record = jsonRecord(value);
  if (!record) return false;
  if (record.name === name) return true;
  return Object.values(record).some((item) => containsNamedFunction(item, name));
}

function containsWebSearchSignal(value: unknown): boolean {
  if (Array.isArray(value)) return value.some(containsWebSearchSignal);
  const record = jsonRecord(value);
  if (!record) return false;
  const type = textValue(record.type).toLowerCase();
  const name = textValue(record.name).toLowerCase();
  if (type.includes("web_search") || name === "web_search" || record.groundingMetadata) return true;
  return Object.values(record).some(containsWebSearchSignal);
}

function containsToolSignal(provider: AccountProbeTarget["provider"], payload: unknown) {
  if (isOpenAICompatibleProvider(provider)) {
    const toolCalls = nested(payload, "choices");
    return Array.isArray(toolCalls) && toolCalls.some((choice) => {
      const calls = nested(choice, "message", "tool_calls");
      return Array.isArray(calls) && calls.length > 0 && containsNamedFunction(calls, "probe_status");
    });
  }
  if (provider === "anthropic") {
    const content = nested(payload, "content");
    return Array.isArray(content) && content.some((item) => {
      const record = jsonRecord(item);
      return record?.type === "tool_use" && record.name === "probe_status";
    });
  }
  return containsNamedFunction(payload, "probe_status");
}

function hasModels(payload: unknown) {
  const data = jsonRecord(payload)?.data;
  const models = jsonRecord(payload)?.models;
  return (Array.isArray(data) && data.length > 0) || (Array.isArray(models) && models.length > 0);
}

function isJsonText(value: string) {
  if (!value.trim()) return false;
  try {
    return Boolean(jsonRecord(JSON.parse(value)));
  } catch {
    return false;
  }
}

export function classifyProbeHttpStatus(id: ApiCapabilityProbeId, status: number): ApiCapabilityProbeStatus {
  if (status >= 200 && status < 300) return "pass";
  if (optionalProbeIds.has(id) && unsupportedStatuses.has(status)) return "unsupported";
  return "fail";
}

export function extractProbeSignal(
  provider: AccountProbeTarget["provider"],
  id: ApiCapabilityProbeId,
  payload: unknown,
) {
  if (id === "models") return hasModels(payload);
  if (id === "tool-calling") return containsToolSignal(provider, payload);
  if (id === "structured-output") {
    return containsToolSignal(provider, payload) || isJsonText(responseText(provider, payload));
  }
  if (id === "web-search") return containsWebSearchSignal(payload);
  return Boolean(responseText(provider, payload));
}

function providerHeaders(target: AccountProbeTarget) {
  const headers: Record<string, string> = {
    Accept: "application/json",
    "Content-Type": "application/json",
  };
  if (target.provider === "anthropic" && target.authScheme === "api-key") {
    headers["x-api-key"] = target.apiKey;
    headers["anthropic-version"] = "2023-06-01";
  } else if (target.provider !== "gemini" || target.authScheme === "bearer") {
    headers.Authorization = `Bearer ${target.apiKey}`;
    if (target.provider === "anthropic") headers["anthropic-version"] = "2023-06-01";
  }
  return headers;
}

function googleUrl(target: AccountProbeTarget, path: string) {
  if (target.authScheme === "bearer") return `${target.endpoint}${path}`;
  const separator = path.includes("?") ? "&" : "?";
  return `${target.endpoint}${path}${separator}key=${encodeURIComponent(target.apiKey)}`;
}

function toolDefinition(provider: AccountProbeTarget["provider"]) {
  const schema = {
    type: "object",
    properties: { ok: { type: "boolean" } },
    required: ["ok"],
    additionalProperties: false,
  };
  if (provider === "anthropic") return { name: "probe_status", description: "Return probe status", input_schema: schema };
  if (provider === "gemini") return { name: "probe_status", description: "Return probe status", parameters: schema };
  return { type: "function", function: { name: "probe_status", description: "Return probe status", parameters: schema, strict: true } };
}

function buildProbeRequest(target: AccountProbeTarget, id: ApiCapabilityProbeId, model: string): { url: string; init: RequestInit } {
  const headers = providerHeaders(target);
  if (isOpenAICompatibleProvider(target.provider)) {
    if (id === "models") return { url: `${target.endpoint}/v1/models`, init: { method: "GET", headers } };
    if (id === "web-search") {
      return {
        url: `${target.endpoint}/v1/responses`,
        init: {
          method: "POST",
          headers,
          body: JSON.stringify({ model, input: "Find the current UTC date and answer briefly.", tools: [{ type: "web_search_preview" }], max_output_tokens: 64 }),
        },
      };
    }
    const body: Record<string, unknown> = {
      model,
      messages: [{ role: "user", content: id === "structured-output" ? "Return JSON with ok=true." : "Reply with ok." }],
      max_tokens: 64,
      stream: false,
    };
    if (id === "tool-calling") {
      body.tools = [toolDefinition("openai")];
      body.tool_choice = { type: "function", function: { name: "probe_status" } };
    }
    if (id === "structured-output") {
      body.response_format = {
        type: "json_schema",
        json_schema: { name: "probe_status", strict: true, schema: { type: "object", properties: { ok: { type: "boolean" } }, required: ["ok"], additionalProperties: false } },
      };
    }
    return { url: `${target.endpoint}/v1/chat/completions`, init: { method: "POST", headers, body: JSON.stringify(body) } };
  }

  if (target.provider === "anthropic") {
    if (id === "models") return { url: `${target.endpoint}/v1/models`, init: { method: "GET", headers } };
    const body: Record<string, unknown> = {
      model,
      max_tokens: 64,
      messages: [{ role: "user", content: id === "structured-output" ? "Return structured status." : "Reply with ok." }],
    };
    if (id === "tool-calling" || id === "structured-output") {
      body.tools = [toolDefinition("anthropic")];
      body.tool_choice = { type: "tool", name: "probe_status" };
    }
    if (id === "web-search") body.tools = [{ type: "web_search_20250305", name: "web_search", max_uses: 1 }];
    return { url: `${target.endpoint}/v1/messages`, init: { method: "POST", headers, body: JSON.stringify(body) } };
  }

  if (id === "models") return { url: googleUrl(target, "/v1beta/models"), init: { method: "GET", headers } };
  const body: Record<string, unknown> = {
    contents: [{ role: "user", parts: [{ text: id === "structured-output" ? "Return JSON with ok=true." : "Reply with ok." }] }],
    generationConfig: { maxOutputTokens: 64 },
  };
  if (id === "tool-calling") {
    body.tools = [{ functionDeclarations: [toolDefinition("gemini")] }];
    body.toolConfig = { functionCallingConfig: { mode: "ANY", allowedFunctionNames: ["probe_status"] } };
  }
  if (id === "structured-output") {
    body.generationConfig = {
      maxOutputTokens: 64,
      responseMimeType: "application/json",
      responseSchema: { type: "OBJECT", properties: { ok: { type: "BOOLEAN" } }, required: ["ok"] },
    };
  }
  if (id === "web-search") body.tools = [{ googleSearch: {} }];
  return {
    url: googleUrl(target, `/v1beta/models/${encodeURIComponent(model)}:generateContent`),
    init: { method: "POST", headers, body: JSON.stringify(body) },
  };
}

async function readProbeResponse(response: Response) {
  const text = await response.text();
  if (!text.trim()) return { payload: null, text: "" };
  try {
    return { payload: JSON.parse(text) as unknown, text };
  } catch {
    return { payload: null, text };
  }
}

async function runProbe(
  target: AccountProbeTarget,
  id: ApiCapabilityProbeId,
  model: string,
  fetchImpl: FetchLike,
  timeoutMs: number,
): Promise<ApiCapabilityProbeResult> {
  const startedAt = Date.now();
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), timeoutMs);
  try {
    const request = buildProbeRequest(target, id, model);
    const response = await fetchImpl(request.url, { ...request.init, signal: controller.signal });
    const latencyMs = Date.now() - startedAt;
    const body = await readProbeResponse(response);
    const status = classifyProbeHttpStatus(id, response.status);
    if (status !== "pass") {
      return {
        id,
        status,
        latencyMs,
        httpStatus: response.status,
        summary: status === "unsupported" ? `接口不支持该能力（HTTP ${response.status}）` : `探测失败（HTTP ${response.status}）`,
      };
    }
    const signal = extractProbeSignal(target.provider, id, body.payload);
    return {
      id,
      status: signal ? "pass" : optionalProbeIds.has(id) ? "unsupported" : "fail",
      latencyMs,
      httpStatus: response.status,
      summary: signal ? "验证通过" : `响应成功但没有识别到${id}能力信号`,
    };
  } catch (error) {
    const message = error instanceof Error && error.name === "AbortError"
      ? "请求超时"
      : redact(error instanceof Error ? error.message : String(error), target.apiKey);
    return { id, status: "fail", latencyMs: Date.now() - startedAt, summary: message || "请求失败" };
  } finally {
    clearTimeout(timeout);
  }
}

export async function runApiCapabilityProbeSuite(input: {
  target: AccountProbeTarget;
  model: string;
  probeIds?: ApiCapabilityProbeId[];
  timeoutMs?: number;
  fetchImpl?: FetchLike;
}): Promise<ApiCapabilityProbeSuite> {
  const startedAt = new Date();
  const probeIds = input.probeIds?.length ? input.probeIds : [...apiCapabilityProbeIds];
  const results: ApiCapabilityProbeResult[] = [];
  for (const id of probeIds) {
    results.push(await runProbe(input.target, id, input.model, input.fetchImpl ?? fetch, input.timeoutMs ?? 30_000));
  }
  const status = results.some((result) => result.status === "fail")
    ? "fail"
    : results.some((result) => result.status === "unsupported")
      ? "partial"
      : "pass";
  return {
    provider: input.target.provider,
    model: input.model,
    status,
    startedAt: startedAt.toISOString(),
    finishedAt: new Date().toISOString(),
    results,
  };
}

function parseSseJson(raw: string) {
  const payloads: unknown[] = [];
  for (const line of raw.split(/\r?\n/)) {
    const match = /^data:\s?(.*)$/.exec(line.trim());
    if (!match || !match[1] || match[1] === "[DONE]") continue;
    try {
      payloads.push(JSON.parse(match[1]) as unknown);
    } catch {
      // Ignore incomplete or provider-specific keepalive frames.
    }
  }
  return payloads;
}

export function extractStreamTextFromSse(provider: AccountProbeTarget["provider"], raw: string) {
  return parseSseJson(raw).map((payload) => {
    if (isOpenAICompatibleProvider(provider)) return textValue(nested(payload, "choices", 0, "delta", "content"));
    if (provider === "anthropic") return textValue(nested(payload, "delta", "text"));
    const candidates = nested(payload, "candidates");
    if (!Array.isArray(candidates)) return "";
    return candidates.flatMap((candidate) => {
      const parts = nested(candidate, "content", "parts");
      return Array.isArray(parts) ? parts.map((part) => textValue(jsonRecord(part)?.text)) : [];
    }).join("");
  }).join("");
}

export function estimateStreamTokens(text: string) {
  const cjk = (text.match(/[\u3400-\u9fff\u3040-\u30ff\uac00-\ud7af]/g) ?? []).length;
  const other = Math.max(0, text.length - cjk);
  return cjk + Math.ceil(other / 4);
}

function buildStreamRequest(target: AccountProbeTarget, model: string): { url: string; init: RequestInit } {
  const headers = { ...providerHeaders(target), Accept: "text/event-stream" };
  if (isOpenAICompatibleProvider(target.provider)) {
    return {
      url: `${target.endpoint}/v1/chat/completions`,
      init: {
        method: "POST",
        headers,
        body: JSON.stringify({ model, messages: [{ role: "user", content: "Count from 1 to 20, one number per line." }], max_tokens: 100, stream: true }),
      },
    };
  }
  if (target.provider === "anthropic") {
    return {
      url: `${target.endpoint}/v1/messages`,
      init: {
        method: "POST",
        headers,
        body: JSON.stringify({ model, max_tokens: 100, stream: true, messages: [{ role: "user", content: "Count from 1 to 20, one number per line." }] }),
      },
    };
  }
  return {
    url: googleUrl(target, `/v1beta/models/${encodeURIComponent(model)}:streamGenerateContent?alt=sse`),
    init: {
      method: "POST",
      headers,
      body: JSON.stringify({ contents: [{ role: "user", parts: [{ text: "Count from 1 to 20, one number per line." }] }], generationConfig: { maxOutputTokens: 100 } }),
    },
  };
}

export async function runStreamPerformanceProbe(input: {
  target: AccountProbeTarget;
  model: string;
  timeoutMs?: number;
  fetchImpl?: FetchLike;
}): Promise<StreamPerformanceResult> {
  const startedAt = Date.now();
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), input.timeoutMs ?? 90_000);
  try {
    const request = buildStreamRequest(input.target, input.model);
    const response = await (input.fetchImpl ?? fetch)(request.url, { ...request.init, signal: controller.signal });
    if (!response.ok || !response.body) {
      const body = await response.text().catch(() => "");
      return {
        success: false,
        message: `流式探测失败（HTTP ${response.status}）${body ? `：${redact(body, input.target.apiKey)}` : ""}`,
        latencyMs: Date.now() - startedAt,
        firstTokenMs: null,
        streamTps: null,
        tokenCount: 0,
      };
    }

    const reader = response.body.getReader();
    const decoder = new TextDecoder();
    let raw = "";
    let previousTextLength = 0;
    let firstTokenMs: number | null = null;
    while (true) {
      const { done, value } = await reader.read();
      if (done) break;
      raw += decoder.decode(value, { stream: true });
      const text = extractStreamTextFromSse(input.target.provider, raw);
      if (text.length > previousTextLength && firstTokenMs === null) firstTokenMs = Date.now() - startedAt;
      previousTextLength = text.length;
    }
    raw += decoder.decode();
    const text = extractStreamTextFromSse(input.target.provider, raw);
    const tokenCount = estimateStreamTokens(text);
    const latencyMs = Date.now() - startedAt;
    const generationMs = firstTokenMs === null ? latencyMs : Math.max(1, latencyMs - firstTokenMs);
    const streamTps = tokenCount > 0 ? tokenCount / (generationMs / 1000) : null;
    return {
      success: tokenCount > 0,
      message: tokenCount > 0 ? "流式生成验证通过" : "流式响应没有可识别文本",
      latencyMs,
      firstTokenMs,
      streamTps: streamTps === null ? null : Math.round(streamTps * 10) / 10,
      tokenCount,
    };
  } catch (error) {
    const message = error instanceof Error && error.name === "AbortError"
      ? "流式探测超时"
      : redact(error instanceof Error ? error.message : String(error), input.target.apiKey);
    return { success: false, message, latencyMs: Date.now() - startedAt, firstTokenMs: null, streamTps: null, tokenCount: 0 };
  } finally {
    clearTimeout(timeout);
  }
}
