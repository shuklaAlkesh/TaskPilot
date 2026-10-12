"use client";

import dynamic from "next/dynamic";
import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";

import type { Job, Stats, TaskType } from "@/lib/api/client";
import { formatMs } from "@/lib/format";
import { AccessCard } from "./access-card";
import { LiveNumber } from "./live-number";
import { Metrics } from "./metrics";
import { Pipeline, type StageKey } from "./pipeline";
import { StageInspector } from "./stage-inspector";
import { Queues } from "./queues";
import { RecentJobs } from "./recent-jobs";
import { SubmitPanel, type TaskOption } from "./submit-panel";
import { WhenVisible } from "./when-visible";

// Below-the-fold sections load their code only when scrolled near (keeps the first load small).
const TaskCatalog = dynamic(() => import("./task-catalog").then((m) => m.TaskCatalog));
const ThroughputChart = dynamic(() => import("./throughput-chart").then((m) => m.ThroughputChart));
const Latency = dynamic(() => import("./breakdowns").then((m) => m.Latency));
const TaskTypeTable = dynamic(() => import("./breakdowns").then((m) => m.TaskTypeTable));
const ErrorList = dynamic(() => import("./breakdowns").then((m) => m.ErrorList));

export type DashboardData = { stats: Stats; recent: Job[] };

const WINDOWS = ["15m", "1h", "6h", "24h"] as const;
const POLL_MS = 2_000;
const timeFmt = new Intl.DateTimeFormat("en", { hour: "2-digit", minute: "2-digit", second: "2-digit" });

function Card({ title, aside, children, className = "" }: { title: string; aside?: React.ReactNode; children: React.ReactNode; className?: string }) {
  return (
    <section className={`rounded-xl border border-zinc-800 bg-zinc-900 p-5 ${className}`} aria-label={title}>
      <div className="mb-4 flex flex-wrap items-center justify-between gap-2">
        <h2 className="text-sm font-semibold tracking-wide text-zinc-300 uppercase">{title}</h2>
        {aside}
      </div>
      {children}
    </section>
  );
}

/**
 * Live dashboard: every number comes from GET /v1/stats (one DB snapshot), refreshed every 2 s
 * while the tab is visible. Refetches keep the previous frame on screen (no flash).
 */
export function Dashboard({
  initial,
  initialWindow,
  options,
  taskTypes,
  scopes,
}: {
  initial: DashboardData;
  initialWindow: (typeof WINDOWS)[number];
  options: TaskOption[];
  taskTypes: TaskType[];
  scopes: string[];
}) {
  const canWrite = scopes.includes("jobs:write");
  const [task, setTask] = useState(options[0]?.value ?? "");
  const router = useRouter();
  const [data, setData] = useState(initial);
  const [win, setWin] = useState(initialWindow);
  const [stale, setStale] = useState(false);
  const [online, setOnline] = useState(true);
  const [stage, setStage] = useState<StageKey | null>(null);

  useEffect(() => {
    const controller = new AbortController();
    let timer: ReturnType<typeof setTimeout> | undefined;
    const tick = async () => {
      if (document.visibilityState === "visible") {
        try {
          const res = await fetch(`/api/dashboard?window=${win}`, { signal: controller.signal, cache: "no-store" });
          if (res.status === 401) return router.refresh(); // the server render signs out
          if (res.ok) {
            setData(await res.json());
            setOnline(true);
          } else setOnline(false);
        } catch {
          if (controller.signal.aborted) return;
          setOnline(false);
        }
        setStale(false);
      }
      timer = setTimeout(tick, POLL_MS);
    };
    void tick();
    return () => {
      controller.abort();
      clearTimeout(timer);
    };
  }, [win, router]);

  const { stats, recent } = data;
  /** "Try it" in the catalogue: pick the task in the submit form (pre-filled with its example) and jump there. */
  const tryTask = (value: string) => {
    setTask(value);
    requestAnimationFrame(() => {
      const select = document.getElementById("task");
      const smooth = !window.matchMedia("(prefers-reduced-motion: reduce)").matches;
      select?.scrollIntoView({ behavior: smooth ? "smooth" : "auto", block: "center" });
      select?.focus({ preventScroll: true });
    });
  };
  const chooseWindow = (w: (typeof WINDOWS)[number]) => {
    setStale(true);
    setWin(w);
    window.history.replaceState(null, "", `/?window=${w}`);
  };

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-2xl font-bold tracking-tight text-zinc-50">Task Queue Dashboard</h1>
          <p className="text-sm text-zinc-400">A live view of jobs moving from submission through workers to an outcome.</p>
        </div>
        <div className="flex flex-wrap items-center gap-2 text-xs">
          {[
            { label: "finished / min", node: <LiveNumber value={stats.last_minute.finished} /> },
            { label: "submitted / min", node: <LiveNumber value={stats.last_minute.submitted} /> },
            { label: "queue lag", node: <span className="tabular-nums">{formatMs(stats.queue_lag_ms)}</span> },
          ].map((chip) => (
            <span key={chip.label} className="flex h-7 w-44 items-center justify-between gap-2 rounded-full border border-zinc-800 bg-zinc-900 px-3">
              <span className="text-zinc-500">{chip.label}</span>
              <span className="font-semibold text-zinc-100">{chip.node}</span>
            </span>
          ))}
          <span className="flex h-7 items-center gap-2 px-1 text-zinc-400" role="status">
            <span className={`size-2 rounded-full ${online ? "animate-pulse bg-emerald-500" : "bg-rose-500"}`} aria-hidden="true" />
            {online ? "Live" : "API unreachable, retrying"} ·{" "}
            <time dateTime={stats.generated_at} className="tabular-nums" suppressHydrationWarning>
              {timeFmt.format(new Date(stats.generated_at))}
            </time>
          </span>
        </div>
      </div>

      <Card title="Task flow pipeline" aside={<span className="text-xs text-zinc-500">all time · click a stage to inspect it</span>}>
        <Pipeline stats={stats} selected={stage} onSelect={setStage} />
        <StageInspector selected={stage} stats={stats} onClose={() => setStage(null)} />
      </Card>

      <div className="grid grid-cols-1 items-start gap-6 lg:grid-cols-[minmax(0,22rem)_minmax(0,1fr)]">
        {canWrite ? (
          <Card title="Submit task">
            <SubmitPanel options={options} selected={task} onSelect={setTask} />
          </Card>
        ) : (
          <Card title="Your access">
            <AccessCard scopes={scopes} />
          </Card>
        )}
        <div className="space-y-6">
          <Card title="Metrics" aside={<span className="text-xs text-zinc-500">all time</span>}>
            <Metrics stats={stats} />
          </Card>
          <Card title="Recent jobs">
            <RecentJobs jobs={recent} />
          </Card>
        </div>
      </div>

      <Card title="Queues" aside={<span className="text-xs text-zinc-500">live · workers seen in the last 30 s</span>}>
        <Queues queues={stats.queues} />
      </Card>

      <WhenVisible minHeight={900}>
        <Card title="Task catalogue" aside={<span className="text-xs text-zinc-500">{taskTypes.length} task types registered by workers</span>}>
          <TaskCatalog types={taskTypes} usage={stats.task_types} onUse={canWrite ? tryTask : undefined} />
        </Card>
      </WhenVisible>

      <section aria-label="Analytics" className={`space-y-6 transition-opacity ${stale ? "opacity-60" : ""}`}>
        <div className="flex flex-wrap items-center gap-3">
          <h2 className="text-lg font-semibold text-zinc-100">Analytics</h2>
          <div className="flex rounded-lg border border-zinc-800 bg-zinc-900 p-0.5" role="group" aria-label="Time range">
            {WINDOWS.map((w) => (
              <button
                key={w}
                onClick={() => chooseWindow(w)}
                aria-pressed={win === w}
                className={`rounded-md px-3 py-1 text-sm ${win === w ? "bg-zinc-700 font-semibold text-white" : "text-zinc-400 hover:text-white"}`}
              >
                {w}
              </button>
            ))}
          </div>
          <span className="text-xs text-zinc-500">Charts, latency, and errors cover the last {win}.</span>
        </div>

        <WhenVisible minHeight={1100}>
          <div className="space-y-6">
            <Card title="Throughput">
              <ThroughputChart buckets={stats.throughput} bucketSeconds={stats.bucket_seconds} />
            </Card>
            <Card title="Latency">
              <Latency latency={stats.latency} queueLagMs={stats.queue_lag_ms} />
            </Card>
            <div className="grid grid-cols-1 gap-6 xl:grid-cols-[minmax(0,2fr)_minmax(0,1fr)]">
              <Card title="By task type">
                <TaskTypeTable rows={stats.task_types} />
              </Card>
              <Card title="Errors">
                <ErrorList errors={stats.errors} attempts={stats.attempts} />
              </Card>
            </div>
          </div>
        </WhenVisible>
      </section>
    </div>
  );
}
