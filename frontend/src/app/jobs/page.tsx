import type { Metadata } from "next";
import Link from "next/link";

import { StatusBadge } from "@/components/status-badge";
import { api, unwrap, type JobStatus } from "@/lib/api/client";
import { shortId, timeAgo } from "@/lib/format";

export const metadata: Metadata = { title: "Jobs" };

const STATUSES: JobStatus[] = ["queued", "running", "succeeded", "failed", "cancelled"];

type SearchParams = Promise<Record<string, string | string[] | undefined>>;

function first(v: string | string[] | undefined): string {
  return (Array.isArray(v) ? v[0] : v) ?? "";
}

export default async function JobsPage({ searchParams }: { searchParams: SearchParams }) {
  const sp = await searchParams;
  const status = STATUSES.find((s) => s === first(sp.status));
  const taskType = first(sp.task_type);
  const queue = first(sp.queue);
  const cursor = first(sp.cursor);

  const { client, session } = await api();
  const [page, types] = await Promise.all([
    client
      .GET("/v1/jobs", {
        params: {
          query: {
            limit: 20,
            ...(status && { status: [status] }),
            ...(taskType && { task_type: taskType }),
            ...(queue && { queue }),
            ...(cursor && { cursor }),
          },
        },
      })
      .then(unwrap),
    client.GET("/v1/task-types").then(unwrap),
  ]);
  const taskTypes = [...new Set(types.items.map((t) => t.task_type))];
  const queues = [...new Set([...types.items.map((t) => t.queue), ...(queue ? [queue] : [])])].sort();
  const filters = new URLSearchParams({
    ...(status && { status }),
    ...(taskType && { task_type: taskType }),
    ...(queue && { queue }),
  });
  const next = page.next_cursor ? new URLSearchParams([...filters, ["cursor", page.next_cursor]]) : null;

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <h1 className="text-2xl font-semibold tracking-tight">Jobs</h1>
        {session.scopes.includes("jobs:write") && (
          <Link href="/" className="rounded-md bg-blue-600 px-3 py-2 text-sm font-medium text-white hover:bg-blue-500">
            Submit a job
          </Link>
        )}
      </div>

      <form method="get" className="flex flex-wrap items-end gap-3 text-sm" aria-label="Filter jobs">
        <label className="flex flex-col gap-1">
          <span className="text-zinc-600 dark:text-zinc-400">Status</span>
          <select name="status" defaultValue={status ?? ""} className="rounded-md border border-zinc-300 bg-white px-2 py-1.5 dark:border-zinc-700 dark:bg-zinc-900">
            <option value="">Any</option>
            {STATUSES.map((s) => (
              <option key={s}>{s}</option>
            ))}
          </select>
        </label>
        <label className="flex flex-col gap-1">
          <span className="text-zinc-600 dark:text-zinc-400">Task type</span>
          <select name="task_type" defaultValue={taskType} className="rounded-md border border-zinc-300 bg-white px-2 py-1.5 dark:border-zinc-700 dark:bg-zinc-900">
            <option value="">Any</option>
            {taskTypes.map((t) => (
              <option key={t}>{t}</option>
            ))}
          </select>
        </label>
        <label className="flex flex-col gap-1">
          <span className="text-zinc-600 dark:text-zinc-400">Queue</span>
          <select name="queue" defaultValue={queue} className="rounded-md border border-zinc-300 bg-white px-2 py-1.5 dark:border-zinc-700 dark:bg-zinc-900">
            <option value="">Any</option>
            {queues.map((q) => (
              <option key={q}>{q}</option>
            ))}
          </select>
        </label>
        <button className="rounded-md border border-zinc-300 px-3 py-1.5 hover:bg-zinc-100 dark:border-zinc-700 dark:hover:bg-zinc-800">Apply</button>
        {(status || taskType || queue) && (
          <Link href="/jobs" className="py-1.5 text-zinc-600 underline dark:text-zinc-400">
            Clear
          </Link>
        )}
      </form>

      {page.items.length === 0 ? (
        <p className="rounded-lg border border-dashed border-zinc-300 p-10 text-center text-zinc-500 dark:border-zinc-700">
          No jobs found.
        </p>
      ) : (
        <div className="overflow-hidden rounded-lg border border-zinc-200 bg-white dark:border-zinc-800 dark:bg-zinc-900">
          <table className="w-full text-sm">
            <thead className="hidden border-b border-zinc-200 bg-zinc-50 text-left text-xs uppercase tracking-wide text-zinc-500 sm:table-header-group dark:border-zinc-800 dark:bg-zinc-900">
              <tr>
                <th className="px-4 py-2 font-medium">Job</th>
                <th className="px-4 py-2 font-medium">Task type</th>
                <th className="px-4 py-2 font-medium">Status</th>
                <th className="px-4 py-2 font-medium">Attempts</th>
                <th className="px-4 py-2 font-medium">Created</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-zinc-200 dark:divide-zinc-800">
              {page.items.map((job) => (
                <tr key={job.id} className="grid grid-cols-2 gap-1 px-4 py-3 hover:bg-zinc-50 sm:table-row sm:p-0 dark:hover:bg-zinc-800/50">
                  <td className="sm:px-4 sm:py-2.5">
                    <Link href={`/jobs/${job.id}`} className="font-mono text-indigo-600 hover:underline dark:text-indigo-400" title={job.id}>
                      {shortId(job.id)}
                    </Link>
                  </td>
                  <td className="text-right sm:px-4 sm:py-2.5 sm:text-left">
                    {job.task_type} <span className="text-zinc-400">v{job.task_version}</span>
                    <span className="block text-xs text-zinc-500">queue {job.queue}</span>
                  </td>
                  <td className="sm:px-4 sm:py-2.5">
                    <StatusBadge status={job.status} cancelRequested={job.cancel_requested} />
                  </td>
                  <td className="text-right tabular-nums sm:px-4 sm:py-2.5 sm:text-left">
                    {job.attempt_count}/{job.max_attempts}
                    <span className="sm:hidden"> attempts</span>
                  </td>
                  <td className="col-span-2 text-zinc-500 sm:px-4 sm:py-2.5">
                    <time dateTime={job.created_at} title={job.created_at}>
                      {timeAgo(job.created_at)}
                    </time>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <div className="flex gap-4 text-sm">
        {cursor && (
          <Link href={`/jobs?${filters}`} className="text-indigo-600 hover:underline dark:text-indigo-400">
            ← Newest
          </Link>
        )}
        {next && (
          <Link href={`/jobs?${next}`} className="ml-auto text-indigo-600 hover:underline dark:text-indigo-400">
            Older jobs →
          </Link>
        )}
      </div>
    </div>
  );
}
