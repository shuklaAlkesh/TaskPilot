"use client";

import type { Stats } from "@/lib/api/client";
import { formatCount, formatMs } from "@/lib/format";
import { LiveNumber } from "./live-number";

export type StageKey = "submitted" | "delayed" | "ready" | "running" | "succeeded" | "retried" | "failed" | "cancelled";

type Props = {
  stats: Stats;
  selected: StageKey | null;
  onSelect: (key: StageKey | null) => void;
};

/** Fixed-size stage: width and height never depend on the numbers inside. */
function Stage({
  id,
  label,
  value,
  detail,
  accent,
  selected,
  onSelect,
}: {
  id: StageKey;
  label: string;
  value: number;
  detail: string;
  accent: string;
  selected: StageKey | null;
  onSelect: Props["onSelect"];
}) {
  const active = selected === id;
  return (
    <button
      type="button"
      onClick={() => onSelect(active ? null : id)}
      aria-pressed={active}
      aria-controls="stage-inspector"
      className={`flex h-32 w-full flex-col items-center justify-center rounded-lg border px-3 text-center transition-colors xl:w-36 xl:shrink-0 ${
        active ? "border-indigo-500 bg-indigo-950/40" : "border-zinc-800 bg-zinc-950 hover:border-zinc-600"
      }`}
    >
      <span className={`mb-2 h-1 w-8 rounded-full ${accent}`} aria-hidden="true" />
      <LiveNumber value={value} className="text-2xl font-semibold text-zinc-50" />
      <span className="text-sm text-zinc-300">{label}</span>
      <span className="mt-0.5 w-full truncate text-xs text-zinc-500">{detail}</span>
    </button>
  );
}

/** Arrow between stages; dashes travel along it while jobs are moving through that hop. */
function Arrow({ flowing }: { flowing: boolean }) {
  return (
    <svg viewBox="0 0 28 12" className="hidden h-3 w-7 shrink-0 xl:block" aria-hidden="true">
      <line
        x1="0"
        y1="6"
        x2="22"
        y2="6"
        strokeWidth="2"
        strokeDasharray={flowing ? "4 2" : undefined}
        className={flowing ? "stroke-indigo-400 motion-safe:animate-flow" : "stroke-zinc-600"}
      />
      <path d="M20 1l6 5-6 5" fill="none" strokeWidth="2" className={flowing ? "stroke-indigo-400" : "stroke-zinc-600"} />
    </svg>
  );
}

function Outcome({ id, label, value, dot, selected, onSelect }: { id: StageKey; label: string; value: number; dot: string; selected: StageKey | null; onSelect: Props["onSelect"] }) {
  const active = selected === id;
  return (
    <button
      type="button"
      onClick={() => onSelect(active ? null : id)}
      aria-pressed={active}
      aria-controls="stage-inspector"
      className={`flex h-8 items-center gap-2 rounded-full border px-3 text-sm transition-colors ${
        active ? "border-indigo-500 bg-indigo-950/40" : "border-zinc-800 bg-zinc-950 hover:border-zinc-600"
      }`}
    >
      <span className={`size-2 shrink-0 rounded-full ${dot}`} aria-hidden="true" />
      <span className="truncate text-zinc-400">{label}</span>
      <LiveNumber value={value} className="ml-auto font-semibold text-zinc-100" />
    </button>
  );
}

/** submit → delayed → ready → running → outcome, as counted by GET /v1/stats. Click any part to inspect it. */
export function Pipeline({ stats, selected, onSelect }: Props) {
  const t = stats.totals;
  const common = { selected, onSelect };
  return (
    <div className="grid grid-cols-2 gap-3 sm:grid-cols-4 xl:flex xl:items-center xl:justify-center">
      <Stage id="submitted" label="Submitted" value={t.submitted} accent="bg-violet-500" detail={`${formatCount(stats.last_minute.submitted)} in the last min`} {...common} />
      <Arrow flowing={stats.last_minute.submitted > 0} />
      <Stage id="delayed" label="Delayed queue" value={t.delayed} accent="bg-amber-500" detail={`${formatCount(t.retry_waiting)} awaiting retry`} {...common} />
      <Arrow flowing={t.delayed > 0} />
      <Stage id="ready" label="Ready queue" value={t.ready} accent="bg-blue-500" detail={`oldest waits ${formatMs(stats.queue_lag_ms)}`} {...common} />
      <Arrow flowing={t.ready > 0} />
      <Stage id="running" label="Running" value={t.running} accent="bg-cyan-500" detail={`on ${t.active_workers} worker${t.active_workers === 1 ? "" : "s"}`} {...common} />
      <Arrow flowing={t.running > 0} />
      <div className="col-span-2 grid grid-cols-2 gap-2 sm:col-span-4 sm:grid-cols-4 xl:flex xl:w-56 xl:shrink-0 xl:flex-col xl:gap-1.5" role="group" aria-label="Outcomes">
        <Outcome id="succeeded" label="Completed" value={t.succeeded} dot="bg-[var(--series-completed)]" {...common} />
        <Outcome id="retried" label="Retried" value={t.retries} dot="bg-amber-500" {...common} />
        <Outcome id="failed" label="Dead letter" value={t.failed} dot="bg-[var(--series-dead)]" {...common} />
        <Outcome id="cancelled" label="Cancelled" value={t.cancelled} dot="bg-[var(--series-cancelled)]" {...common} />
      </div>
    </div>
  );
}
