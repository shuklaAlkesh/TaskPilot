"use client";

import type { Stats } from "@/lib/api/client";
import { formatCount, formatPercent } from "@/lib/format";
import { Icon, type IconName } from "./icons";
import { LiveNumber } from "./live-number";

type Tile = { icon: IconName; tone: string; label: string; value: number; detail: string; hint: string };

/** Fixed-height tile: label, value, and one line of context. */
function MetricTile({ icon, tone, label, value, detail, hint }: Tile) {
  return (
    <div className="flex h-24 items-start gap-3 rounded-lg border border-zinc-800 bg-zinc-950 p-4" title={hint}>
      <span className={`mt-0.5 ${tone}`}>
        <Icon name={icon} />
      </span>
      <div className="min-w-0">
        <LiveNumber value={value} className="inline-block text-2xl leading-tight font-semibold text-zinc-50" />
        <div className="text-xs font-medium tracking-wide text-zinc-400 uppercase">{label}</div>
        <div className="truncate text-xs text-zinc-500">{detail}</div>
      </div>
    </div>
  );
}

/** All-time counters. Values use text colour; the icon beside each carries its identity. */
export function Metrics({ stats }: { stats: Stats }) {
  const t = stats.totals;
  const finished = t.succeeded + t.failed + t.cancelled;
  const ofFinished = (n: number) => `${formatPercent(finished ? n / finished : null)} of finished jobs`;
  const tiles: Tile[] = [
    { icon: "check", tone: "text-emerald-400", label: "Completed", value: t.succeeded, detail: ofFinished(t.succeeded), hint: "Jobs whose handler returned a result" },
    { icon: "cross", tone: "text-rose-400", label: "Dead letter", value: t.failed, detail: ofFinished(t.failed), hint: "Permanent errors, or retries exhausted" },
    { icon: "retry", tone: "text-amber-400", label: "Retries", value: t.retries, detail: `across ${formatCount(t.jobs_retried)} jobs`, hint: "Retry attempts started (attempts after the first)" },
    { icon: "layers", tone: "text-blue-400", label: "Queue size", value: t.ready + t.delayed, detail: `${formatCount(t.ready)} ready · ${formatCount(t.delayed)} delayed`, hint: "Jobs waiting for a worker" },
    { icon: "workers", tone: "text-cyan-400", label: "Active workers", value: t.active_workers, detail: `running ${formatCount(t.running)} jobs`, hint: "Workers running one of your jobs now. Idle workers are not counted." },
    { icon: "clock", tone: "text-amber-400", label: "Delayed", value: t.delayed, detail: `${formatCount(t.retry_waiting)} awaiting retry`, hint: "Waiting for a scheduled time or retry backoff" },
    { icon: "ban", tone: "text-teal-400", label: "Cancelled", value: t.cancelled, detail: ofFinished(t.cancelled), hint: "Cancelled before or during a run" },
    { icon: "pulse", tone: "text-violet-400", label: "Submitted", value: t.submitted, detail: `${formatCount(stats.last_minute.submitted)} in the last minute`, hint: "All jobs still within retention" },
  ];
  const pct = stats.success_rate === null ? 0 : stats.success_rate * 100;
  return (
    <div className="space-y-4">
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
        {tiles.map((tile) => (
          <MetricTile key={tile.label} {...tile} />
        ))}
      </div>
      <div>
        <div className="mb-1.5 flex justify-between text-sm">
          <span className="text-zinc-400">Success rate</span>
          <span className="font-semibold text-zinc-100 tabular-nums">{formatPercent(stats.success_rate)}</span>
        </div>
        <div
          className="h-2 overflow-hidden rounded-full bg-blue-950"
          role="meter"
          aria-label="Success rate: completed out of completed plus dead letter"
          aria-valuemin={0}
          aria-valuemax={100}
          aria-valuenow={Math.round(pct * 10) / 10}
        >
          <div className="h-full rounded-full bg-blue-500 transition-[width] duration-500" style={{ width: `${pct}%` }} />
        </div>
        <p className="mt-1 text-xs text-zinc-500">Completed ÷ (completed + dead letter). Cancelled jobs are not counted.</p>
      </div>
    </div>
  );
}
