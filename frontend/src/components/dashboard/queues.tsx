import Link from "next/link";

import type { Stats } from "@/lib/api/client";
import { formatCount, formatMs } from "@/lib/format";

type Queue = Stats["queues"][number];

/** Queue state as icon + label + colour (never colour alone). */
function state(q: Queue): { icon: string; label: string; cls: string } {
  if (q.workers_online === 0 && q.ready + q.delayed > 0) return { icon: "⚠", label: "Stranded", cls: "text-amber-300" };
  if (q.workers_online === 0) return { icon: "○", label: "No worker", cls: "text-zinc-500" };
  if (q.running > 0) return { icon: "●", label: "Busy", cls: "text-sky-300" };
  return { icon: "✓", label: "Ready", cls: "text-emerald-300" };
}

/** Running jobs against the worker slots serving this queue, as a same-ramp meter. */
function Utilization({ q }: { q: Queue }) {
  if (q.slots === 0) return <span className="text-zinc-600">—</span>;
  const pct = Math.min(100, (q.running / q.slots) * 100);
  return (
    <div className="flex items-center justify-end gap-2" title={`${q.running} of ${q.slots} slots in use`}>
      <div
        className="h-1.5 w-20 overflow-hidden rounded-full bg-cyan-950"
        role="meter"
        aria-label={`${q.queue} utilization`}
        aria-valuemin={0}
        aria-valuemax={q.slots}
        aria-valuenow={q.running}
      >
        <div className="h-full rounded-full bg-cyan-400 transition-[width] duration-500" style={{ width: `${pct}%` }} />
      </div>
      <span className="w-14 text-right text-zinc-300">
        {q.running}/{q.slots}
      </span>
    </div>
  );
}

/** One row per named queue this application uses: its jobs waiting and running, and the workers serving it. */
export function Queues({ queues }: { queues: Stats["queues"] }) {
  if (queues.length === 0) {
    return <p className="text-sm text-zinc-500">No queues yet. They appear once a worker registers this application&apos;s tasks or you submit a job.</p>;
  }
  return (
    <div className="overflow-x-auto">
      <table className="w-full text-sm">
        <thead className="text-left text-xs tracking-wide text-zinc-500 uppercase">
          <tr>
            <th className="pb-2 pr-3 font-medium">Queue</th>
            <th className="px-3 pb-2 font-medium">Status</th>
            <th className="px-3 pb-2 text-right font-medium">Ready</th>
            <th className="px-3 pb-2 text-right font-medium">Delayed</th>
            <th className="px-3 pb-2 text-right font-medium">Lag</th>
            <th className="px-3 pb-2 text-right font-medium">Workers</th>
            <th className="px-3 pb-2 text-right font-medium">Utilization</th>
            <th className="pb-2 pl-3 text-right font-medium">Task types</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-zinc-800/80 tabular-nums">
          {queues.map((q) => {
            const s = state(q);
            return (
              <tr key={q.queue} className="hover:bg-zinc-800/30">
                <td className="py-2.5 pr-3">
                  <Link href={`/jobs?queue=${encodeURIComponent(q.queue)}`} className="font-mono text-indigo-300 hover:underline">
                    {q.queue}
                  </Link>
                </td>
                <td className={`px-3 py-2.5 whitespace-nowrap ${s.cls}`}>
                  <span aria-hidden="true">{s.icon} </span>
                  {s.label}
                </td>
                <td className="px-3 py-2.5 text-right">{formatCount(q.ready)}</td>
                <td className="px-3 py-2.5 text-right">{formatCount(q.delayed)}</td>
                <td className="px-3 py-2.5 text-right">{formatMs(q.lag_ms)}</td>
                <td className={`px-3 py-2.5 text-right ${q.workers_online ? "text-emerald-300" : "text-zinc-500"}`}>{q.workers_online}</td>
                <td className="px-3 py-2.5">
                  <Utilization q={q} />
                </td>
                <td className="py-2.5 pl-3 text-right text-zinc-400">{q.task_types}</td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}
