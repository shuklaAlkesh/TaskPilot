"use client";

import Link from "next/link";
import { useEffect, useState } from "react";

import { StatusBadge } from "@/components/status-badge";
import type { Job, Stats } from "@/lib/api/client";
import { formatCount, formatMs, formatPercent, shortId, timeAgo } from "@/lib/format";
import type { StageKey } from "./pipeline";

type Fact = [label: string, value: string];

type StageInfo = {
  title: string;
  what: string;
  how: string;
  facts: (s: Stats) => Fact[];
  /** API status to list, plus an optional client-side filter for finer stages. */
  list?: { status?: Job["status"]; keep?: (j: Job, now: number) => boolean; empty: string };
  /** How many jobs are in this stage, to tell "empty" apart from "not among the newest". */
  count: (s: Stats) => number;
  jobsLink?: string;
};

const finished = (t: Stats["totals"]) => t.succeeded + t.failed + t.cancelled;
const share = (n: number, t: Stats["totals"]) => formatPercent(finished(t) ? n / finished(t) : null);

// Plain-language summary of docs/05-JOB-LIFECYCLE.md for each stage.
const STAGES: Record<StageKey, StageInfo> = {
  submitted: {
    title: "Submitted",
    what: "Every job the API accepted with 202. It is committed to PostgreSQL before the response is sent, so an accepted job is never lost.",
    how: "Sending the same Idempotency-Key again returns the original job instead of creating a duplicate. The payload was already checked against the task type's JSON Schema.",
    facts: (s) => [
      ["Total submitted", formatCount(s.totals.submitted)],
      ["In the last minute", formatCount(s.last_minute.submitted)],
      ["Still in flight", formatCount(s.totals.delayed + s.totals.ready + s.totals.running)],
    ],
    list: { empty: "No jobs yet." },
    count: (s) => s.totals.submitted,
    jobsLink: "/jobs",
  },
  delayed: {
    title: "Delayed queue",
    what: "Queued jobs that are not claimable yet: either scheduled for later (a delay / run_at) or waiting out retry backoff after a failed attempt.",
    how: "Backoff grows exponentially (5 s, 10 s, 20 s … capped at 10 min) with random jitter, so a burst of failures doesn't retry as a burst. When the time comes, the job moves to the ready queue by itself.",
    facts: (s) => [
      ["Delayed now", formatCount(s.totals.delayed)],
      ["Awaiting a retry", formatCount(s.totals.retry_waiting)],
      ["Scheduled for later", formatCount(s.totals.delayed - s.totals.retry_waiting)],
    ],
    list: { status: "queued", keep: (j, now) => !!j.next_attempt_at && Date.parse(j.next_attempt_at) > now, empty: "Nothing is delayed right now." },
    count: (s) => s.totals.delayed,
    jobsLink: "/jobs?status=queued",
  },
  ready: {
    title: "Ready queue",
    what: "Queued jobs a worker can claim right now. Workers take the oldest first.",
    how: "Claims use SELECT … FOR UPDATE SKIP LOCKED, so two workers never receive the same job and never wait on each other. A growing lag means workers can't keep up: add worker slots.",
    facts: (s) => [
      ["Ready now", formatCount(s.totals.ready)],
      ["Oldest has waited", formatMs(s.queue_lag_ms)],
      ["Finished in the last minute", formatCount(s.last_minute.finished)],
    ],
    list: { status: "queued", keep: (j, now) => !j.next_attempt_at || Date.parse(j.next_attempt_at) <= now, empty: "The ready queue is empty." },
    count: (s) => s.totals.ready,
    jobsLink: "/jobs?status=queued",
  },
  running: {
    title: "Running",
    what: "Jobs leased to one worker attempt. Each attempt runs in its own subprocess with a timeout, so it can always be stopped.",
    how: "The worker renews its 30 s lease with a heartbeat every 10 s. If a worker crashes, heartbeats stop, the lease expires, and the reaper re-queues the job. A stale worker can't overwrite the result (fencing).",
    facts: (s) => [
      ["Running now", formatCount(s.totals.running)],
      ["Workers busy with your jobs", formatCount(s.totals.active_workers)],
      ["Median run time", formatMs(s.latency.run_time_p50_ms)],
    ],
    list: { status: "running", empty: "No job is running right now." },
    count: (s) => s.totals.running,
    jobsLink: "/jobs?status=running",
  },
  succeeded: {
    title: "Completed",
    what: "The handler returned a valid result. This is final: the job never changes again.",
    how: "Delivery is at-least-once: after a crash a handler may run twice, so every handler is designed to be safe to repeat.",
    facts: (s) => [
      ["Completed", formatCount(s.totals.succeeded)],
      ["Share of finished jobs", share(s.totals.succeeded, s.totals)],
      ["Median submit → done", formatMs(s.latency.time_to_complete_p50_ms)],
    ],
    list: { status: "succeeded", empty: "No completed jobs yet." },
    count: (s) => s.totals.succeeded,
    jobsLink: "/jobs?status=succeeded",
  },
  retried: {
    title: "Retried",
    what: "Attempts after the first. A retryable failure (timeout, crash, handler error, lost worker) with attempts left sends the job back to the delayed queue.",
    how: "Retries count toward max attempts. Most retried jobs succeed on a later attempt; those that run out go to the dead letter.",
    facts: (s) => [
      ["Retry attempts", formatCount(s.totals.retries)],
      ["Jobs that needed a retry", formatCount(s.totals.jobs_retried)],
      ["Waiting for a retry now", formatCount(s.totals.retry_waiting)],
    ],
    list: { keep: (j) => j.attempt_count > 1, empty: "No recent job needed a retry." },
    count: (s) => s.totals.jobs_retried,
    jobsLink: "/jobs",
  },
  failed: {
    title: "Dead letter",
    what: "Jobs that failed for good: a permanent error (bad input, invalid result) or a retryable error that used up every attempt.",
    how: "They are kept with their last error and every attempt so you can see what went wrong. Open one to read its attempt history.",
    facts: (s) => [
      ["Dead letter", formatCount(s.totals.failed)],
      ["Share of finished jobs", share(s.totals.failed, s.totals)],
      ["Top error in this window", s.errors[0] ? `${s.errors[0].code} (${formatCount(s.errors[0].count)})` : "—"],
    ],
    list: { status: "failed", empty: "Nothing in the dead letter." },
    count: (s) => s.totals.failed,
    jobsLink: "/jobs?status=failed",
  },
  cancelled: {
    title: "Cancelled",
    what: "Jobs cancelled by a client. A queued job is cancelled at once. A running job gets a cancel request and the worker stops it at its next heartbeat.",
    how: "Cancellation is best-effort: if the handler finishes first, the job still completes. Cancelled jobs are never retried.",
    facts: (s) => [
      ["Cancelled", formatCount(s.totals.cancelled)],
      ["Share of finished jobs", share(s.totals.cancelled, s.totals)],
    ],
    list: { status: "cancelled", empty: "No cancelled jobs." },
    count: (s) => s.totals.cancelled,
    jobsLink: "/jobs?status=cancelled",
  },
};

const STEPS = [
  ["1", "Submit", "The API validates the payload and stores the job in PostgreSQL."],
  ["2", "Queue", "It waits in the ready queue, or the delayed queue if scheduled or retrying."],
  ["3", "Run", "A Python worker claims it and runs the handler in an isolated subprocess."],
  ["4", "Outcome", "Completed, retried with backoff, dead-lettered, or cancelled."],
] as const;

const ROWS = 6;

function JobList({ stage, stats }: { stage: StageInfo; stats: Stats }) {
  const [jobs, setJobs] = useState<Job[] | null>(null);
  const { status, keep } = stage.list ?? {};
  const refreshKey = stats.generated_at;
  const now = Date.parse(stats.generated_at); // the snapshot's clock, so the list agrees with the numbers

  useEffect(() => {
    const controller = new AbortController();
    const query = new URLSearchParams({ limit: keep ? "100" : String(ROWS), ...(status && { status }) });
    fetch(`/api/jobs?${query}`, { signal: controller.signal, cache: "no-store" })
      .then((r) => (r.ok ? r.json() : null))
      .then((page: { items: Job[] } | null) => {
        if (!page) return;
        setJobs((keep ? page.items.filter((j) => keep(j, now)) : page.items).slice(0, ROWS));
      })
      .catch(() => {}); // aborted or offline: keep showing the previous list
    return () => controller.abort();
  }, [status, keep, refreshKey, now]);

  return (
    // Fixed height for ROWS rows, so the panel doesn't jump while the list loads or shrinks.
    <ul className="h-[15rem] divide-y divide-zinc-800 overflow-hidden rounded-lg border border-zinc-800 bg-zinc-950 text-sm" aria-live="polite">
      {jobs === null ? (
        <li className="p-3 text-zinc-500">Loading…</li>
      ) : jobs.length === 0 ? (
        <li className="p-3 text-zinc-500">
          {stage.count(stats) === 0
            ? stage.list?.empty
            : `None among the 100 newest jobs. ${formatCount(stage.count(stats))} are older: open Jobs to browse them.`}
        </li>
      ) : (
        jobs.map((j) => (
          <li key={j.id} className="flex h-10 items-center gap-3 px-3">
            <Link href={`/jobs/${j.id}`} className="font-mono text-indigo-400 hover:underline" title={j.id}>
              {shortId(j.id)}
            </Link>
            <span className="truncate text-zinc-300">{j.task_type}</span>
            <StatusBadge status={j.status} cancelRequested={j.cancel_requested} />
            <span className="ml-auto shrink-0 text-xs text-zinc-500 tabular-nums" suppressHydrationWarning>
              {j.next_attempt_at && Date.parse(j.next_attempt_at) > now
                ? `runs ${timeAgo(j.next_attempt_at, now)}`
                : `${j.attempt_count}/${j.max_attempts} · ${timeAgo(j.updated_at, now)}`}
            </span>
          </li>
        ))
      )}
    </ul>
  );
}

/** Explains the selected stage with live numbers and the jobs in it; shows the overview when none is selected. */
export function StageInspector({ selected, stats, onClose }: { selected: StageKey | null; stats: Stats; onClose: () => void }) {
  if (!selected) {
    return (
      <div id="stage-inspector" className="mt-5 border-t border-zinc-800 pt-5">
        <ol className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
          {STEPS.map(([n, title, text]) => (
            <li key={n} className="flex gap-3 rounded-lg border border-zinc-800 bg-zinc-950 p-3">
              <span className="flex size-6 shrink-0 items-center justify-center rounded-full bg-indigo-950 text-xs font-semibold text-indigo-300">{n}</span>
              <span className="text-sm">
                <span className="font-medium text-zinc-200">{title}. </span>
                <span className="text-zinc-400">{text}</span>
              </span>
            </li>
          ))}
        </ol>
        <p className="mt-3 text-xs text-zinc-500">Select any stage or outcome above to see what it means and the jobs in it right now.</p>
      </div>
    );
  }

  const stage = STAGES[selected];
  return (
    <div id="stage-inspector" className="mt-5 grid gap-5 border-t border-zinc-800 pt-5 lg:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]" aria-live="polite">
      <div className="space-y-3">
        <div className="flex items-center justify-between">
          <h3 className="text-base font-semibold text-zinc-50">{stage.title}</h3>
          <button type="button" onClick={onClose} className="text-xs text-zinc-400 hover:text-white">
            Close ✕
          </button>
        </div>
        <p className="text-sm text-zinc-300">{stage.what}</p>
        <p className="text-sm text-zinc-400">{stage.how}</p>
        <dl className="grid grid-cols-1 gap-2 sm:grid-cols-3">
          {stage.facts(stats).map(([label, value]) => (
            <div key={label} className="rounded-lg border border-zinc-800 bg-zinc-950 px-3 py-2">
              <dt className="truncate text-xs text-zinc-500">{label}</dt>
              <dd className="font-semibold text-zinc-100 tabular-nums">{value}</dd>
            </div>
          ))}
        </dl>
      </div>
      <div className="space-y-2">
        <div className="flex items-center justify-between text-sm">
          <span className="font-medium text-zinc-300">Latest jobs here</span>
          {stage.jobsLink && (
            <Link href={stage.jobsLink} className="text-indigo-400 hover:underline">
              Open in Jobs →
            </Link>
          )}
        </div>
        <JobList key={selected} stage={stage} stats={stats} />
      </div>
    </div>
  );
}
