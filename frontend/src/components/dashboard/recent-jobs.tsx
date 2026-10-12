import Link from "next/link";

import { StatusBadge } from "@/components/status-badge";
import type { Job } from "@/lib/api/client";
import { shortId, timeAgo } from "@/lib/format";

/** The newest jobs, linking to their detail pages. */
export function RecentJobs({ jobs }: { jobs: Job[] }) {
  if (jobs.length === 0) return <p className="text-sm text-zinc-500">No jobs yet. Submit one on the left.</p>;
  return (
    <ul className="divide-y divide-zinc-800 text-sm">
      {jobs.map((j) => (
        <li key={j.id} className="flex flex-wrap items-center gap-x-3 gap-y-1 py-2">
          <Link href={`/jobs/${j.id}`} className="font-mono text-indigo-400 hover:underline" title={j.id}>
            {shortId(j.id)}
          </Link>
          <span className="text-zinc-300">{j.task_type}</span>
          <StatusBadge status={j.status} cancelRequested={j.cancel_requested} />
          <span className="text-zinc-500 tabular-nums">
            {j.attempt_count}/{j.max_attempts} attempts
          </span>
          <time className="ml-auto text-zinc-500" dateTime={j.created_at} suppressHydrationWarning>
            {timeAgo(j.created_at)}
          </time>
        </li>
      ))}
    </ul>
  );
}
