import type { Stats, TaskType } from "@/lib/api/client";
import { formatCount, formatMs, formatPercent } from "@/lib/format";
import { WorkerStatus } from "./worker-status";

type Prop = {
  type?: string;
  enum?: unknown[];
  default?: unknown;
  description?: string;
  minimum?: number;
  maximum?: number;
  minLength?: number;
  maxLength?: number;
};

const show = (v: unknown) => (v === "\t" ? "tab" : JSON.stringify(v));

/** "text · 1–60,000 chars", "integer · 0–50 · default 10", "one of: succeed, crash". */
function rule(p: Prop): string {
  const parts: string[] = [];
  if (p.enum) parts.push(`one of: ${p.enum.map(show).join(", ")}`);
  else parts.push(p.type === "string" ? "text" : (p.type ?? "value"));
  if (p.minimum !== undefined || p.maximum !== undefined) parts.push(`${p.minimum ?? "…"}–${p.maximum ?? "…"}`);
  if (p.maxLength !== undefined) parts.push(`up to ${formatCount(p.maxLength)} chars`);
  if (p.default !== undefined) parts.push(`default ${show(p.default)}`);
  return parts.join(" · ");
}

function inputs(schema: TaskType["payload_schema"]): { name: string; required: boolean; prop: Prop }[] {
  const props = (schema.properties ?? {}) as Record<string, Prop>;
  const required = new Set(Array.isArray(schema.required) ? (schema.required as string[]) : []);
  return Object.entries(props).map(([name, prop]) => ({ name, required: required.has(name), prop }));
}

function Badge({ children, tone }: { children: React.ReactNode; tone: string }) {
  return <span className={`rounded-full px-2 py-0.5 text-xs font-medium ring-1 ring-inset ${tone}`}>{children}</span>;
}

/**
 * Every registered task type in plain language: what it does, its inputs, an example, its limits,
 * and how it has performed for this tenant. Nothing outside this list can ever run.
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
  return (
    <div className="space-y-4">
      <p className="text-sm text-zinc-400">
        TaskPilot only runs task types that a worker has registered, never arbitrary code. Deploy a worker with new
        handlers and they appear here automatically. Every input is checked against the task&apos;s schema before it is
        queued, so bad input is rejected straight away with a clear error.
      </p>
      <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
        {types.map((t) => {
          const used = usage.find((u) => u.task_type === t.task_type);
          const done = used ? used.succeeded + used.failed : 0;
          const value = `${t.task_type}@${t.task_version}`;
          return (
            <article key={value} className="flex flex-col rounded-lg border border-zinc-800 bg-zinc-950 p-4">
              <header className="flex flex-wrap items-center gap-2">
                <h3 className="text-base font-semibold text-zinc-50">{t.title.replace(/\s*\(test only\)/i, "")}</h3>
                <code className="text-xs text-zinc-500">
                  {t.task_type} v{t.task_version}
                </code>
                <span className="ml-auto flex gap-1.5">
                  {t.idempotent && !t.side_effects && <Badge tone="text-emerald-300 ring-emerald-800">safe to retry</Badge>}
                  {t.test_only && <Badge tone="text-amber-300 ring-amber-800">test only</Badge>}
                </span>
              </header>
              <p className="mt-2 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs">
                <span className="text-zinc-500">
                  queue <code className="text-zinc-300">{t.queue}</code>
                </span>
                <WorkerStatus online={t.workers_online} />
              </p>
              <p className="mt-2 text-sm text-zinc-300">{t.description}</p>

              <dl className="mt-3 space-y-1.5 text-sm">
                {inputs(t.payload_schema).map(({ name, required, prop }) => (
                  <div key={name} className="grid grid-cols-[8.5rem_minmax(0,1fr)] gap-2">
                    <dt className="font-mono text-xs leading-5 break-all text-indigo-300">
                      {name}
                      {!required && <span className="text-zinc-600">?</span>}
                    </dt>
                    <dd className="text-zinc-400">
                      {prop.description}
                      <span className="block text-xs text-zinc-500">{rule(prop)}</span>
                    </dd>
                  </div>
                ))}
              </dl>

              {t.example && (
                <details className="mt-3 text-sm">
                  <summary className="cursor-pointer text-zinc-400 hover:text-zinc-200">Example input</summary>
                  <pre className="mt-2 overflow-x-auto rounded-md bg-zinc-900 p-3 font-mono text-xs text-zinc-200">
                    {JSON.stringify(t.example, null, 2)}
                  </pre>
                </details>
              )}

              <div className="mt-auto pt-4">
                <dl className="grid grid-cols-3 gap-2 border-t border-zinc-800 pt-3 text-xs">
                  <div>
                    <dt className="text-zinc-500">Timeout</dt>
                    <dd className="text-zinc-200">
                      {t.default_timeout_seconds}s <span className="text-zinc-500">(max {t.max_timeout_seconds}s)</span>
                    </dd>
                  </div>
                  <div>
                    <dt className="text-zinc-500">Attempts</dt>
                    <dd className="text-zinc-200">up to {t.default_max_attempts}</dd>
                  </div>
                  <div>
                    <dt className="text-zinc-500">Your usage</dt>
                    <dd className="text-zinc-200 tabular-nums">
                      {used ? (
                        <>
                          {formatCount(used.total)} runs · {formatPercent(done ? used.succeeded / done : null)} ok
                          <span className="block text-zinc-500">~{formatMs(used.run_time_p50_ms)} each</span>
                        </>
                      ) : (
                        "not used yet"
                      )}
                    </dd>
                  </div>
                </dl>
                {onUse && (
                  <button
                    type="button"
                    onClick={() => onUse(value)}
                    className="mt-3 w-full rounded-md border border-zinc-700 py-1.5 text-sm font-medium text-zinc-200 hover:border-indigo-500 hover:bg-indigo-950/40"
                  >
                    Try it with the example →
                  </button>
                )}
              </div>
            </article>
          );
        })}
      </div>
    </div>
  );
}
