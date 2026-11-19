"use client";

import Link from "next/link";
import { useState } from "react";

import type { Stats, TaskType } from "@/lib/api/client";
import { formatCount, formatMs, formatPercent } from "@/lib/format";
import { WorkerStatus } from "./worker-status";

const FILTERS = [
  { key: "all", label: "All", keep: () => true },
  { key: "own", label: "This application", keep: (t: TaskType) => !t.shared },
  { key: "shared", label: "Shared", keep: (t: TaskType) => t.shared },
  { key: "online", label: "Worker online", keep: (t: TaskType) => t.workers_online > 0 },
] as const;

type FilterKey = (typeof FILTERS)[number]["key"];

/** Runnable first (a worker is online), then this application's own, then by name. */
function rank(a: TaskType, b: TaskType): number {
  return (
    Number(b.workers_online > 0) - Number(a.workers_online > 0) ||
    Number(a.shared) - Number(b.shared) ||
    a.task_type.localeCompare(b.task_type)
  );
}

/**
 * Task types this application can run, as registered by workers (GET /v1/task-types): its own and
 * shared ones. Nothing here is predefined; an empty list means no worker has registered yet.
 */
export function TaskCatalog({
  types,
  usage,
  onUse,
}: {
  types: TaskType[];
  usage: Stats["task_types"];
  onUse?: ((value: string) => void) | undefined;
}) {
  const [filter, setFilter] = useState<FilterKey>("all");
  const [query, setQuery] = useState("");

  if (types.length === 0) {
    return (
      <p className="text-sm text-zinc-400">
        No task types yet. Start a worker with this application&apos;s API key and its tasks appear here automatically.{" "}
        <Link href="/connect" className="text-indigo-400 hover:underline">
          How to connect →
        </Link>
      </p>
    );
  }

  const q = query.trim().toLowerCase();
  const keep = FILTERS.find((f) => f.key === filter)!.keep;
  const shown = types
    .filter((t) => keep(t) && (!q || t.task_type.includes(q) || t.title.toLowerCase().includes(q) || t.queue.includes(q)))
    .sort(rank);

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <div className="flex rounded-lg border border-zinc-800 bg-zinc-950 p-0.5" role="group" aria-label="Filter task types">
          {FILTERS.map((f) => {
            const count = types.filter(f.keep).length;
            return (
              <button
                key={f.key}
                type="button"
                onClick={() => setFilter(f.key)}
                aria-pressed={filter === f.key}
                className={`rounded-md px-2.5 py-1 text-xs ${filter === f.key ? "bg-zinc-700 font-semibold text-white" : "text-zinc-400 hover:text-white"}`}
              >
                {f.label} <span className="text-zinc-500 tabular-nums">{count}</span>
              </button>
            );
          })}
        </div>
        <input
          type="search"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="Search task, title, or queue"
          aria-label="Search task types"
          className="ml-auto w-full rounded-md border border-zinc-800 bg-zinc-950 px-3 py-1.5 text-sm text-zinc-100 placeholder:text-zinc-600 focus:border-indigo-500 sm:w-64"
        />
      </div>

      {shown.length === 0 ? (
        <p className="py-4 text-sm text-zinc-500">No task types match.</p>
      ) : (
        <div className="max-h-[28rem] overflow-auto rounded-lg border border-zinc-800">
          <table className="w-full text-sm">
            <thead className="sticky top-0 bg-zinc-900 text-left text-xs tracking-wide text-zinc-500 uppercase">
              <tr>
                <th className="px-3 py-2 font-medium">Task type</th>
                <th className="px-3 py-2 font-medium">Owner</th>
                <th className="px-3 py-2 font-medium">Queue</th>
                <th className="px-3 py-2 font-medium">Workers</th>
                <th className="px-3 py-2 text-right font-medium">Limits</th>
                <th className="px-3 py-2 text-right font-medium">Runs</th>
                {onUse && <th className="px-3 py-2" aria-label="Actions" />}
              </tr>
            </thead>
            <tbody className="divide-y divide-zinc-800/80">
              {shown.map((t) => {
                const used = usage.find((u) => u.task_type === t.task_type);
                const done = used ? used.succeeded + used.failed : 0;
                const value = `${t.task_type}@${t.task_version}`;
                return (
                  <tr key={value} className="hover:bg-zinc-800/30">
                    <td className="px-3 py-2">
                      <span className="text-zinc-100">{t.title.replace(/\s*\(test only\)/i, "")}</span>
                      {t.test_only && <span className="ml-2 rounded bg-amber-950 px-1.5 py-0.5 text-[10px] font-medium text-amber-300 uppercase">test</span>}
                      <code className="block text-xs text-zinc-500">
                        {t.task_type} v{t.task_version}
                      </code>
                    </td>
                    <td className="px-3 py-2">
                      <span
                        className={`rounded-full px-2 py-0.5 text-xs ring-1 ring-inset ${t.shared ? "text-zinc-400 ring-zinc-700" : "text-indigo-300 ring-indigo-800"}`}
                      >
                        {t.shared ? "shared" : "this app"}
                      </span>
                    </td>
                    <td className="px-3 py-2">
                      <code className="text-xs text-zinc-300">{t.queue}</code>
                    </td>
                    <td className="px-3 py-2 text-xs whitespace-nowrap">
                      <WorkerStatus online={t.workers_online} />
                    </td>
                    <td className="px-3 py-2 text-right text-xs whitespace-nowrap text-zinc-400 tabular-nums">
                      {t.default_timeout_seconds}s · {t.default_max_attempts}×
                    </td>
                    <td className="px-3 py-2 text-right whitespace-nowrap text-zinc-300 tabular-nums">
                      {used ? (
                        <>
                          {formatCount(used.total)} · {formatPercent(done ? used.succeeded / done : null)}
                          <span className="block text-xs text-zinc-500">p50 {formatMs(used.run_time_p50_ms)}</span>
                        </>
                      ) : (
                        <span className="text-zinc-600">—</span>
                      )}
                    </td>
                    {onUse && (
                      <td className="px-3 py-2 text-right">
                        <button
                          type="button"
                          onClick={() => onUse(value)}
                          className="rounded-md border border-zinc-700 px-2 py-0.5 text-xs text-zinc-200 hover:border-indigo-500 hover:bg-indigo-950/40"
                        >
                          Submit
                        </button>
                      </td>
                    )}
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
