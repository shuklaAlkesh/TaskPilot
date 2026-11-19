const relative = new Intl.RelativeTimeFormat("en", { numeric: "auto" });
const dateTime = new Intl.DateTimeFormat("en", { dateStyle: "medium", timeStyle: "medium" });

const UNITS: [Intl.RelativeTimeFormatUnit, number][] = [
  ["day", 86_400],
  ["hour", 3_600],
  ["minute", 60],
  ["second", 1],
];

/** "3 minutes ago", "in 2 hours", "now". */
export function timeAgo(iso: string, now = Date.now()): string {
  const seconds = Math.round((Date.parse(iso) - now) / 1000);
  const [unit, size] = UNITS.find(([, s]) => Math.abs(seconds) >= s) ?? ["second", 1];
  return relative.format(Math.round(seconds / size), unit);
}

export function formatDateTime(iso: string | null | undefined): string {
  return iso ? dateTime.format(new Date(iso)) : "—";
}

export function shortId(id: string): string {
  return id.slice(-8);
}

export const TERMINAL_STATUSES = ["succeeded", "failed", "cancelled"] as const;

export function isTerminal(status: string): boolean {
  return (TERMINAL_STATUSES as readonly string[]).includes(status);
}

const compact = new Intl.NumberFormat("en", { notation: "compact", maximumFractionDigits: 1 });
const whole = new Intl.NumberFormat("en");

/** 1,284 below 10k, then 12.9K / 1.2M. */
export function formatCount(n: number): string {
  return n < 10_000 ? whole.format(n) : compact.format(n);
}

/** "—" for no data, then ms / s / min with sensible precision. */
export function formatMs(ms: number | null | undefined): string {
  if (ms === null || ms === undefined) return "—";
  if (ms < 1000) return `${Math.round(ms)} ms`;
  if (ms < 60_000) return `${(ms / 1000).toFixed(ms < 10_000 ? 2 : 1)} s`;
  return `${(ms / 60_000).toFixed(1)} min`;
}

/** 0.9937 → "99.4%"; null (nothing finished yet) → "—". */
export function formatPercent(ratio: number | null | undefined): string {
  return ratio === null || ratio === undefined ? "—" : `${(ratio * 100).toFixed(1)}%`;
}
