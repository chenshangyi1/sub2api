export type RateRuleSourceState = {
  currentRate?: number | null;
  fresh?: boolean;
  siteEnabled?: boolean | null;
  lastStatus?: string | null;
  monitorExcluded?: boolean;
};

export function rateRuleNeedsSource(mode?: string | null) {
  return mode !== "locked" && mode !== "manual_source";
}

export function rateRuleSourceIssues(source: RateRuleSourceState) {
  const issues: string[] = [];
  if (source.siteEnabled !== true) issues.push("源站未启用");
  if (source.lastStatus?.toLowerCase() !== "online") issues.push("源站离线");
  if (source.fresh !== true) issues.push("采集数据已过期");
  if (source.monitorExcluded === true) issues.push("已被监控排除");
  if (typeof source.currentRate !== "number" || !Number.isFinite(source.currentRate) || source.currentRate <= 0) {
    issues.push("倍率无效");
  }
  return issues;
}

export function gateRateRuleSources<T extends RateRuleSourceState>(mode: string | null | undefined, sources: T[]) {
  if (!rateRuleNeedsSource(mode)) {
    return { ok: true as const, sources, skippedSources: [] as Array<{ source: T; issues: string[] }>, reason: null, notice: null };
  }

  const evaluated = sources.map((source) => ({ source, issues: rateRuleSourceIssues(source) }));
  const usableSources = evaluated.filter((item) => item.issues.length === 0).map((item) => item.source);
  const skippedSources = evaluated.filter((item) => item.issues.length > 0);
  if (usableSources.length === 0) {
    return {
      ok: false as const,
      sources: [] as T[],
      skippedSources,
      reason: "绑定源中没有可用倍率；请重新采集、调整绑定，或改用手动上游倍率",
      notice: null,
    };
  }
  const notice = skippedSources.length > 0
    ? `已使用 ${usableSources.length}/${sources.length} 个可用源，自动跳过 ${skippedSources.length} 个异常源`
    : null;
  return { ok: true as const, sources: usableSources, skippedSources, reason: null, notice };
}
