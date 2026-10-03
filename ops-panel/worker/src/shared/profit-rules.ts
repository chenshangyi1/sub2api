import { normalizeRateMultiplier } from "@/shared/rates";

const AUTO_RATE_DECIMAL_PLACES = 3;
const AUTO_RATE_FACTOR = 10 ** AUTO_RATE_DECIMAL_PLACES;
const DEFAULT_PROFIT_RATIO = 0.25;
const LOW_RATE_MIN_PROFIT_THRESHOLD = 0.05;
const MIN_LOW_RATE_PROFIT = 0.01;
const COMPACT_DECIMAL_PLACES = 2;
const COMPACT_FACTOR = 10 ** COMPACT_DECIMAL_PLACES;

function finiteNumber(value: unknown) {
  if (value === null || value === undefined || value === "") return null;
  const numeric = Number(value);
  return Number.isFinite(numeric) && numeric > 0 ? numeric : null;
}

function ceilToAutoDecimals(value: number) {
  return Math.ceil((value - Number.EPSILON) * AUTO_RATE_FACTOR) / AUTO_RATE_FACTOR;
}

function floorToCompactDecimals(value: number) {
  if (!Number.isFinite(value) || value <= 0) return value;
  return Math.floor((value + Number.EPSILON) * COMPACT_FACTOR) / COMPACT_FACTOR;
}

function minimumAbsoluteProfit(rate: number) {
  return rate < LOW_RATE_MIN_PROFIT_THRESHOLD ? MIN_LOW_RATE_PROFIT : 0;
}

export function firstSignificantDigitStep(value: number) {
  const abs = Math.abs(value);
  if (!Number.isFinite(abs) || abs <= 0) return 0;
  return 10 ** Math.floor(Math.log10(abs));
}

export function billingRateMultiplier(accountRate: unknown) {
  const rate = finiteNumber(accountRate);
  if (rate === null) return 1;
  const proportionalProfit = rate * DEFAULT_PROFIT_RATIO;
  return rate + Math.max(proportionalProfit, minimumAbsoluteProfit(rate));
}

export function ceilGroupRatio(value: unknown) {
  const numeric = finiteNumber(value);
  if (numeric === null) return 1;
  return ceilToAutoDecimals(numeric);
}

export function profitGroupRateMultiplier(sourceRate: unknown) {
  const rate = finiteNumber(sourceRate);
  if (rate === null) return 1;

  const target = ceilGroupRatio(billingRateMultiplier(rate));
  const compact = floorToCompactDecimals(target);
  const minimumRate = rate + minimumAbsoluteProfit(rate);
  if (compact > rate && compact + Number.EPSILON >= minimumRate) {
    return normalizeRateMultiplier(compact);
  }
  return normalizeRateMultiplier(target);
}
