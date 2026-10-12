import Link from "next/link";

import type { Stats } from "@/lib/api/client";
import { formatCount, formatMs } from "@/lib/format";

/**
 * One row per named queue (like Celery queues): your jobs waiting and running in it, and the
 * workers serving it. A queue with waiting jobs and no worker is flagged, since nothing will run.
 */
export function Queues({ queues }: { queues: Stats["queues"] }) {
  if (queues.length === 0) {
    return <p className="text-sm text-zinc-500">No queues yet. Start a worker and its queues appear here.</p>;
  }
  return (
    <div className="overflow-x-auto">
      <table className="w-full text-sm">
        <thead className="text-left text-xs tracking-wide text-zinc-400 uppercase">
          <tr>
            <th className="py-2 pr-3 font-medium">Queue</th>
            <th className="px-3 py-2 text-right font-medium">Ready</th>
            <th className="px-3 py-2 text-right font-medium">Delayed</th>
            <th className="px-3 py-2 text-right font-medium">Running</th>
            <th className="px-3 py-2 text-right font-medium">Lag</th>
            <th className="px-3 py-2 text-right font-medium">Workers</th>
            <th className="px-3 py-2 text-right font-medium">Slots</th>
            <th className="py-2 pl-3 text-right font-medium">Task types</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-zinc-800 tabular-nums">
          {queues.map((q) => {
            const stranded = q.workers_online === 0 && q.ready + q.delayed > 0;
            return (
              <tr key={q.queue}>
                <td className="py-2 pr-3">
                  <Link href={`/jobs?queue=${encodeURIComponent(q.queue)}`} className="font-mono text-indigo-300 hover:underline">
                    {q.queue}
                  </Link>
                  {stranded && <span className="mt-0.5 block text-xs text-amber-300">jobs waiting, but no worker serves this queue</span>}
                </td>
                <td className="px-3 py-2 text-right">{formatCount(q.ready)}</td>
                <td className="px-3 py-2 text-right">{formatCount(q.delayed)}</td>
                <td className="px-3 py-2 text-right">{formatCount(q.running)}</td>
                <td className="px-3 py-2 text-right">{formatMs(q.lag_ms)}</td>
                <td className="px-3 py-2 text-right">
                  <span className={q.workers_online ? "text-emerald-300" : "text-zinc-500"}>{q.workers_online}</span>
                </td>
                <td className="px-3 py-2 text-right" title="Jobs these workers can run at the same time">
                  {q.slots}
                </td>
                <td className="py-2 pl-3 text-right">{q.task_types}</td>
              </tr>
            );
          })}
        </tbody>
      </table>
      <p className="mt-3 text-xs text-zinc-500">
        Workers subscribe to queues and only claim jobs from them, so heavy work (training) never blocks quick work
        (notifications). Add slots to a queue by starting more workers with <code>TASKPILOT_WORKER_QUEUES</code>.
      </p>
    </div>
  );
}
