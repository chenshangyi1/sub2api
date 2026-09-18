/** Format a single rate, trimming trailing zeros (0.0600 -> 0.06). */
export function formatRateNumber(rate: number): string {
  if (!Number.isFinite(rate)) return String(rate)
  // Keep up to 4 decimal places as used by backend numeric(10,4), drop trailing zeros.
  const fixed = rate.toFixed(4)
  return fixed.replace(/\.?0+$/, '')
}

/**
 * Adaptive parents expose min~max leaf rates; regular groups show a single rate.
 * Example: 0.06~0.22x
 */
export function formatGroupRateLabel(opts: {
  rate?: number | null
  min?: number | null
  max?: number | null
  suffix?: string
}): string {
  const suffix = opts.suffix ?? 'x'
  const min = opts.min
  const max = opts.max
  if (min != null && max != null && Number.isFinite(min) && Number.isFinite(max)) {
    if (min === max) return `${formatRateNumber(min)}${suffix}`
    return `${formatRateNumber(min)}~${formatRateNumber(max)}${suffix}`
  }
  if (opts.rate != null && Number.isFinite(opts.rate)) {
    return `${formatRateNumber(opts.rate)}${suffix}`
  }
  return ''
}
