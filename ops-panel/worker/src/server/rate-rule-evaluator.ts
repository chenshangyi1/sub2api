import { profitGroupRateMultiplier } from "@/server/profit-rules";
import { normalizeRateMultiplier } from "@/shared/rates";

export type GroupRateRuleInput = {
  enabled: boolean;
  mode: "first" | "average" | "min" | "max" | "custom" | "locked" | "manual_source";
  offset: number;
  expression?: string | null;
};

function normalizeNullable(value?: string | null) {
  const trimmed = value?.trim();
  return trimmed ? trimmed : null;
}

function normalizeRule(rule?: Partial<GroupRateRuleInput> | null): GroupRateRuleInput {
  return {
    enabled: rule?.enabled ?? false,
    mode: rule?.mode ?? "first",
    offset: Number.isFinite(rule?.offset) ? Number(rule?.offset) : 0,
    expression: normalizeNullable(rule?.expression),
  };
}

function toFiniteNumber(value: unknown) {
  if (value === null || value === undefined || value === "") return null;
  const numeric = Number(value);
  return Number.isFinite(numeric) ? numeric : null;
}

function safeEvaluateCustomExpression(expression: string, rates: number[], current: number | null) {
  if (!/^[\d\s+\-*/%().,_a-zA-Z]+$/.test(expression)) {
    throw new Error("自定义公式只能包含数字、变量、函数和数学运算符");
  }

  const identifiers = expression.match(/[A-Za-z_][A-Za-z0-9_]*/g) ?? [];
  const allowed = new Set([
    "abs", "avg", "ceil", "clamp", "count", "current", "first", "floor",
    "max", "min", "profit", "rate", "round", "sum",
  ]);
  for (const identifier of identifiers) {
    if (!allowed.has(identifier) && !/^r\d+$/.test(identifier)) {
      throw new Error(`自定义公式包含不支持的变量或函数：${identifier}`);
    }
  }

  const avg = rates.reduce((total, value) => total + value, 0) / rates.length;
  const vars: Record<string, unknown> = {
    abs: Math.abs,
    avg,
    ceil: Math.ceil,
    clamp: (value: number, min: number, max: number) => Math.min(Math.max(value, min), max),
    count: rates.length,
    current: current ?? 0,
    first: rates[0],
    floor: Math.floor,
    max: (...values: number[]) => Math.max(...(values.length ? values : rates)),
    min: (...values: number[]) => Math.min(...(values.length ? values : rates)),
    profit: (value = rates[0]) => profitGroupRateMultiplier(value),
    rate: (index: number) => rates[index] ?? 0,
    round: (value: number, digits = 4) => {
      const factor = 10 ** digits;
      return Math.round(value * factor) / factor;
    },
    sum: (...values: number[]) => (values.length ? values : rates).reduce((total, value) => total + value, 0),
  };

  for (let index = 0; index < Math.max(rates.length, 20); index += 1) {
    vars[`r${index}`] = rates[index] ?? 0;
  }

  const argNames = Object.keys(vars);
  const argValues = Object.values(vars);
  const fn = new Function(...argNames, `"use strict"; return (${expression});`);
  return Number(fn(...argValues));
}

export function evaluateGroupRateRule(input: {
  rule?: Partial<GroupRateRuleInput> | null;
  sourceRates: Array<number | null | undefined>;
  currentRate?: number | null;
}) {
  const rule = normalizeRule(input.rule);
  const manualValue = toFiniteNumber(rule.offset);

  if (rule.mode === "locked") {
    if (manualValue === null || manualValue <= 0) {
      throw new Error("手动锁定倍率必须是大于 0 的有效倍率");
    }
    return normalizeRateMultiplier(manualValue);
  }

  if (rule.mode === "manual_source") {
    if (manualValue === null || manualValue <= 0) {
      throw new Error("手动上游倍率必须是大于 0 的有效倍率");
    }
    const result = normalizeRateMultiplier(profitGroupRateMultiplier(manualValue));
    if (!Number.isFinite(result) || result <= 0) {
      throw new Error("规则计算结果必须是大于 0 的有效倍率");
    }
    return result;
  }

  const rates = input.sourceRates.map(toFiniteNumber).filter((value): value is number => value !== null);
  if (rates.length === 0) {
    throw new Error("没有可用于计算的采集源倍率");
  }

  const avg = rates.reduce((total, value) => total + value, 0) / rates.length;
  let base: number;
  switch (rule.mode) {
    case "average":
      base = avg;
      break;
    case "min":
      base = Math.min(...rates);
      break;
    case "max":
      base = Math.max(...rates);
      break;
    case "custom":
      base = safeEvaluateCustomExpression(rule.expression || "avg", rates, input.currentRate ?? null);
      break;
    case "first":
      base = rates[0];
      break;
    default:
      throw new Error(`不支持的倍率计算模式：${String(rule.mode)}`);
  }

  const result = normalizeRateMultiplier(base + rule.offset);
  if (!Number.isFinite(result) || result <= 0) {
    throw new Error("规则计算结果必须是大于 0 的有效倍率");
  }
  if (result > 100_000) {
    throw new Error("规则计算结果超过允许范围");
  }
  return result;
}
