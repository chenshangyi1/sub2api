/**
 * 媒体生成：直接用网关密钥调用同源 /v1/images|videos 透传端点。
 * 与 midstream-ops 的"用用户自己的 Key 调网关"语义一致，管理端在这里做生成验证。
 */

export interface MediaGenerationRequest {
  model: string
  prompt: string
  size?: string
  n?: number
}

export interface MediaGenerationResult {
  /** 生成的图片 URL（上游 CDN 直链）。 */
  url?: string
  /** base64 图片数据（URL 为空时回退）。 */
  b64_json?: string
  revised_prompt?: string
}

export async function generateImage(
  key: string,
  input: MediaGenerationRequest,
): Promise<{ data?: MediaGenerationResult[]; error?: { message?: string } }> {
  const res = await fetch("/v1/images/generations", {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Authorization: `Bearer ${key}`,
    },
    body: JSON.stringify({
      model: input.model,
      prompt: input.prompt,
      size: input.size ?? "1024x1024",
      n: input.n ?? 1,
    }),
  })
  return parseJson(res)
}

export async function generateVideo(
  key: string,
  input: MediaGenerationRequest,
): Promise<{ request_id?: string; error?: { message?: string } }> {
  const res = await fetch("/v1/videos/generations", {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Authorization: `Bearer ${key}`,
    },
    body: JSON.stringify({
      model: input.model,
      prompt: input.prompt,
      resolution: input.size ?? "720p",
    }),
  })
  return parseJson(res)
}

export async function queryVideo(key: string, requestID: string): Promise<Record<string, unknown>> {
  const res = await fetch(`/v1/videos/${encodeURIComponent(requestID)}`, {
    headers: { Authorization: `Bearer ${key}` },
  })
  return parseJson(res)
}

async function parseJson(res: Response): Promise<any> {
  const text = await res.text()
  let body: unknown = null
  if (text) {
    try {
      body = JSON.parse(text)
    } catch {
      body = text
    }
  }
  if (!res.ok) {
    return { error: { message: typeof body === "string" ? body : JSON.stringify(body) } }
  }
  return body
}
