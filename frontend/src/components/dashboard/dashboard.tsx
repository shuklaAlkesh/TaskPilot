"use client";

import dynamic from "next/dynamic";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";

import type { Job, Stats, TaskType } from "@/lib/api/client";
import { Health } from "./health";
import { Icon, type IconName } from "./icons";
import { Metrics } from "./metrics";
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

function Card({
  title,
  icon,
  aside,
  children,
  className = "",
}: {
  title: string;
  icon?: IconName;
  aside?: React.ReactNode;
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <section
      className={`rounded-xl border border-zinc-800 bg-linear-to-b from-zinc-900 to-zinc-900/60 p-5 shadow-lg shadow-black/20 ${className}`}
      aria-label={title}
    >
      <div className="mb-4 flex flex-wrap items-center justify-between gap-2">
        <h2 className="flex items-center gap-2 text-sm font-semibold tracking-wide text-zinc-200">
          {icon && (
            <span className="flex size-6 items-center justify-center rounded-md bg-zinc-800 text-zinc-400">
              <Icon name={icon} className="size-3.5" />
            </span>
          )}
          {title}
        </h2>
        {aside}
      </div>
      {children}
    </section>
  );
}

/**
 * Live dashboard for one application: every number comes from GET /v1/stats (one DB snapshot),
 * refreshed every 2 s while the tab is visible. Refetches keep the previous frame on screen (no flash).
 */
export function Dashboard({
  appName,
  appId,
  initial,
  initialWindow,
  options,
  taskTypes,
  scopes,
}: {
  appName: string;
  appId: string;
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
  /** "Submit" in the task list: pick the task in the submit form (pre-filled with its example) and jump there. */
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
      <header className="flex flex-wrap items-end justify-between gap-4 border-b border-zinc-800/80 pb-5">
        <div className="min-w-0">
          <p className="text-xs font-medium tracking-widest text-indigo-400 uppercase">Application</p>
          <h1 className="mt-1 truncate text-3xl font-bold tracking-tight text-zinc-50">{appName}</h1>
          <p className="mt-1 font-mono text-xs text-zinc-500" title="Application ID (X-TaskPilot-App)">
            {appId}
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-3">
          {/* One time range for the whole page: trends, run time, and analytics all follow it. */}
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
          <span
            className={`flex h-8 items-center gap-2 rounded-full border px-3 text-xs ${online ? "border-emerald-900 bg-emerald-950/40 text-emerald-300" : "border-rose-900 bg-rose-950/40 text-rose-300"}`}
            role="status"
          >
            <span className={`size-2 rounded-full ${online ? "animate-pulse bg-emerald-400" : "bg-rose-400"}`} aria-hidden="true" />
            {online ? "Live" : "API unreachable, retrying"}
            <time dateTime={stats.generated_at} className="text-zinc-400 tabular-nums" suppressHydrationWarning>
              {timeFmt.format(new Date(stats.generated_at))}
            </time>
          </span>
        </div>
      </header>

      <div className={`space-y-4 transition-opacity ${stale ? "opacity-60" : ""}`}>
        <Metrics stats={stats} />
        <Health stats={stats} />
      </div>

      <div className="grid grid-cols-1 items-start gap-6 lg:grid-cols-[minmax(0,24rem)_minmax(0,1fr)]">
        {canWrite && (
          <Card title="Submit task" icon="send">
            <SubmitPanel options={options} selected={task} onSelect={setTask} />
          </Card>
        )}
        <Card
          title="Recent activity"
          icon="clock"
          className={canWrite ? "" : "lg:col-span-2"}
          aside={
            <Link href="/jobs" className="text-xs text-indigo-400 hover:underline">
              All jobs →
            </Link>
          }
        >
          <RecentJobs jobs={recent} />
        </Card>
      </div>

      <Card title="Queues" icon="layers" aside={<span className="text-xs text-zinc-500">live · workers seen in the last 30 s</span>}>
        <Queues queues={stats.queues} />
      </Card>

      <WhenVisible minHeight={300}>
        <Card title="Task types" icon="workers" aside={<span className="text-xs text-zinc-500">{taskTypes.length} registered by workers</span>}>
          <TaskCatalog types={taskTypes} usage={stats.task_types} onUse={canWrite && taskTypes.length > 0 ? tryTask : undefined} />
        </Card>
      </WhenVisible>

      <section aria-label="Analytics" className={`space-y-6 transition-opacity ${stale ? "opacity-60" : ""}`}>
        <div className="flex flex-wrap items-baseline gap-3">
          <h2 className="text-lg font-semibold text-zinc-100">Analytics</h2>
          <span className="text-xs text-zinc-500">Last {win} · change the range at the top of the page.</span>
        </div>

        <WhenVisible minHeight={1100}>
          <div className="space-y-6">
            <Card title="Throughput" icon="pulse">
              <ThroughputChart buckets={stats.throughput} bucketSeconds={stats.bucket_seconds} />
            </Card>
            <Card title="Latency" icon="clock">
              <Latency latency={stats.latency} queueLagMs={stats.queue_lag_ms} />
            </Card>
            <div className="grid grid-cols-1 gap-6 xl:grid-cols-[minmax(0,2fr)_minmax(0,1fr)]">
              <Card title="By task type" icon="layers">
                <TaskTypeTable rows={stats.task_types} />
              </Card>
              <Card title="Errors" icon="cross">
                <ErrorList errors={stats.errors} attempts={stats.attempts} />
              </Card>
            </div>
          </div>
        </WhenVisible>
      </section>
    </div>
  );
}
