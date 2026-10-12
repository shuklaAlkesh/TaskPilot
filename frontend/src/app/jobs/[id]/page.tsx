import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { cancelJob } from "@/app/actions";
import { JsonBlock } from "@/components/json-block";
import { StatusBadge } from "@/components/status-badge";
import { api, unwrap } from "@/lib/api/client";
import { formatDateTime, isTerminal, shortId } from "@/lib/format";
import { JobStatusPoller } from "./job-status-poller";

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

type Params = Promise<{ id: string }>;

export async function generateMetadata({ params }: { params: Params }): Promise<Metadata> {
  return { title: `Job ${shortId((await params).id)}` };
}

export default async function JobPage({ params }: { params: Params }) {
  const { id } = await params;
  if (!UUID.test(id)) notFound();
  const { client, session } = await api();
  const path = { params: { path: { job_id: id } } };
  const [job, attempts] = await Promise.all([
    client.GET("/v1/jobs/{job_id}", path).then(unwrap),
    client.GET("/v1/jobs/{job_id}/attempts", path).then(unwrap),
  ]);
  const cancellable =
    session.scopes.includes("jobs:cancel") &&
    (job.status === "queued" || (job.status === "running" && !job.cancel_requested));

  const facts: [string, React.ReactNode][] = [
    ["Task type", `${job.task_type} v${job.task_version}`],
    ["Queue", job.queue],
    ["Attempts", `${job.attempt_count} of ${job.max_attempts}`],
    ["Timeout", `${job.timeout_seconds}s per attempt`],
    ["Created", formatDateTime(job.created_at)],
    ["Started", formatDateTime(job.started_at)],
    ["Finished", formatDateTime(job.finished_at)],
  ];
  if (job.next_attempt_at) facts.push(["Next attempt", formatDateTime(job.next_attempt_at)]);
  if (job.failure_reason) facts.push(["Failure reason", job.failure_reason]);

  return (
    <div className="space-y-8">
      <div>
        <Link href="/jobs" className="text-sm text-zinc-500 hover:underline">
          ← All jobs
        </Link>
        <div className="mt-2 flex flex-wrap items-center gap-3">
          <h1 className="font-mono text-xl font-semibold break-all">{job.id}</h1>
          <StatusBadge status={job.status} cancelRequested={job.cancel_requested} />
          {!isTerminal(job.status) && <JobStatusPoller jobId={job.id} />}
          {cancellable && (
            <form action={cancelJob.bind(null, job.id)} className="ml-auto">
              <button className="rounded-md border border-rose-300 px-3 py-1.5 text-sm text-rose-700 hover:bg-rose-50 dark:border-rose-800 dark:text-rose-400 dark:hover:bg-rose-950">
                Cancel job
              </button>
            </form>
          )}
        </div>
        {job.status === "running" && job.cancel_requested && (
          <p className="mt-2 text-sm text-amber-700 dark:text-amber-400">
            Cancel requested. The worker stops the handler on its next heartbeat. If the handler finishes first, the job still
            succeeds.
          </p>
        )}
      </div>

      <dl className="grid grid-cols-1 gap-x-8 gap-y-3 rounded-lg border border-zinc-200 bg-white p-5 text-sm sm:grid-cols-2 dark:border-zinc-800 dark:bg-zinc-900">
        {facts.map(([label, value]) => (
          <div key={label} className="flex justify-between gap-4 sm:block">
            <dt className="text-zinc-500">{label}</dt>
            <dd className="font-medium sm:mt-0.5">{value}</dd>
          </div>
        ))}
      </dl>

      {job.last_error && (
        <section aria-labelledby="last-error" className="rounded-lg border border-rose-200 bg-rose-50 p-4 text-sm dark:border-rose-900 dark:bg-rose-950/40">
          <h2 id="last-error" className="font-semibold text-rose-800 dark:text-rose-300">
            Last error: <code>{job.last_error.code}</code>
          </h2>
          <p className="mt-1 text-rose-700 dark:text-rose-400">{job.last_error.message}</p>
        </section>
      )}

      <div className="grid gap-6 lg:grid-cols-2">
        <section aria-labelledby="payload" className="space-y-2">
          <h2 id="payload" className="font-semibold">Payload</h2>
          <JsonBlock value={job.payload} />
        </section>
        <section aria-labelledby="result" className="space-y-2">
          <h2 id="result" className="font-semibold">Result</h2>
          {job.result ? <JsonBlock value={job.result} /> : <p className="text-sm text-zinc-500">No result yet.</p>}
        </section>
      </div>

      <section aria-labelledby="attempts" className="space-y-2">
        <h2 id="attempts" className="font-semibold">Attempts</h2>
        {attempts.items.length === 0 ? (
          <p className="text-sm text-zinc-500">Not started yet.</p>
        ) : (
          <ol className="divide-y divide-zinc-200 rounded-lg border border-zinc-200 bg-white text-sm dark:divide-zinc-800 dark:border-zinc-800 dark:bg-zinc-900">
            {attempts.items.map((a) => (
              <li key={a.id} className="flex flex-wrap items-center gap-x-4 gap-y-1 px-4 py-3">
                <span className="w-8 font-medium tabular-nums">#{a.attempt_no}</span>
                <StatusBadge status={a.status} />
                <span className="text-zinc-500">{formatDateTime(a.started_at)}</span>
                {a.error && (
                  <span className="basis-full text-rose-700 sm:basis-auto dark:text-rose-400">
                    <code>{a.error.code}</code>: {a.error.message}
                    {a.error.retryable !== undefined && <span className="text-zinc-500">{a.error.retryable ? " (retryable)" : " (permanent)"}</span>}
                  </span>
                )}
              </li>
            ))}
          </ol>
        )}
      </section>
    </div>
  );
}
