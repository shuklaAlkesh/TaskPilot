import type { Stats } from "@/lib/api/client";
import { formatCount, formatMs, formatPercent } from "@/lib/format";

function Cell({ label, children, sub }: { label: string; children: React.ReactNode; sub: React.ReactNode }) {
  return (
    <div className="min-w-0 px-4 py-3">
      <div className="text-xs font-medium tracking-wide text-zinc-500 uppercase">{label}</div>
      <div className="mt-1 text-lg font-semibold text-zinc-50">{children}</div>
      <div className="truncate text-xs text-zinc-500">{sub}</div>
    </div>
  );
}

/**
 * One-line system health for the application: success rate, throughput, queue lag, run time, and
 * whether every queue with work has a worker. Status colours always come with an icon and a label.
 */
export function Health({ stats }: { stats: Stats }) {
  const pct = stats.success_rate === null ? 0 : stats.success_rate * 100;
  const served = stats.queues.filter((q) => q.workers_online > 0).length;
  const stranded = stats.queues.filter((q) => q.workers_online === 0 && q.ready + q.delayed > 0);
  return (
    <section
      aria-label="System health"
      className="grid grid-cols-2 divide-zinc-800 rounded-xl border border-zinc-800 bg-zinc-900/60 sm:grid-cols-3 lg:grid-cols-5 lg:divide-x"
    >
      <Cell label="Success rate" sub={<span>completed ÷ (completed + failed)</span>}>
        {formatPercent(stats.success_rate)}
        <div
          className="mt-1.5 h-1.5 overflow-hidden rounded-full bg-blue-950"
          role="meter"
          aria-label="Success rate"
          aria-valuemin={0}
          aria-valuemax={100}
          aria-valuenow={Math.round(pct * 10) / 10}
        >
          <div className="h-full rounded-full bg-blue-500 transition-[width] duration-500" style={{ width: `${pct}%` }} />
        </div>
      </Cell>
      <Cell label="Throughput" sub={`${formatCount(stats.last_minute.submitted)} submitted in the last minute`}>
        {formatCount(stats.last_minute.finished)}
        <span className="ml-1 text-sm font-normal text-zinc-500">finished / min</span>
      </Cell>
      <Cell label="Queue lag" sub="oldest ready job has waited">
        {formatMs(stats.queue_lag_ms)}
      </Cell>
      <Cell label={`Run time · ${stats.window}`} sub={`p50 ${formatMs(stats.latency.run_time_p50_ms)}`}>
        {formatMs(stats.latency.run_time_p95_ms)}
        <span className="ml-1 text-sm font-normal text-zinc-500">p95</span>
      </Cell>
      <Cell
        label="Worker coverage"
        sub={
          stranded.length > 0 ? (
            <span className="text-amber-300">
              <span aria-hidden="true">⚠ </span>
              {stranded.length} queue{stranded.length === 1 ? "" : "s"} waiting with no worker
            </span>
          ) : (
            "queues with an online worker"
          )
        }
      >
        {served}
        <span className="text-sm font-normal text-zinc-500"> / {stats.queues.length} queues</span>
      </Cell>
    </section>
  );
}
