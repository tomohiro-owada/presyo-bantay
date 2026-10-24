/**
 * Pure statistics helpers for the Presyo Bantay site.
 *
 * These mirror `presyo_bantay/stats.py` semantics exactly:
 * - `pearson` returns `r: null` with fewer than 3 pairs or zero variance.
 *   `reason` distinguishes `"insufficient"` (n < 3) from `"zero_variance"`.
 * - `laggedCorrelation` pairs the price on date `d` with the FX rate as-of
 *   `d - lag` (last available rate on or before that date, skipped when more
 *   than `maxStaleDays` old; ECB publishes business days only so plain
 *   weekends resolve to Friday).
 */

export type NullReason = "insufficient" | "zero_variance";

export interface PearsonResult {
  r: number | null;
  n: number;
  /** Why `r` is null (null when `r` is a valid number). */
  reason: NullReason | null;
}

export interface LagResult {
  lag: number;
  r: number | null;
  n: number;
  /** Why `r` is null (null when `r` is a valid number). */
  reason: NullReason | null;
}

/** A date string (`YYYY-MM-DD`) paired with a possibly-missing value. */
export type DatedValue = readonly [string, number | null];

/** Parse `YYYY-MM-DD` as a UTC day timestamp (avoids timezone pitfalls). */
export function dayMs(iso: string): number {
  const [y, m, d] = iso.split("-").map(Number);
  return Date.UTC(y, m - 1, d);
}

/** Format a UTC day timestamp back to `YYYY-MM-DD`. */
export function dayIso(ms: number): string {
  const t = new Date(ms);
  const y = t.getUTCFullYear();
  const m = String(t.getUTCMonth() + 1).padStart(2, "0");
  const d = String(t.getUTCDate()).padStart(2, "0");
  return `${y}-${m}-${d}`;
}

/** Shift an ISO date by `deltaDays` (negative = earlier). */
export function shiftDate(iso: string, deltaDays: number): string {
  return dayIso(dayMs(iso) + deltaDays * 86_400_000);
}

/** Whole days from `earlier` to `later` (both `YYYY-MM-DD`). */
export function diffDays(earlier: string, later: string): number {
  return Math.round((dayMs(later) - dayMs(earlier)) / 86_400_000);
}

export function pearson(
  xs: ReadonlyArray<number | null>,
  ys: ReadonlyArray<number | null>,
): PearsonResult {
  const pairs: Array<[number, number]> = [];
  const len = Math.min(xs.length, ys.length);
  for (let i = 0; i < len; i++) {
    const x = xs[i];
    const y = ys[i];
    if (x !== null && x !== undefined && y !== null && y !== undefined) {
      pairs.push([x, y]);
    }
  }
  const n = pairs.length;
  if (n < 3) {
    return { r: null, n, reason: "insufficient" };
  }
  let sumX = 0;
  let sumY = 0;
  for (const [x, y] of pairs) {
    sumX += x;
    sumY += y;
  }
  const meanX = sumX / n;
  const meanY = sumY / n;
  let sxx = 0;
  let syy = 0;
  let sxy = 0;
  for (const [x, y] of pairs) {
    const dx = x - meanX;
    const dy = y - meanY;
    sxx += dx * dx;
    syy += dy * dy;
    sxy += dx * dy;
  }
  if (sxx === 0 || syy === 0) {
    return { r: null, n, reason: "zero_variance" };
  }
  return { r: sxy / Math.sqrt(sxx * syy), n, reason: null };
}

/**
 * Last available FX rate on or before `target` (`YYYY-MM-DD`), or `null`
 * when there is none or the nearest rate is more than `maxStaleDays` old.
 *
 * `fxDates` must be sorted ascending; `fxByDate` maps date -> rate.
 * Mirrors the `asof` helper inside `stats.py::lagged_correlation`.
 */
export function asofLookup(
  fxByDate: ReadonlyMap<string, number>,
  fxDates: ReadonlyArray<string>,
  target: string,
  maxStaleDays = 5,
): number | null {
  // Binary search: index of the last entry <= target.
  let lo = 0;
  let hi = fxDates.length - 1;
  let best = -1;
  while (lo <= hi) {
    const mid = (lo + hi) >> 1;
    if (fxDates[mid] <= target) {
      best = mid;
      lo = mid + 1;
    } else {
      hi = mid - 1;
    }
  }
  if (best < 0) {
    return null;
  }
  const bestDate = fxDates[best];
  if (diffDays(bestDate, target) > maxStaleDays) {
    return null;
  }
  const rate = fxByDate.get(bestDate);
  return rate === undefined ? null : rate;
}

export function laggedCorrelation(
  priceSeries: ReadonlyArray<DatedValue>,
  fxSeries: ReadonlyArray<DatedValue>,
  lagDays: number,
  maxStaleDays = 5,
): LagResult {
  const fxByDate = new Map<string, number>();
  for (const [day, rate] of fxSeries) {
    if (rate !== null && rate !== undefined) {
      fxByDate.set(day, rate);
    }
  }
  const fxDates = [...fxByDate.keys()].sort();
  const xs: number[] = [];
  const ys: number[] = [];
  for (const [day, price] of priceSeries) {
    if (price === null || price === undefined) {
      continue;
    }
    const target = shiftDate(day, -lagDays);
    const rate = asofLookup(fxByDate, fxDates, target, maxStaleDays);
    if (rate === null) {
      continue;
    }
    xs.push(price);
    ys.push(rate);
  }
  const { r, n, reason } = pearson(xs, ys);
  return { lag: lagDays, r, n, reason };
}
