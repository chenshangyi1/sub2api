export const RATE_DECIMAL_PLACES = 4;
const RATE_FACTOR = 10 ** RATE_DECIMAL_PLACES;

export function normalizeRateMultiplier(value: number) {
  if (!Number.isFinite(value)) return value;
  return Math.round((value + Number.EPSILON) * RATE_FACTOR) / RATE_FACTOR;
}

export function ratesEqual(left: number, right: number) {
  if (!Number.isFinite(left) || !Number.isFinite(right)) return false;
  return normalizeRateMultiplier(left) === normalizeRateMultiplier(right);
}

export function formatRateMultiplier(value: number) {
  if (!Number.isFinite(value)) return "-";
  return normalizeRateMultiplier(value).toFixed(RATE_DECIMAL_PLACES).replace(/\.?0+$/, "");
}
