import { useState } from "react"
import { ImagePlus, KeyRound, Loader2, Play, RotateCcw } from "lucide-react"
import { toast } from "sonner"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Textarea } from "@/components/ui/textarea"
import { OperationsPageHeader } from "@/components/operations/operations-layout"
import { generateImage, generateVideo, queryVideo, type MediaGenerationResult } from "@/lib/media-api"

const imageModels = ["grok-2-image", "gpt-image-1", "dall-e-3", "flux-dev"] as const
const videoModels = ["grok-imagine-video", "veo-3"] as const
const imageSizes = ["1024x1024", "1024x1536", "1536x1024", "2048x2048"] as const

export default function MediaPage() {
  const [mode, setMode] = useState<"image" | "video">("image")
  const [key, setKey] = useState("")
  const [model, setModel] = useState<string>("grok-2-image")
  const [prompt, setPrompt] = useState("")
  const [size, setSize] = useState("1024x1024")
  const [busy, setBusy] = useState(false)
  const [images, setImages] = useState<MediaGenerationResult[]>([])
  const [videoRequestID, setVideoRequestID] = useState("")
  const [videoStatus, setVideoStatus] = useState<Record<string, unknown> | null>(null)

  function switchMode(next: "image" | "video") {
    setMode(next)
    setModel(next === "image" ? "grok-2-image" : "grok-imagine-video")
    setSize(next === "image" ? "1024x1024" : "720p")
    setImages([])
    setVideoRequestID("")
    setVideoStatus(null)
  }

  async function handleGenerate() {
    if (!key.trim()) {
      toast.error("请先粘贴网关 API Key")
      return
    }
    if (!prompt.trim()) {
      toast.error("请填写提示词")
      return
    }
    setBusy(true)
    try {
      if (mode === "image") {
        const result = await generateImage(key.trim(), { model, prompt, size })
        if (result.error) {
          toast.error(result.error.message || "生成失败")
        } else {
          setImages(result.data ?? [])
        }
      } else {
        const result = await generateVideo(key.trim(), { model, prompt, size })
        if (result.error) {
          toast.error(result.error.message || "生成失败")
        } else if (result.request_id) {
          setVideoRequestID(result.request_id)
          toast.success("视频任务已提交，可轮询查询结果")
        } else {
          toast.success("已提交，请检查返回内容")
        }
      }
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "请求失败")
    } finally {
      setBusy(false)
    }
  }

  async function handlePoll() {
    if (!key.trim() || !videoRequestID) return
    setBusy(true)
    try {
      const result = await queryVideo(key.trim(), videoRequestID)
      setVideoStatus(result)
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "查询失败")
    } finally {
      setBusy(false)
    }
  }

  return (
    <section className="space-y-5">
      <OperationsPageHeader
        icon={ImagePlus}
        title="媒体生成"
        description="用网关密钥直接调用上游生图 / 生视频接口，用于验证模型可用性与生成效果。"
        actions={
          <div className="flex rounded-md border border-border bg-background p-0.5">
            {(["image", "video"] as const).map((item) => (
              <Button
                key={item}
                variant="ghost"
                size="sm"
                className={mode === item ? "bg-muted" : ""}
                onClick={() => switchMode(item)}
              >
                {item === "image" ? "生图" : "生视频"}
              </Button>
            ))}
          </div>
        }
      />

      <div className="grid gap-5 lg:grid-cols-[minmax(0,24rem)_minmax(0,1fr)]">
        <div className="space-y-4 rounded-md border border-border p-4">
          <div className="space-y-1.5">
            <Label className="flex items-center gap-1.5">
              <KeyRound className="size-3.5" />
              网关 API Key
            </Label>
            <Input
              type="password"
              value={key}
              onChange={(e) => setKey(e.target.value)}
              placeholder="sk-..."
              autoComplete="off"
            />
            <p className="text-[11px] text-muted-foreground">
              在"请求网关 → 密钥"里复制，后端只读，不会保存。
            </p>
          </div>

          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-1.5">
              <Label>模型</Label>
              <Select value={model} onValueChange={setModel}>
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {(mode === "image" ? imageModels : videoModels).map((item) => (
                    <SelectItem key={item} value={item}>
                      {item}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-1.5">
              <Label>{mode === "image" ? "尺寸" : "分辨率"}</Label>
              <Select value={size} onValueChange={setSize}>
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {(mode === "image" ? imageSizes : ["480p", "720p", "1080p"]).map((item) => (
                    <SelectItem key={item} value={item}>
                      {item}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </div>

          <div className="space-y-1.5">
            <Label>提示词</Label>
            <Textarea
              value={prompt}
              onChange={(e) => setPrompt(e.target.value)}
              placeholder={mode === "image" ? "描述你想生成的图片..." : "描述你想生成的视频..."}
              rows={5}
            />
          </div>

          <Button className="w-full gap-1.5" onClick={handleGenerate} disabled={busy}>
            {busy ? <Loader2 className="size-4 animate-spin" /> : <Play className="size-4" />}
            生成
          </Button>

          {mode === "video" && videoRequestID ? (
            <Button variant="outline" className="w-full gap-1.5" onClick={handlePoll} disabled={busy}>
              <RotateCcw className="size-4" />
              轮询任务 {videoRequestID.slice(0, 8)}
            </Button>
          ) : null}
        </div>

        <div className="min-h-64 space-y-4 rounded-md border border-border p-4">
          {mode === "image" ? (
            images.length > 0 ? (
              <div className="grid gap-3 sm:grid-cols-2">
                {images.map((item, index) => (
                  <figure key={index} className="space-y-1.5">
                    <div className="overflow-hidden rounded-md border border-border bg-muted/40">
                      <ImagePreview item={item} />
                    </div>
                    {item.revised_prompt ? (
                      <figcaption className="line-clamp-2 text-xs text-muted-foreground">
                        {item.revised_prompt}
                      </figcaption>
                    ) : null}
                  </figure>
                ))}
              </div>
            ) : (
              <EmptyResult hint="生成结果会显示在这里" />
            )
          ) : videoStatus ? (
            <pre className="max-h-96 overflow-auto rounded-md bg-muted/40 p-3 text-xs">
              {JSON.stringify(videoStatus, null, 2)}
            </pre>
          ) : (
            <EmptyResult hint="视频任务返回 request_id 后可轮询查询状态" />
          )}
        </div>
      </div>
    </section>
  )
}

function ImagePreview({ item }: { item: MediaGenerationResult }) {
  const [failed, setFailed] = useState(false)
  const src = item.url ?? (item.b64_json ? `data:image/png;base64,${item.b64_json}` : "")
  if (!src) {
    return (
      <pre className="max-h-64 overflow-auto p-3 text-xs">
        {JSON.stringify(item, null, 2)}
      </pre>
    )
  }
  if (failed) {
    return (
      <div className="flex h-40 items-center justify-center p-3 text-center text-xs text-muted-foreground">
        <div>
          <p>图片加载失败</p>
          <a
            href={item.url}
            target="_blank"
            rel="noopener noreferrer"
            className="mt-1 inline-block break-all text-foreground underline underline-offset-2"
          >
            {item.url}
          </a>
        </div>
      </div>
    )
  }
  return (
    <img
      src={src}
      alt="生成结果"
      className="mx-auto max-h-96 w-full object-contain"
      onError={() => setFailed(true)}
    />
  )
}

function EmptyResult({ hint }: { hint: string }) {
  return (
    <div className="flex h-56 flex-col items-center justify-center gap-2 text-sm text-muted-foreground">
      <Badge variant="outline" className="font-normal">
        {hint}
      </Badge>
    </div>
  )
}
