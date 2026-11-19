import Link from "next/link";

import { StatusBadge } from "@/components/status-badge";
import type { Job } from "@/lib/api/client";
import { formatMs, shortId, timeAgo } from "@/lib/format";

/** Run time for finished jobs, elapsed time for running ones, nothing for queued ones. */
function duration(j: Job): string {
  if (!j.started_at) return "—";
  const end = j.finished_at ? Date.parse(j.finished_at) : Date.now();
  return formatMs(end - Date.parse(j.started_at));
}

/** The newest jobs, linking to their detail pages. */
export function RecentJobs({ jobs }: { jobs: Job[] }) {
  if (jobs.length === 0) {
    return <p className="text-sm text-zinc-500">No jobs yet. Jobs your application submits show up here as they run.</p>;
  }
  return (
    <div className="overflow-x-auto">
      <table className="w-full text-sm">
        <thead className="text-left text-xs tracking-wide text-zinc-500 uppercase">
          <tr>
            <th className="pb-2 pr-3 font-medium">Job</th>
            <th className="px-3 pb-2 font-medium">Task</th>
            <th className="px-3 pb-2 font-medium">Status</th>
            <th className="px-3 pb-2 text-right font-medium">Attempts</th>
            <th className="px-3 pb-2 text-right font-medium">Duration</th>
            <th className="pb-2 pl-3 text-right font-medium">Submitted</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-zinc-800/80">
          {jobs.map((j) => (
            <tr key={j.id} className="hover:bg-zinc-800/30">
              <td className="py-2 pr-3">
                <Link href={`/jobs/${j.id}`} className="font-mono text-indigo-400 hover:underline" title={j.id}>
                  {shortId(j.id)}
                </Link>
              </td>
              <td className="max-w-64 px-3 py-2">
                <span className="block truncate text-zinc-200" title={j.task_type}>
                  {j.task_type}
                </span>
                <code className="text-xs text-zinc-500">{j.queue}</code>
              </td>
              <td className="px-3 py-2">
                <StatusBadge status={j.status} cancelRequested={j.cancel_requested} />
              </td>
              <td className={`px-3 py-2 text-right tabular-nums ${j.attempt_count > 1 ? "text-amber-300" : "text-zinc-400"}`}>
                {j.attempt_count}/{j.max_attempts}
              </td>
              <td className="px-3 py-2 text-right text-zinc-400 tabular-nums" suppressHydrationWarning>
                {duration(j)}
              </td>
              <td className="py-2 pl-3 text-right whitespace-nowrap text-zinc-500" suppressHydrationWarning>
                <time dateTime={j.created_at}>{timeAgo(j.created_at)}</time>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
