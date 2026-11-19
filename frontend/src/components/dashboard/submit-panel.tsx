"use client";

import Link from "next/link";
import { startTransition, useActionState, useState } from "react";

import { submitJob, type FormState } from "@/app/actions";
import { shortId } from "@/lib/format";
import { FIELD_PREFIX, type Field } from "@/lib/payload";
import { Icon } from "./icons";
import { WorkerStatus } from "./worker-status";

export type TaskOption = {
  value: string;
  label: string;
  fields: Field[] | null;
  example: Record<string, unknown> | null;
  queue: string;
  workersOnline: number;
  defaultTimeoutSeconds: number;
  maxTimeoutSeconds: number;
  defaultMaxAttempts: number;
};

const input =
  "mt-1 w-full rounded-md border border-zinc-800 bg-zinc-950 px-3 py-2 text-sm text-zinc-100 placeholder:text-zinc-600 focus:border-indigo-500";
const labelCls = "block text-sm font-medium text-zinc-300";

/** One generated input, pre-filled with the schema default or the task's example value. */
function FieldInput({ field, example }: { field: Field; example: unknown }) {
  const id = FIELD_PREFIX + field.name;
  const common = { id, name: id, required: field.required };
  const initial = field.defaultValue ?? (example as string | number | boolean | undefined);
  const help = field.description && <p className="mt-1 text-xs text-zinc-500">{field.description}</p>;
  const label = (
    <label htmlFor={id} className={labelCls}>
      {field.name}
      {!field.required && <span className="font-normal text-zinc-500"> (optional)</span>}
    </label>
  );
  switch (field.kind) {
    case "boolean":
      return (
        <div>
          <label className="flex items-center gap-2 text-sm font-medium text-zinc-300">
            <input type="checkbox" id={id} name={id} defaultChecked={initial === true} className="size-4 accent-indigo-500" />
            {field.name}
          </label>
          {help}
        </div>
      );
    case "enum":
      return (
        <div>
          {label}
          <select {...common} defaultValue={String(initial ?? "")} className={input}>
            {!field.required && <option value="">(default)</option>}
            {field.options?.map((o) => (
              <option key={o} value={o}>
                {o === "\t" ? "tab" : o}
              </option>
            ))}
          </select>
          {help}
        </div>
      );
    case "text":
      return (
        <div>
          {label}
          <textarea {...common} rows={4} defaultValue={String(initial ?? "")} className={`${input} font-mono`} />
          {help}
        </div>
      );
    default:
      return (
        <div>
          {label}
          <input
            {...common}
            type={field.kind === "string" ? "text" : "number"}
            step={field.kind === "number" ? "any" : 1}
            defaultValue={initial === undefined ? "" : String(initial)}
            className={input}
          />
          {help}
        </div>
      );
  }
}

/** Submit one task: a payload form generated from the task's schema, pre-filled with its example. */
export function SubmitPanel({
  options,
  selected,
  onSelect,
}: {
  options: TaskOption[];
  selected: string;
  onSelect: (value: string) => void;
}) {
  const [state, action, pending] = useActionState(submitJob, {} as FormState);
  const [rawJson, setRawJson] = useState(false);
  const task = options.find((o) => o.value === selected);
  const jsonMode = rawJson || !task?.fields;

  if (options.length === 0) {
    return (
      <p className="text-sm text-zinc-400">
        Nothing to submit yet: no worker has registered a task type for this application.{" "}
        <Link href="/connect" className="text-indigo-400 hover:underline">
          Connect a worker →
        </Link>
      </p>
    );
  }

  return (
    <div className="space-y-6">
      {/* onSubmit (not action=) so React keeps the inputs after submitting: re-sending the same
          idempotency key and payload is how you see a replay. */}
      <form
        onSubmit={(e) => {
          e.preventDefault();
          const form = new FormData(e.currentTarget);
          startTransition(() => action(form));
        }}
        className="space-y-4"
      >
        <input type="hidden" name="mode" value={jsonMode ? "json" : "form"} />
        <div>
          <label htmlFor="task" className={labelCls}>
            Task type
          </label>
          <select id="task" name="task" value={selected} onChange={(e) => onSelect(e.target.value)} className={input}>
            {[
              { label: "Worker online", items: options.filter((o) => o.workersOnline > 0) },
              { label: "No worker online (jobs will wait)", items: options.filter((o) => o.workersOnline === 0) },
            ]
              .filter((g) => g.items.length > 0)
              .map((g) => (
                <optgroup key={g.label} label={g.label}>
                  {g.items.map((o) => (
                    <option key={o.value} value={o.value}>
                      {o.label}
                    </option>
                  ))}
                </optgroup>
              ))}
          </select>
          {task && (
            <p className="mt-1.5 flex flex-wrap items-center gap-x-3 text-xs">
              <span className="text-zinc-500">
                runs on queue <code className="text-zinc-300">{task.queue}</code>
              </span>
              <WorkerStatus online={task.workersOnline} />
            </p>
          )}
        </div>

        <fieldset key={`${selected}-${jsonMode}`} className="space-y-3 rounded-lg border border-zinc-800 p-3">
          <legend className="flex w-full items-center justify-between px-1 text-xs font-semibold tracking-wide text-zinc-400 uppercase">
            Payload
            {task?.fields && (
              <button type="button" onClick={() => setRawJson(!rawJson)} className="text-xs font-normal tracking-normal text-indigo-400 normal-case hover:underline">
                {rawJson ? "Use form" : "Edit as JSON"}
              </button>
            )}
          </legend>
          {jsonMode ? (
            <textarea
              name="payload_json"
              rows={6}
              defaultValue={JSON.stringify(task?.example ?? {}, null, 2)}
              aria-label="Payload JSON"
              className={`${input} font-mono`}
            />
          ) : (
            task?.fields?.map((f) => <FieldInput key={f.name} field={f} example={task.example?.[f.name]} />)
          )}
        </fieldset>

        <details className="rounded-lg border border-zinc-800 p-3 [&[open]>summary]:mb-3">
          <summary className="cursor-pointer text-sm text-zinc-400 hover:text-zinc-200">Advanced options</summary>
          <div className="space-y-3">
        <div>
          <label htmlFor="idempotency_key" className={labelCls}>
            Idempotency key <span className="font-normal text-zinc-500">(auto-generated if empty)</span>
          </label>
          <input id="idempotency_key" name="idempotency_key" maxLength={255} placeholder="order-1001" className={input} />
          <p className="mt-1 text-xs text-zinc-500">Submit the same key twice and you get the original job back, not a duplicate.</p>
        </div>
        <div className="grid grid-cols-2 gap-3">
          <div>
            <label htmlFor="delay_seconds" className={labelCls}>
              Delay (seconds)
            </label>
            <input id="delay_seconds" name="delay_seconds" type="number" min={0} max={604800} defaultValue={0} className={input} />
          </div>
          <div>
            <label htmlFor="max_attempts" className={labelCls}>
              Max attempts
            </label>
            <input id="max_attempts" name="max_attempts" type="number" min={1} max={10} placeholder={String(task?.defaultMaxAttempts ?? 3)} className={input} />
          </div>
        </div>
        <div>
          <label htmlFor="timeout_seconds" className={labelCls}>
            Timeout (seconds) <span className="font-normal text-zinc-500">(max {task?.maxTimeoutSeconds})</span>
          </label>
          <input
            id="timeout_seconds"
            name="timeout_seconds"
            type="number"
            min={1}
            max={task?.maxTimeoutSeconds}
            placeholder={String(task?.defaultTimeoutSeconds ?? "")}
            className={input}
          />
        </div>
          </div>
        </details>

        {state.error && (
          <div role="alert" className="rounded-md border border-rose-900 bg-rose-950/40 p-3 text-sm text-rose-300">
            <p className="font-medium">{state.error}</p>
            {state.fields && (
              <ul className="mt-1 list-inside list-disc">
                {state.fields.map((f) => (
                  <li key={f.field + f.message}>
                    <code>{f.field}</code> {f.message}
                  </li>
                ))}
              </ul>
            )}
          </div>
        )}
        {state.jobId && !state.error && (
          <p role="status" className="rounded-md border border-emerald-900 bg-emerald-950/40 p-3 text-sm text-emerald-300">
            {state.replayed ? "Key already used: this is the original job " : "Submitted job "}
            <Link href={`/jobs/${state.jobId}`} className="font-mono underline">
              {shortId(state.jobId)}
            </Link>
          </p>
        )}

        <button
          disabled={pending}
          className="flex w-full items-center justify-center gap-2 rounded-md bg-blue-600 px-4 py-2.5 text-sm font-semibold text-white hover:bg-blue-500 disabled:opacity-60"
        >
          <Icon name="send" className="size-4" />
          {pending ? "Submitting…" : "Submit task"}
        </button>
      </form>
    </div>
  );
}
