"use client";

import Link from "next/link";

import type { Stats } from "@/lib/api/client";
import { formatCount, formatPercent } from "@/lib/format";
import { Icon, type IconName } from "./icons";
import { LiveNumber } from "./live-number";
import { Sparkline } from "./sparkline";

type Tile = {
  icon: IconName;
  tone: string; // accent text colour: icon, sparkline dot, top edge
  edge: string;
  label: string;
  value: number;
  detail: string;
  href: string;
  trend?: { values: number[]; label: string } | undefined;
};

const SPARK_POINTS = 24;

/** Stat tile: label, value, one line of context, and a trend over the selected window when there is one. */
function MetricTile({ icon, tone, edge, label, value, detail, href, trend }: Tile) {
  return (
    <Link
      href={href}
      className={`group relative flex h-28 flex-col justify-between overflow-hidden rounded-xl border border-zinc-800 bg-linear-to-b from-zinc-900 to-zinc-900/40 p-4 transition-colors hover:border-zinc-600`}
    >
      <span className={`absolute inset-x-0 top-0 h-0.5 ${edge} opacity-70`} aria-hidden="true" />
      <div className="flex items-center gap-2 text-xs font-medium tracking-wide text-zinc-400 uppercase">
        <span className={tone}>
          <Icon name={icon} className="size-4" />
        </span>
        {label}
      </div>
      <div className="flex items-end justify-between gap-2">
        <div className="min-w-0">
          <LiveNumber value={value} className="inline-block text-[1.7rem] leading-none font-semibold tracking-tight text-zinc-50" />
          <div className="mt-1 truncate text-xs text-zinc-500">{detail}</div>
        </div>
        {trend && <Sparkline values={trend.values} label={trend.label} className={tone} />}
      </div>
    </Link>
  );
}

/** The application's job counts at a glance (all time, within retention), from GET /v1/stats. */
export function Metrics({ stats }: { stats: Stats }) {
  const t = stats.totals;
  const buckets = stats.throughput.slice(-SPARK_POINTS);
  const per = stats.bucket_seconds >= 60 ? `${stats.bucket_seconds / 60} min` : `${stats.bucket_seconds} s`;
  const trend = (key: "submitted" | "succeeded" | "failed" | "retried", what: string) => {
    const values = buckets.map((b) => b[key]);
    return values.some((v) => v > 0)
      ? { values, label: `${what} per ${per}, last ${values.length} intervals. Latest: ${formatCount(values[values.length - 1] ?? 0)}` }
      : undefined;
  };
  const tiles: Tile[] = [
    { icon: "pulse", tone: "text-violet-400", edge: "bg-violet-500", label: "Total", value: t.submitted, detail: `${formatCount(stats.last_minute.submitted)} in the last minute`, href: "/jobs", trend: trend("submitted", "Jobs submitted") },
    { icon: "layers", tone: "text-blue-400", edge: "bg-blue-500", label: "Pending", value: t.ready + t.delayed, detail: `${formatCount(t.ready)} ready · ${formatCount(t.delayed)} scheduled`, href: "/jobs?status=queued" },
    { icon: "workers", tone: "text-cyan-400", edge: "bg-cyan-500", label: "Running", value: t.running, detail: `on ${formatCount(t.active_workers)} worker${t.active_workers === 1 ? "" : "s"}`, href: "/jobs?status=running" },
    { icon: "check", tone: "text-emerald-400", edge: "bg-emerald-500", label: "Completed", value: t.succeeded, detail: `${formatPercent(stats.success_rate)} success rate`, href: "/jobs?status=succeeded", trend: trend("succeeded", "Jobs completed") },
    { icon: "cross", tone: "text-rose-400", edge: "bg-rose-500", label: "Failed", value: t.failed, detail: `${formatCount(t.cancelled)} cancelled`, href: "/jobs?status=failed", trend: trend("failed", "Jobs failed") },
    { icon: "retry", tone: "text-amber-400", edge: "bg-amber-500", label: "Retries", value: t.retries, detail: `${formatCount(t.retry_waiting)} waiting to retry`, href: "/jobs?status=queued", trend: trend("retried", "Retries started") },
  ];
  return (
    <div className="grid grid-cols-2 gap-3 md:grid-cols-3 xl:grid-cols-6" aria-label="Job counts">
      {tiles.map((tile) => (
        <MetricTile key={tile.label} {...tile} />
      ))}
    </div>
  );
}
