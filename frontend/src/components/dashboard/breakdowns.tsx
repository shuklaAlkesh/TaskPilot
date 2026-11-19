import type { Stats } from "@/lib/api/client";
import { formatCount, formatMs, formatPercent } from "@/lib/format";

/** p50 / p95 latency tiles for the selected window. */
export function Latency({ latency, queueLagMs }: { latency: Stats["latency"]; queueLagMs: number | null }) {
  const rows = [
    { label: "Time to start", hint: "Submit → first attempt starts (includes any delay)", p50: latency.time_to_start_p50_ms, p95: latency.time_to_start_p95_ms },
    { label: "Run time", hint: "Successful attempt duration, incl. subprocess start", p50: latency.run_time_p50_ms, p95: latency.run_time_p95_ms },
    { label: "Time to complete", hint: "Submit → succeeded, incl. retries and backoff", p50: latency.time_to_complete_p50_ms, p95: latency.time_to_complete_p95_ms },
  ];
  return (
    <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
      <div className="h-24 rounded-lg border border-zinc-800 bg-zinc-950 p-4" title="How long the oldest ready job has been waiting for a worker, right now">
        <div className="text-xs font-medium tracking-wide text-zinc-400 uppercase">Queue lag (now)</div>
        <div className="mt-1 text-xl font-semibold text-zinc-50 tabular-nums">{formatMs(queueLagMs)}</div>
        <div className="text-xs text-zinc-500">oldest ready job waiting</div>
      </div>
      {rows.map((r) => (
        <div key={r.label} className="h-24 rounded-lg border border-zinc-800 bg-zinc-950 p-4" title={r.hint}>
          <div className="text-xs font-medium tracking-wide text-zinc-400 uppercase">{r.label}</div>
          <div className="mt-1 text-xl font-semibold text-zinc-50 tabular-nums">{formatMs(r.p50)}</div>
          <div className="text-xs text-zinc-500">
            p50 · p95 <span className="text-zinc-300">{formatMs(r.p95)}</span>
          </div>
        </div>
      ))}
    </div>
  );
}

/** Per task type: all-time counts, success rate, and run time in the window. */
export function TaskTypeTable({ rows }: { rows: Stats["task_types"] }) {
  if (rows.length === 0) return <p className="text-sm text-zinc-500">No jobs yet.</p>;
  return (
    <div className="overflow-x-auto">
      <table className="w-full text-sm">
        <thead className="text-left text-xs tracking-wide text-zinc-400 uppercase">
          <tr>
            <th className="py-2 pr-3 font-medium">Task type</th>
            <th className="px-3 py-2 text-right font-medium">Total</th>
            <th className="px-3 py-2 text-right font-medium">Completed</th>
            <th className="px-3 py-2 text-right font-medium">Dead letter</th>
            <th className="px-3 py-2 text-right font-medium">Retries</th>
            <th className="px-3 py-2 text-right font-medium">In flight</th>
            <th className="px-3 py-2 text-right font-medium">Success</th>
            <th className="py-2 pl-3 text-right font-medium">Run p50 / p95</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-zinc-800 tabular-nums">
          {rows.map((r) => {
            const done = r.succeeded + r.failed;
            return (
              <tr key={r.task_type}>
                <td className="py-2 pr-3 font-mono text-zinc-200">{r.task_type}</td>
                <td className="px-3 py-2 text-right">{formatCount(r.total)}</td>
                <td className="px-3 py-2 text-right">{formatCount(r.succeeded)}</td>
                <td className="px-3 py-2 text-right">{formatCount(r.failed)}</td>
                <td className="px-3 py-2 text-right">{formatCount(r.retries)}</td>
                <td className="px-3 py-2 text-right">{formatCount(r.queued + r.running)}</td>
                <td className="px-3 py-2 text-right">{formatPercent(done ? r.succeeded / done : null)}</td>
                <td className="py-2 pl-3 text-right whitespace-nowrap text-zinc-300">
                  {formatMs(r.run_time_p50_ms)} / {formatMs(r.run_time_p95_ms)}
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

const ATTEMPT_ORDER = ["succeeded", "failed", "timed_out", "lease_expired", "cancelled"];

/** Top attempt error codes in the window, as a single-series bar list. */
export function ErrorList({ errors, attempts }: { errors: Stats["errors"]; attempts: Stats["attempts"] }) {
  const max = Math.max(1, ...errors.map((e) => e.count));
  const finished = ATTEMPT_ORDER.filter((s) => attempts[s]);
  return (
    <div className="space-y-4">
      <p className="text-sm text-zinc-400">
        Attempts finished:{" "}
        {finished.length === 0
          ? "none yet"
          : finished.map((s, i) => (
              <span key={s}>
                {i > 0 && " · "}
                <span className="font-semibold text-zinc-100 tabular-nums">{formatCount(attempts[s] ?? 0)}</span> {s.replace("_", " ")}
              </span>
            ))}
      </p>
      {errors.length === 0 ? (
        <p className="text-sm text-zinc-500">No errors in this window.</p>
      ) : (
        <ul className="space-y-2">
          {errors.map((e) => (
            <li key={e.code} className="grid grid-cols-[minmax(0,9rem)_1fr_auto] items-center gap-3 text-sm">
              <code className="truncate text-zinc-300" title={e.code}>
                {e.code}
              </code>
              <span className="h-2 rounded-r-full bg-[var(--series-completed)]" style={{ width: `${(e.count / max) * 100}%` }} aria-hidden="true" />
              <span className="font-semibold text-zinc-100 tabular-nums">{formatCount(e.count)}</span>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
