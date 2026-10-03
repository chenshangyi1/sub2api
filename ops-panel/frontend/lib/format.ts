/**
 * 各种小格式化工具：相对时间、金额、倍率箭头等。
 */

const RELATIVE_THRESHOLDS: Array<{ limit: number; unit: string; divisor: number }> = [
  { limit: 60, unit: "秒前", divisor: 1 },
  { limit: 60 * 60, unit: "分钟前", divisor: 60 },
  { limit: 24 * 60 * 60, unit: "小时前", divisor: 60 * 60 },
  { limit: 30 * 24 * 60 * 60, unit: "天前", divisor: 24 * 60 * 60 },
]

/** 把 ISO 时间转成"X 分钟前"等相对描述。 */
export function relativeTime(iso?: string | null, now: Date = new Date()): string {
  if (!iso) return "—"
  const t = new Date(iso).getTime()
  if (!Number.isFinite(t)) return "—"
  const diff = Math.max(0, Math.floor((now.getTime() - t) / 1000))
  if (diff < 5) return "刚刚"
  for (const r of RELATIVE_THRESHOLDS) {
    if (diff < r.limit) {
      return `${Math.floor(diff / r.divisor)} ${r.unit}`
    }
  }
  return new Date(iso).toLocaleDateString("zh-CN")
}

/** 把 ISO 时间转成简短的"HH:MM"。 */
export function shortTime(iso?: string | null): string {
  if (!iso) return "—"
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return "—"
  return d.toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit" })
}

export function dateTime(iso?: string | null): string {
  if (!iso) return "—"
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return "—"
  return d.toLocaleString("zh-CN", {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  })
}

/** 完整本地时间，如 2026/07/14 01:47:18 */
export function fullDateTime(iso?: string | null): string {
  if (!iso) return "—"
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return "—"
  const y = d.getFullYear()
  const m = String(d.getMonth() + 1).padStart(2, "0")
  const day = String(d.getDate()).padStart(2, "0")
  const h = String(d.getHours()).padStart(2, "0")
  const min = String(d.getMinutes()).padStart(2, "0")
  const s = String(d.getSeconds()).padStart(2, "0")
  return `${y}/${m}/${day} ${h}:${min}:${s}`
}

/** 毫秒 → 可读延迟，如 7.98s / 52.95s / 120ms */
export function formatDurationMS(ms?: number | null): string {
  if (ms == null || !Number.isFinite(ms) || ms < 0) return "—"
  if (ms < 1000) return `${Math.round(ms)}ms`
  return `${(ms / 1000).toFixed(2)}s`
}

/**
 * Token 数量格式化（对齐 sub2api formatTokensK）：
 *   >= 1_000_000 → "3.5M"
 *   >= 1_000     → "1.2K"
 *   其它         → "950"
 */
export function formatTokens(n?: number | null): string {
  if (n == null || !Number.isFinite(n)) return "0"
  const tokens = Math.round(n)
  const abs = Math.abs(tokens)
  if (abs >= 1_000_000) return `${(tokens / 1_000_000).toFixed(1)}M`
  if (abs >= 1_000) return `${(tokens / 1_000).toFixed(1)}K`
  return String(tokens)
}

/**
 * 大数字紧凑格式（对齐 sub2api formatCompactNumber）：K / M / B
 */
export function formatCompactNumber(
  n?: number | null,
  options?: { allowBillions?: boolean },
): string {
  if (n == null || !Number.isFinite(n)) return "0"
  const abs = Math.abs(n)
  const allowBillions = options?.allowBillions !== false
  if (allowBillions && abs >= 1_000_000_000) return `${(n / 1_000_000_000).toFixed(1)}B`
  if (abs >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`
  if (abs >= 1_000) return `${(n / 1_000).toFixed(1)}K`
  return String(Math.round(n))
}

/** 货币格式：$1,234.56。 */
export function money(value: number | null | undefined, opts?: { precise?: boolean }) {
  if (value == null || !Number.isFinite(value)) return "—"
  return (
    "$" +
    value.toLocaleString("en-US", {
      minimumFractionDigits: opts?.precise ? 4 : 2,
      maximumFractionDigits: opts?.precise ? 4 : 2,
    })
  )
}

export function decimal(value: number | null | undefined, digits = 2) {
  if (value == null || !Number.isFinite(value)) return "—"
  return value.toLocaleString("en-US", {
    minimumFractionDigits: 0,
    maximumFractionDigits: digits,
  })
}

export function formatRatio(value: number | null | undefined) {
  if (value == null || !Number.isFinite(value)) return "—"
  return value.toLocaleString("en-US", {
    minimumFractionDigits: 2,
    maximumFractionDigits: 6,
  })
}

/** 把倍率渲染成"1.20 → 1.50"。 */
export function ratioArrow(from: number | null | undefined, to: number) {
  return `${formatRatio(from)} → ${formatRatio(to)}`
}

/** 计算变化方向 / 百分比文案，比如 "+25.0%"。 */
export function ratioDelta(from: number | null | undefined, to: number) {
  if (from == null || from === 0) {
    return { direction: "up" as const, pct: "新增" }
  }
  const pct = ((to - from) / Math.abs(from)) * 100
  const direction = pct >= 0 ? ("up" as const) : ("down" as const)
  return { direction, pct: `${pct >= 0 ? "+" : ""}${pct.toFixed(1)}%` }
}

/** 把 ChannelType "newapi"/"sub2api" 转成显示名 / 角标颜色。 */
export function channelTypeLabel(t: string) {
  switch (t) {
    case "newapi":
      return "NewAPI"
    case "sub2api":
      return "Sub2API"
    default:
      return t
  }
}
